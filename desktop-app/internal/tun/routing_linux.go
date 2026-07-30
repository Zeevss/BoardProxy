//go:build linux

package tun

import (
	"bproxy-core/pkg/bproxy"
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// defaultTunName — имя TUN-интерфейса на Linux (IFNAMSIZ ≤ 15 символов).
func defaultTunName() string { return "bproxy0" }

const resolvConfPath = "/etc/resolv.conf"

// linuxPlatform держит исходное состояние сети для отката и настраивает маршруты
// через утилиту ip. Привилегии (root/CAP_NET_ADMIN) требуются на этапе
// applyRouting и для SO_BINDTODEVICE в protector.
type linuxPlatform struct {
	phys physNet
	prot *linuxProtector

	tunName      string
	addedDefault bool

	// backup /etc/resolv.conf для восстановления.
	resolvBackup    []byte
	resolvWasLink   bool
	resolvLinkDest  string
	resolvExisted   bool
	dnsReconfigured bool
}

type physNet struct {
	dev     string // физический интерфейс с маршрутом по умолчанию
	gateway string // его шлюз
	dns     string // рабочий upstream-резолвер вне loopback
}

func newPlatform() (platform, error) {
	// Если прошлый запуск завершился аварийно, система осталась с нашим DNS и
	// маршрутами — откатываем их до подъёма нового туннеля.
	recoverStaleState()

	phys, err := detectPhysical()
	if err != nil {
		return nil, err
	}
	prot := &linuxProtector{dev: phys.dev, dns: phys.dns}
	if err := prot.verifyPrivileges(); err != nil {
		return nil, err
	}
	return &linuxPlatform{phys: phys, prot: prot}, nil
}

func (l *linuxPlatform) protector() bproxy.SocketProtector { return l.prot }

// detectPhysical читает текущий маршрут по умолчанию (dev + gateway) и upstream
// DNS. Вызывать до перекрытия default через TUN.
func detectPhysical() (physNet, error) {
	out, err := exec.Command("ip", "route", "show", "default").Output()
	if err != nil {
		return physNet{}, fmt.Errorf("tun: read default route: %w", err)
	}
	// Пример: "default via 192.168.1.1 dev wlan0 proto dhcp metric 600"
	fields := strings.Fields(string(out))
	var p physNet
	for i := 0; i < len(fields)-1; i++ {
		switch fields[i] {
		case "via":
			p.gateway = fields[i+1]
		case "dev":
			p.dev = fields[i+1]
		}
	}
	if p.dev == "" {
		return physNet{}, fmt.Errorf("tun: no default route found")
	}
	p.dns = upstreamDNS()
	return p, nil
}

// upstreamDNS возвращает первый nameserver из resolv.conf вне loopback (стаб
// systemd-resolved 127.0.0.53 недостижим через сокет, привязанный к физическому
// интерфейсу). Если такого нет — публичный резолвер по умолчанию.
func upstreamDNS() string {
	f, err := os.Open(resolvConfPath)
	if err != nil {
		return defaultDNS
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "nameserver") {
			continue
		}
		addr := strings.TrimSpace(strings.TrimPrefix(line, "nameserver"))
		if addr != "" && !strings.HasPrefix(addr, "127.") && addr != "::1" {
			return addr
		}
	}
	return defaultDNS
}

func (l *linuxPlatform) applyRouting(ifName string, p Params) error {
	l.tunName = ifName
	// Назначаем адрес и поднимаем интерфейс.
	if err := run("ip", "addr", "add", fmt.Sprintf("%s/%d", p.TunAddr, p.Prefix), "dev", ifName); err != nil {
		return err
	}
	if err := run("ip", "link", "set", "dev", ifName, "up"); err != nil {
		return err
	}
	// Перекрываем маршрут по умолчанию: добавляем default через TUN с меньшей
	// метрикой (выше приоритет), сохраняя исходный физический default в таблице —
	// им пользуются сокеты, привязанные к физическому интерфейсу (см. protector).
	if err := run("ip", "route", "add", "default", "dev", ifName, "metric", "1"); err != nil {
		return err
	}
	l.addedDefault = true
	l.writeJournal()
	return nil
}

// applyDNS переписывает /etc/resolv.conf на наш резолвер, запоминая исходник
// (включая случай, когда это симлинк на systemd-resolved).
func (l *linuxPlatform) applyDNS(dns string) error {
	if dest, err := os.Readlink(resolvConfPath); err == nil {
		l.resolvWasLink = true
		l.resolvLinkDest = dest
		l.resolvExisted = true
	} else if data, err := os.ReadFile(resolvConfPath); err == nil {
		l.resolvBackup = data
		l.resolvExisted = true
	}
	// Журналируем ДО изменения: если процесс умрёт сразу после записи,
	// откат всё равно возможен при следующем запуске.
	l.dnsReconfigured = true
	l.writeJournal()

	_ = os.Remove(resolvConfPath)
	content := fmt.Sprintf("# BoardProxy TUN\nnameserver %s\n", dns)
	if err := os.WriteFile(resolvConfPath, []byte(content), 0o644); err != nil {
		l.dnsReconfigured = false
		l.writeJournal()
		return fmt.Errorf("tun: write resolv.conf: %w", err)
	}
	return nil
}

// writeJournal сохраняет состояние отката на диск (страховка от аварийного
// завершения; см. journal.go).
func (l *linuxPlatform) writeJournal() {
	j := journal{TunName: l.tunName, DefaultRoute: l.addedDefault}
	if l.dnsReconfigured {
		j.DNSBackup = string(l.resolvBackup)
		j.ResolvLink = l.resolvLinkDest
	}
	j.save()
}

// recoverStaleState восстанавливает настройки, оставшиеся от аварийно
// завершившегося запуска.
func recoverStaleState() {
	j := loadJournal()
	if j == nil {
		return
	}
	if j.DNSBackup != "" || j.ResolvLink != "" {
		_ = os.Remove(resolvConfPath)
		if j.ResolvLink != "" {
			_ = os.Symlink(j.ResolvLink, resolvConfPath)
		} else {
			_ = os.WriteFile(resolvConfPath, []byte(j.DNSBackup), 0o644)
		}
	}
	if j.DefaultRoute && j.TunName != "" {
		_ = run("ip", "route", "del", "default", "dev", j.TunName, "metric", "1")
	}
	clearJournal()
}

func (l *linuxPlatform) revertRouting() error {
	var firstErr error
	if l.dnsReconfigured {
		_ = os.Remove(resolvConfPath)
		switch {
		case l.resolvWasLink:
			if err := os.Symlink(l.resolvLinkDest, resolvConfPath); err != nil {
				firstErr = err
			}
		case l.resolvExisted:
			if err := os.WriteFile(resolvConfPath, l.resolvBackup, 0o644); err != nil {
				firstErr = err
			}
		}
		l.dnsReconfigured = false
	}
	if l.addedDefault {
		// Маршрут исчезнет вместе с интерфейсом, но удаляем явно на случай гонки.
		if err := run("ip", "route", "del", "default", "dev", l.tunName, "metric", "1"); err != nil && firstErr == nil {
			firstErr = err
		}
		l.addedDefault = false
	}
	clearJournal()
	return firstErr
}

// run выполняет системную команду, включая её вывод в текст ошибки.
func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tun: %s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
