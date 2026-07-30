//go:build darwin

package tun

import (
	"bproxy-core/pkg/bproxy"
	"fmt"
	"os/exec"
	"strings"
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
	dnsBackupOK bool
	dnsReconfig bool
	tunName     string
}

type physNet struct {
	dev     string
	index   int
	gateway string
}

func newPlatform() (platform, error) {
	phys, err := detectPhysical()
	if err != nil {
		return nil, err
	}
	return &darwinPlatform{
		phys: phys,
		prot: &darwinProtector{index: phys.index, dns: defaultDNS},
	}, nil
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

func (d *darwinPlatform) applyRouting(ifName string, p Params) error {
	d.tunName = ifName
	// Поднимаем utun как point-to-point: локальный и адрес-пир.
	if err := run("ifconfig", ifName, p.TunAddr, p.Gateway, "up"); err != nil {
		return err
	}
	// Перекрываем default двумя более специфичными маршрутами (0/1 и 128/1),
	// не трогая системный default, чтобы легко откатиться.
	if err := run("route", "-n", "add", "-net", "0.0.0.0/1", "-interface", ifName); err != nil {
		return err
	}
	if err := run("route", "-n", "add", "-net", "128.0.0.0/1", "-interface", ifName); err != nil {
		_ = run("route", "-n", "delete", "-net", "0.0.0.0/1", "-interface", ifName)
		return err
	}
	d.splitRoutes = true

	if err := d.setDNS(p.DNS); err != nil {
		// DNS — не фатально: split-маршруты всё равно уводят публичные резолверы
		// в туннель. Продолжаем.
		d.prot.dns = defaultDNS
	}
	return nil
}

// setDNS определяет сетевой сервис по физическому устройству и прописывает наш
// резолвер, сохранив прежний список.
func (d *darwinPlatform) setDNS(dns string) error {
	service, err := serviceForDevice(d.phys.dev)
	if err != nil {
		return err
	}
	d.service = service
	if cur, err := exec.Command("networksetup", "-getdnsservers", service).Output(); err == nil {
		d.dnsBackup = strings.TrimSpace(string(cur))
		d.dnsBackupOK = true
	}
	if err := run("networksetup", "-setdnsservers", service, dns); err != nil {
		return err
	}
	d.dnsReconfig = true
	return nil
}

func (d *darwinPlatform) revertRouting() error {
	var firstErr error
	if d.dnsReconfig && d.service != "" {
		// networksetup требует слово "empty" для сброса на DHCP-значения.
		restore := "empty"
		if d.dnsBackupOK && d.dnsBackup != "" && !strings.Contains(d.dnsBackup, "aren't any") {
			restore = d.dnsBackup
		}
		args := append([]string{"-setdnsservers", d.service}, strings.Fields(restore)...)
		if err := run("networksetup", args...); err != nil {
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
	return firstErr
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
