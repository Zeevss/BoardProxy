//go:build windows

package tun

import (
	"bproxy-core/pkg/bproxy"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
)

// defaultTunName — имя Wintun-адаптера.
func defaultTunName() string { return "BoardProxy" }

// windowsPlatform настраивает адрес/маршруты/DNS через netsh и route. Требует
// прав администратора; Wintun DLL поставляется движком tun2socks.
type windowsPlatform struct {
	phys physNet
	prot *windowsProtector

	tunName      string
	routeNexthop string
	addedRoute   bool
	dnsReconfig  bool
}

type physNet struct {
	index   int    // индекс физического интерфейса
	gateway string // шлюз по умолчанию
	srcIP   string // адрес физического интерфейса
	dns     string
}

func newPlatform() (platform, error) {
	// Откатываем настройки, если прошлый запуск завершился аварийно.
	recoverStaleState()

	phys, err := detectPhysical()
	if err != nil {
		return nil, err
	}
	return &windowsPlatform{
		phys: phys,
		prot: &windowsProtector{index: phys.index, dns: phys.dns},
	}, nil
}

func (w *windowsPlatform) protector() bproxy.SocketProtector { return w.prot }

// detectPhysical парсит `route print -4`, выбирая маршрут по умолчанию с
// наименьшей метрикой. Строки данных числовые, поэтому парсинг не зависит от
// локали Windows.
func detectPhysical() (physNet, error) {
	out, err := exec.Command("route", "print", "-4").Output()
	if err != nil {
		return physNet{}, fmt.Errorf("tun: read routing table: %w", err)
	}
	var (
		best   physNet
		found  bool
		bestMt = int(^uint(0) >> 1)
	)
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(strings.TrimSpace(line))
		// Формат: Destination Netmask Gateway Interface Metric
		if len(f) < 5 || f[0] != "0.0.0.0" || f[1] != "0.0.0.0" {
			continue
		}
		metric, err := strconv.Atoi(f[4])
		if err != nil {
			continue
		}
		if net.ParseIP(f[2]) == nil || net.ParseIP(f[3]) == nil {
			continue
		}
		if metric < bestMt {
			bestMt = metric
			best = physNet{gateway: f[2], srcIP: f[3], dns: defaultDNS}
			found = true
		}
	}
	if !found {
		return physNet{}, fmt.Errorf("tun: no default route found")
	}
	if idx, err := indexForIP(best.srcIP); err == nil {
		best.index = idx
	}
	return best, nil
}

// indexForIP находит индекс интерфейса, которому принадлежит адрес ip.
func indexForIP(ip string) (int, error) {
	target := net.ParseIP(ip)
	ifaces, err := net.Interfaces()
	if err != nil {
		return 0, err
	}
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.Equal(target) {
				return iface.Index, nil
			}
		}
	}
	return 0, fmt.Errorf("tun: no interface for ip %s", ip)
}

func (w *windowsPlatform) applyRouting(ifName string, p Params) error {
	w.tunName = ifName
	mask := prefixToMask(p.Prefix)
	// Назначаем статический адрес Wintun-адаптеру.
	if err := run("netsh", "interface", "ip", "set", "address",
		fmt.Sprintf("name=%s", ifName), "static", p.TunAddr, mask); err != nil {
		return err
	}
	// Маршрут по умолчанию через TUN с метрикой 1 (побеждает физический default).
	if err := run("netsh", "interface", "ipv4", "add", "route",
		"prefix=0.0.0.0/0", fmt.Sprintf("interface=%s", ifName),
		fmt.Sprintf("nexthop=%s", p.Gateway), "metric=1", "store=active"); err != nil {
		return err
	}
	w.addedRoute = true
	w.routeNexthop = p.Gateway
	w.writeJournal()
	return nil
}

// applyDNS прописывает резолвер на адаптере TUN. Вызывается после старта
// локального резолвера (см. Controller.ApplyDNS).
func (w *windowsPlatform) applyDNS(dns string) error {
	// Журналируем ДО изменения — страховка от аварийного завершения.
	w.dnsReconfig = true
	w.writeJournal()
	if err := run("netsh", "interface", "ipv4", "set", "dnsservers",
		fmt.Sprintf("name=%s", w.tunName), "static", dns, "primary"); err != nil {
		w.dnsReconfig = false
		w.writeJournal()
		return err
	}
	return nil
}

// writeJournal сохраняет состояние отката на диск (см. journal.go).
func (w *windowsPlatform) writeJournal() {
	j := journal{TunName: w.tunName, DefaultRoute: w.addedRoute, DNSService: w.routeNexthop}
	if w.dnsReconfig {
		j.DNSBackup = "dhcp"
	}
	j.save()
}

// recoverStaleState откатывает настройки, оставшиеся от аварийного завершения.
func recoverStaleState() {
	j := loadJournal()
	if j == nil {
		return
	}
	if j.TunName != "" {
		if j.DNSBackup != "" {
			_ = run("netsh", "interface", "ipv4", "set", "dnsservers",
				fmt.Sprintf("name=%s", j.TunName), "dhcp")
		}
		if j.DefaultRoute {
			_ = run("netsh", "interface", "ipv4", "delete", "route",
				"prefix=0.0.0.0/0", fmt.Sprintf("interface=%s", j.TunName),
				fmt.Sprintf("nexthop=%s", j.DNSService))
		}
	}
	clearJournal()
}

func (w *windowsPlatform) revertRouting() error {
	var firstErr error
	if w.dnsReconfig {
		if err := run("netsh", "interface", "ipv4", "set", "dnsservers",
			fmt.Sprintf("name=%s", w.tunName), "dhcp"); err != nil {
			firstErr = err
		}
		w.dnsReconfig = false
	}
	if w.addedRoute {
		if err := run("netsh", "interface", "ipv4", "delete", "route",
			"prefix=0.0.0.0/0", fmt.Sprintf("interface=%s", w.tunName),
			fmt.Sprintf("nexthop=%s", w.routeNexthop)); err != nil && firstErr == nil {
			firstErr = err
		}
		w.addedRoute = false
	}
	clearJournal()
	return firstErr
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tun: %s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
