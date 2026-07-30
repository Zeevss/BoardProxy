// Пакет bypass решает, какие адреса пускать мимо туннеля — напрямую в сеть.
// Список Go-regexp'ов, каждый проверяется против хоста цели. Список можно
// заменять на лету (Update) без пересоздания клиента — на этом строится
// реактивное обновление bypass при изменении конфига (по образцу ICMP).
package bypass

import (
	"fmt"
	"net"
	"regexp"
	"sync"
)

// Matcher — потокобезопасный набор regexp-паттернов bypass.
type Matcher struct {
	mu       sync.RWMutex
	patterns []*regexp.Regexp
}

// New компилирует стартовый список паттернов. Пустой список — валиден (bypass
// никогда не срабатывает, весь трафик идёт в туннель).
func New(patterns []string) (*Matcher, error) {
	m := &Matcher{}
	return m, m.Update(patterns)
}

// Update атомарно заменяет список паттернов. Если хоть один паттерн не
// компилируется — возвращает ошибку и НЕ трогает текущий список (частичная
// замена недопустима: конфиг применяется целиком или никак).
func (m *Matcher) Update(patterns []string) error {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		r, err := regexp.Compile(p)
		if err != nil {
			return fmt.Errorf("bypass: некорректный regexp %q: %w", p, err)
		}
		compiled = append(compiled, r)
	}
	m.mu.Lock()
	m.patterns = compiled
	m.mu.Unlock()
	return nil
}

// Match сообщает, попадает ли цель под bypass. Принимает как "host:port", так и
// голый host — порт отбрасывается, паттерны проверяются против хоста.
func (m *Matcher) Match(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, r := range m.patterns {
		if r.MatchString(host) {
			return true
		}
	}
	return false
}
