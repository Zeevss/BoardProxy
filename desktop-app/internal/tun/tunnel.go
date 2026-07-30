package tun

import (
	"errors"
	"sync"

	"bproxy-core/pkg/bproxy"
)

// Params задаёт параметры TUN-интерфейса. Нулевые поля заполняются дефолтами
// (по образцу Android VpnTunnelConfig).
type Params struct {
	// ProxyAddr — адрес локального SOCKS5, куда движок форвардит трафик,
	// вида "127.0.0.1:1080" (обязателен).
	ProxyAddr string
	// TunName — желаемое имя интерфейса. Пусто — платформенный дефолт (на macOS
	// имя всё равно назначает ОС, напр. utun3).
	TunName string
	// TunAddr — IPv4-адрес на TUN-интерфейсе.
	TunAddr string
	// Gateway — адрес-пир/шлюз TUN (нужен там, где маршрут требует next-hop:
	// Windows, отчасти macOS).
	Gateway string
	// Prefix — длина маски адреса TUN.
	Prefix int
	// MTU интерфейса.
	MTU int
	// DNS — резолвер, который прописывается системе на время туннеля. Должен быть
	// вне локальной подсети, чтобы запросы шли через TUN (и резолвились egress'ом
	// на сервере). Пусто — дефолт.
	DNS string
}

// DefaultTunAddr — адрес по умолчанию на TUN-интерфейсе. Экспортирован, чтобы
// helper мог поднять на нём локальный DNS-резолвер и согласованно прописать его
// системе.
const DefaultTunAddr = "10.89.0.2"

const (
	defaultTunAddr = DefaultTunAddr
	defaultGateway = "10.89.0.1"
	defaultPrefix  = 24
	defaultMTU     = 1500
	defaultDNS     = "1.1.1.1"
)

func (p *Params) normalize() {
	if p.TunAddr == "" {
		p.TunAddr = defaultTunAddr
	}
	if p.Gateway == "" {
		p.Gateway = defaultGateway
	}
	if p.Prefix == 0 {
		p.Prefix = defaultPrefix
	}
	if p.MTU == 0 {
		p.MTU = defaultMTU
	}
	if p.DNS == "" {
		p.DNS = defaultDNS
	}
}

// platform инкапсулирует привилегированную системную интеграцию, специфичную
// для ОС: защиту control-plane сокетов от петли и настройку маршрутов/DNS.
// Конкретную реализацию отдаёт newPlatform (файлы с build-тегами).
type platform interface {
	// protector выводит собственные сокеты клиента (WSS/REST/DNS к доске) из
	// TUN, привязывая их к физическому интерфейсу. Передаётся в bproxy.Config,
	// без него получилась бы бесконечная петля маршрутизации.
	protector() bproxy.SocketProtector
	// applyRouting назначает адрес интерфейсу ifName, поднимает его, перекрывает
	// маршрут по умолчанию через TUN и прописывает DNS.
	applyRouting(ifName string, p Params) error
	// revertRouting восстанавливает исходные маршруты и DNS. Идемпотентна.
	revertRouting() error
}

// Controller управляет жизненным циклом TUN-режима. Порядок использования:
//
//	c, err := tun.New()          // определяет физическую сеть, строит Protector
//	cfg.Protector = c.Protector()
//	// ... запустить bproxy.Client, дождаться StatusConnected ...
//	err = c.Start(params)        // поднять устройство, движок и маршруты
//	// ... работа ...
//	c.Stop()                     // откатить маршруты, закрыть устройство
type Controller struct {
	plat platform
	// startFn инъектируется в тестах; в проде — реальный движок tun2socks.
	startFn func(name, proxyAddr string, mtu int) (stackEngine, string, error)

	mu      sync.Mutex
	eng     stackEngine
	started bool
	stopped bool
}

// stackEngine — минимальный контракт userspace-стека (для подмены в тестах).
type stackEngine interface{ stop() }

// New определяет физический интерфейс/шлюз/DNS (до перекрытия маршрута по
// умолчанию) и готовит платформенный Protector. Требует прав root/администратора
// на этапе Start; New лишь читает состояние сети.
func New() (*Controller, error) {
	plat, err := newPlatform()
	if err != nil {
		return nil, err
	}
	return &Controller{plat: plat, startFn: realStartEngine}, nil
}

// realStartEngine — продовый запуск движка, приведённый к интерфейсному типу.
func realStartEngine(name, proxyAddr string, mtu int) (stackEngine, string, error) {
	return startEngine(name, proxyAddr, mtu)
}

// Protector возвращает защитник control-plane сокетов для bproxy.Config.
func (c *Controller) Protector() bproxy.SocketProtector {
	return c.plat.protector()
}

// Start поднимает TUN-устройство и движок tun2socks, затем настраивает
// системные маршруты/DNS. При ошибке настройки маршрутов движок откатывается,
// чтобы не оставить полуготовое устройство.
func (c *Controller) Start(p Params) error {
	p.normalize()
	if p.ProxyAddr == "" {
		return errors.New("tun: proxy address is required")
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.started {
		return errors.New("tun: already started")
	}

	eng, ifName, err := c.startFn(p.TunName, p.ProxyAddr, p.MTU)
	if err != nil {
		return err
	}
	if err := c.plat.applyRouting(ifName, p); err != nil {
		eng.stop()
		return err
	}
	c.eng = eng
	c.started = true
	return nil
}

// Stop восстанавливает маршруты/DNS и закрывает устройство. Безопасно вызывать
// повторно и даже если Start не выполнялся. Возвращает ошибку отката маршрутов
// (устройство закрывается в любом случае).
func (c *Controller) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped {
		return nil
	}
	c.stopped = true

	var routeErr error
	if c.started {
		routeErr = c.plat.revertRouting()
	}
	if c.eng != nil {
		c.eng.stop()
		c.eng = nil
	}
	return routeErr
}
