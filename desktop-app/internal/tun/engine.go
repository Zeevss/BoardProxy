// Пакет tun реализует режим полного туннеля (TUN/VPN) для desktop-app. Он
// поднимает виртуальный сетевой интерфейс и userspace TCP/IP-стек (gVisor через
// tun2socks/v2), заворачивая весь IP-трафик в уже работающий локальный SOCKS5,
// который bproxy.Client отдаёт поверх mux-транспорта доски. Аналог Android, где
// эту роль играет нативный hev-socks5-tunnel.
package tun

import (
	"fmt"
	"net/url"

	tun2socks "github.com/xjasonlyu/tun2socks/v2/core"
	"github.com/xjasonlyu/tun2socks/v2/core/device"
	"github.com/xjasonlyu/tun2socks/v2/core/device/tun"
	"github.com/xjasonlyu/tun2socks/v2/log"
	"github.com/xjasonlyu/tun2socks/v2/proxy"
	// Регистрирует протокол "socks5" в proxy.Parse через init(); без этого
	// blank-импорта диспетчер вернёт "unknown protocol".
	_ "github.com/xjasonlyu/tun2socks/v2/proxy/socks5"
	"github.com/xjasonlyu/tun2socks/v2/tunnel"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

// engine — экземпляр userspace-стека tun2socks: связка TUN-устройства и gVisor
// netstack, форвардящего перехваченные потоки в SOCKS-прокси. Мы собираем стек
// напрямую из экспортированных пакетов tun2socks, а не через engine.Start(),
// потому что тот при любой ошибке вызывает log.Fatalf и убил бы весь GUI-процесс
// (например при нехватке прав на создание устройства).
type engine struct {
	dev device.Device
	stk *stack.Stack
}

// startEngine открывает TUN-устройство name (пустое — платформенный дефолт) с
// заданным MTU и направляет весь трафик в socks5://proxyAddr. Возвращает
// движок и фактическое имя интерфейса (ОС может назначить своё, напр. utunN на
// macOS) — оно нужно для настройки маршрутов.
func startEngine(name, proxyAddr string, mtu int) (*engine, string, error) {
	// Приглушаем встроенный логгер tun2socks (по умолчанию — шумный zap на
	// stderr): в проде интересны только предупреждения и ошибки.
	if level, err := log.ParseLevel("warning"); err == nil {
		log.SetLogger(log.Must(log.NewLeveled(level)))
	}

	proxyURL := &url.URL{Scheme: "socks5", Host: proxyAddr}
	p, err := proxy.Parse(proxyURL)
	if err != nil {
		return nil, "", fmt.Errorf("tun: parse proxy %q: %w", proxyAddr, err)
	}
	// tunnel.T() — глобальный обработчик tun2socks (единственный экземпляр на
	// процесс). Наш режим TUN тоже единственный за раз, так что синглтон подходит.
	tunnel.T().SetProxy(p)

	if name == "" {
		name = defaultTunName()
	}
	dev, err := tun.Open(name, uint32(mtu))
	if err != nil {
		return nil, "", fmt.Errorf("tun: open device %q: %w", name, err)
	}

	stk, err := tun2socks.CreateStack(&tun2socks.Config{
		LinkEndpoint:     dev,
		TransportHandler: tunnel.T(),
	})
	if err != nil {
		dev.Close()
		return nil, "", fmt.Errorf("tun: create netstack: %w", err)
	}

	return &engine{dev: dev, stk: stk}, dev.Name(), nil
}

// stop закрывает netstack и TUN-устройство. Идемпотентность обеспечивает
// вызывающий (Tunnel).
func (e *engine) stop() {
	if e == nil {
		return
	}
	if e.dev != nil {
		e.dev.Close()
	}
	if e.stk != nil {
		e.stk.Close()
		e.stk.Wait()
	}
}
