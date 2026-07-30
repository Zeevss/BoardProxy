//go:build darwin

package tun

import (
	"fmt"
	"os/exec"
	"strings"

	"bproxy-core/pkg/bproxy"
)

// defaultTunName — на macOS имя utun-интерфейса назначает ядро; передаём
// префикс, фактическое имя (utunN) читаем из устройства после открытия.
func defaultTunName() string { return "utun" }

// darwinPlatform настраивает маршруты через route(8)/ifconfig(8) и DNS через
// networksetup(1). Требует root на этапе applyRouting.
type darwinPlatform struct {
	phys physNet
	prot *darwinProtector

	service     string // сетевой сервис (напр. "Wi-Fi") для networksetup
	splitRoutes bool
	dnsBackup   string
	dnsReconfig bool
	tunName     string
}

type physNet struct {
	dev     string
	index   int
	gateway string
}

func newPlatform() (platform, error) {
	// Если прошлый запуск завершился аварийно, система осталась с нашим DNS и
	// маршрутами — откатываем их до того, как поднимем новый туннель.
	recoverStaleState()

	phys, err := detectPhysical()
	if err != nil {
		return nil, err
	}
	return &darwinPlatform{
		phys: phys,
		// Свой DNS-резолв клиента направляем на публичный резолвер через
		// физический интерфейс: системный DNS на время туннеля указывает внутрь
		// TUN и для control-plane недостижим.
		prot: &darwinProtector{index: phys.index, dns: defaultDNS},
	}, nil
}

// recoverStaleState восстанавливает настройки, оставшиеся от аварийно
// завершившегося запуска (см. journal).
func recoverStaleState() {
	j := loadJournal()
	if j == nil {
		return
	}
	if j.DNSService != "" {
		restoreDNS(j.DNSService, j.DNSBackup)
	}
	if j.SplitRoutes && j.TunName != "" {
		_ = run("route", "-n", "delete", "-net", "0.0.0.0/1", "-interface", j.TunName)
		_ = run("route", "-n", "delete", "-net", "128.0.0.0/1", "-interface", j.TunName)
	}
	clearJournal()
}

func (d *darwinPlatform) protector() bproxy.SocketProtector { return d.prot }

// detectPhysical читает интерфейс и шлюз маршрута по умолчанию через route(8).
func detectPhysical() (physNet, error) {
	out, err := exec.Command("route", "-n", "get", "default").Output()
	if err != nil {
		return physNet{}, fmt.Errorf("tun: read default route: %w", err)
	}
	var p physNet
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "interface:"):
			p.dev = strings.TrimSpace(strings.TrimPrefix(line, "interface:"))
		case strings.HasPrefix(line, "gateway:"):
			p.gateway = strings.TrimSpace(strings.TrimPrefix(line, "gateway:"))
		}
	}
	if p.dev == "" {
		return physNet{}, fmt.Errorf("tun: no default route found")
	}
	if idx, err := interfaceIndex(p.dev); err == nil {
		p.index = idx
	}
	return p, nil
}

// applyRouting поднимает utun и перекрывает маршрут по умолчанию. DNS здесь не
// трогаем — он ставится позже, из applyDNS.
func (d *darwinPlatform) applyRouting(ifName string, p Params) error {
	d.tunName = ifName
	// utun — point-to-point: локальный адрес, адрес-пир и маска /32.
	if err := run("ifconfig", ifName, "inet", p.TunAddr, p.Gateway,
		"netmask", "255.255.255.255", "mtu", fmt.Sprint(p.MTU), "up"); err != nil {
		return err
	}
	// Перекрываем default двумя более специфичными маршрутами (0/1 и 128/1), не
	// трогая системный default, чтобы легко откатиться.
	if err := run("route", "-n", "add", "-net", "0.0.0.0/1", "-interface", ifName); err != nil {
		return err
	}
	if err := run("route", "-n", "add", "-net", "128.0.0.0/1", "-interface", ifName); err != nil {
		_ = run("route", "-n", "delete", "-net", "0.0.0.0/1", "-interface", ifName)
		return err
	}
	d.splitRoutes = true
	d.writeJournal()
	return nil
}

