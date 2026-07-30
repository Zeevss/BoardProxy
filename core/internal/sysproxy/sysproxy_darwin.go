//go:build darwin

package sysproxy

import (
	"bufio"
	"fmt"
	"net"
	"os/exec"
	"strings"
)

// В macOS прокси настраивается через `networksetup` покомандно и привязан к
// сетевому сервису (Wi-Fi, Ethernet, …). Ставим SOCKS + HTTP + HTTPS прокси на
// все включённые сервисы, предварительно запомнив прежнее состояние каждого,
// чтобы Unset его точно восстановил.

// proxyKind — тройка подкоманд networksetup для одного типа прокси.
type proxyKind struct {
	get, set, state string
}

var kinds = []proxyKind{
	{"-getsocksfirewallproxy", "-setsocksfirewallproxy", "-setsocksfirewallproxystate"},
	{"-getwebproxy", "-setwebproxy", "-setwebproxystate"},
	{"-getsecurewebproxy", "-setsecurewebproxy", "-setsecurewebproxystate"},
}

// savedState — прежнее состояние одного (сервис × тип прокси), снятое в Set.
type savedState struct {
	service string
	kind    proxyKind
	enabled bool
	server  string
	port    string
}

// saved хранит состояние, снятое последним Set, для восстановления в Unset.
var saved []savedState

// Set включает прокси на addr для всех активных сетевых сервисов, запомнив их
// прежнее состояние.
func Set(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	services, err := activeServices()
	if err != nil {
		return err
	}
	if len(services) == 0 {
		return fmt.Errorf("sysproxy: активных сетевых сервисов не найдено")
	}
	saved = nil
	for _, svc := range services {
		for _, k := range kinds {
			st, err := readState(svc, k)
			if err != nil {
				return err
			}
			saved = append(saved, st)
			if err := run("networksetup", k.set, svc, host, port); err != nil {
				return err
			}
			if err := run("networksetup", k.state, svc, "on"); err != nil {
				return err
			}
		}
	}
	return nil
}

// Unset восстанавливает состояние прокси, снятое в Set. Возвращает первую
// встреченную ошибку, но пытается восстановить всё остальное.
func Unset() error {
	var firstErr error
	fail := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	for _, s := range saved {
		if s.enabled {
			// Был включён — вернём прежние адрес/порт и оставим включённым.
			fail(run("networksetup", s.kind.set, s.service, s.server, s.port))
			fail(run("networksetup", s.kind.state, s.service, "on"))
		} else {
			// Был выключен — просто выключаем (наш адрес остаётся, но неактивен).
			fail(run("networksetup", s.kind.state, s.service, "off"))
		}
	}
	saved = nil
	return firstErr
}

// activeServices возвращает включённые сетевые сервисы. Отключённые в выводе
// `-listallnetworkservices` помечены звёздочкой в начале строки; первая строка —
// пояснительный заголовок.
func activeServices() ([]string, error) {
	out, err := exec.Command("networksetup", "-listallnetworkservices").Output()
	if err != nil {
		return nil, fmt.Errorf("networksetup -listallnetworkservices: %w", err)
	}
	var services []string
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	first := true
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if first {
			first = false // строка-заголовок про звёздочку
			continue
		}
		if line == "" || strings.HasPrefix(line, "*") {
			continue // пустая строка или отключённый сервис
		}
		services = append(services, line)
	}
	return services, sc.Err()
}

// readState снимает текущее состояние одного типа прокси для сервиса. Вывод
// networksetup -getXXXproxy — строки вида "Enabled: No", "Server: ", "Port: 0".
func readState(service string, k proxyKind) (savedState, error) {
	out, err := exec.Command("networksetup", k.get, service).Output()
	if err != nil {
		return savedState{}, fmt.Errorf("networksetup %s %q: %w", k.get, service, err)
	}
	st := savedState{service: service, kind: k}
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		key, val, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		switch strings.TrimSpace(key) {
		case "Enabled":
			st.enabled = strings.EqualFold(val, "Yes")
		case "Server":
			st.server = val
		case "Port":
			st.port = val
		}
	}
	// networksetup -setXXXproxy требует непустой сервер; для восстановления
	// выключенного состояния сервер не используется, но подстрахуемся.
	if st.server == "" {
		st.server = "0.0.0.0"
	}
	if st.port == "" {
		st.port = "0"
	}
	return st, nil
}

func run(name string, args ...string) error {
	if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("%s %v: %w (%s)", name, args, err, out)
	}
	return nil
}