// applyDNS прописывает резолвер сетевому сервису, сохранив прежний список.
// Ошибка не фатальна: split-маршруты уже уводят публичные резолверы в туннель.
func (d *darwinPlatform) applyDNS(dns string) error {
	service, err := serviceForDevice(d.phys.dev)
	if err != nil {
		return err
	}
	d.service = service
	if cur, err := exec.Command("networksetup", "-getdnsservers", service).Output(); err == nil {
		d.dnsBackup = normalizeDNSBackup(string(cur))
	}
	// Журналируем ДО изменения: если процесс умрёт сразу после setdnsservers,
	// откат всё равно возможен при следующем запуске.
	d.dnsReconfig = true
	d.writeJournal()

	if err := run("networksetup", "-setdnsservers", service, dns); err != nil {
		d.dnsReconfig = false
		d.writeJournal()
		return err
	}
	return nil
}

func (d *darwinPlatform) revertRouting() error {
	var firstErr error
	if d.dnsReconfig && d.service != "" {
		if err := restoreDNS(d.service, d.dnsBackup); err != nil {
			firstErr = err
		}
		d.dnsReconfig = false
	}
	if d.splitRoutes {
		if err := run("route", "-n", "delete", "-net", "0.0.0.0/1", "-interface", d.tunName); err != nil && firstErr == nil {
			firstErr = err
		}
		if err := run("route", "-n", "delete", "-net", "128.0.0.0/1", "-interface", d.tunName); err != nil && firstErr == nil {
			firstErr = err
		}
		d.splitRoutes = false
	}
	clearJournal()
	return firstErr
}

// writeJournal сохраняет текущее состояние отката на диск.
func (d *darwinPlatform) writeJournal() {
	j := journal{TunName: d.tunName, SplitRoutes: d.splitRoutes}
	if d.dnsReconfig {
		j.DNSService = d.service
		j.DNSBackup = d.dnsBackup
	}
	j.save()
}

// restoreDNS возвращает сервису прежние резолверы. Пустой backup означает
// «не было своих» — networksetup сбрасывается словом empty (вернётся к DHCP).
func restoreDNS(service, backup string) error {
	args := []string{"-setdnsservers", service}
	if backup == "" {
		args = append(args, "empty")
	} else {
		args = append(args, strings.Fields(backup)...)
	}
	return run("networksetup", args...)
}

// normalizeDNSBackup приводит вывод -getdnsservers к списку адресов. Когда своих
// резолверов нет, networksetup печатает фразу вида "There aren't any DNS
// Servers set on ..." — её нужно трактовать как пустой список.
func normalizeDNSBackup(out string) string {
	out = strings.TrimSpace(out)
	if out == "" || strings.Contains(strings.ToLower(out), "aren't any") {
		return ""
	}
	fields := strings.Fields(out)
	for _, f := range fields {
		// Санитарная проверка: ожидаем адреса, а не текст сообщения.
		if strings.ContainsAny(f, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ") &&
			!strings.Contains(f, ":") {
			return ""
		}
	}
	return strings.Join(fields, " ")
}

// serviceForDevice сопоставляет устройство (enX) с именем сетевого сервиса,
// которое понимает networksetup.
func serviceForDevice(dev string) (string, error) {
	out, err := exec.Command("networksetup", "-listnetworkserviceorder").Output()
	if err != nil {
		return "", err
	}
	// Блоки вида:
	// (1) Wi-Fi
	// (Hardware Port: Wi-Fi, Device: en0)
	lines := strings.Split(string(out), "\n")
	for i, line := range lines {
		if strings.Contains(line, "Device: "+dev+")") && i > 0 {
			prev := strings.TrimSpace(lines[i-1])
			if idx := strings.Index(prev, ") "); idx != -1 {
				return strings.TrimSpace(prev[idx+2:]), nil
			}
		}
	}
	return "", fmt.Errorf("tun: no network service for device %s", dev)
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tun: %s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
