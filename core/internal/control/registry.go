// Package control contains the in-memory desired-state registry used by the
// server data plane. It has no filesystem or database dependency.
package control

import (
	"context"
	"encoding/base64"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"bproxy-core/internal/hub"
	"bproxy-core/internal/serverconfig"
)

var ErrUnauthorized = errors.New("control: user is not authorized")

type userPolicy struct {
	user    hub.User
	public  []byte
	enabled bool
	boards  map[string]bool
}

type policySnapshot struct {
	byPublic map[string]userPolicy
	byID     map[string]userPolicy
}

type runtimeUser struct {
	active   int
	rx       uint64
	tx       uint64
	lastSeen time.Time
}

// Registry provides lock-free authorization reads and a small locked runtime
// accounting map. Replacing desired policy never erases since-start counters.
type Registry struct {
	policy  atomic.Pointer[policySnapshot]
	mu      sync.Mutex
	runtime map[string]*runtimeUser
}

type UserView struct {
	ID             string
	Name           string
	Enabled        bool
	PublicKey      []byte
	Boards         []string
	MaxSessions    int
	MaxLanes       int
	ActiveSessions int
	RXBytes        uint64
	TXBytes        uint64
	LastSeen       time.Time
}

func NewRegistry(users []serverconfig.User) (*Registry, error) {
	r := &Registry{runtime: make(map[string]*runtimeUser)}
	if err := r.Replace(users); err != nil {
		return nil, err
	}
	return r, nil
}

func compileUsers(users []serverconfig.User) (*policySnapshot, error) {
	s := &policySnapshot{
		byPublic: make(map[string]userPolicy, len(users)),
		byID:     make(map[string]userPolicy, len(users)),
	}
	for _, cfg := range users {
		identity, err := cfg.Identity()
		if err != nil {
			return nil, err
		}
		boards := make(map[string]bool, len(cfg.Boards))
		for _, board := range cfg.Boards {
			boards[board] = true
		}
		policy := userPolicy{
			user: hub.User{
				ID: cfg.Tag, Name: cfg.Name,
				MaxSessions: cfg.MaxSessions, MaxLanes: cfg.MaxLanes,
			},
			public:  append([]byte(nil), identity.Public...),
			enabled: cfg.IsEnabled(),
			boards:  boards,
		}
		s.byID[cfg.Tag] = policy
		s.byPublic[base64.RawStdEncoding.EncodeToString(identity.Public)] = policy
	}
	return s, nil
}

func (r *Registry) Replace(users []serverconfig.User) error {
	s, err := compileUsers(users)
	if err != nil {
		return err
	}
	r.policy.Store(s)
	return nil
}

func (r *Registry) Authorize(_ context.Context, publicKey []byte, boardTag string) (hub.User, error) {
	s := r.policy.Load()
	if s == nil {
		return hub.User{}, ErrUnauthorized
	}
	p, ok := s.byPublic[base64.RawStdEncoding.EncodeToString(publicKey)]
	if !ok || !p.enabled || !p.boards[boardTag] {
		return hub.User{}, ErrUnauthorized
	}
	return p.user, nil
}

func (r *Registry) AcquireSession(userID string) bool {
	s := r.policy.Load()
	if s == nil {
		return false
	}
	p, ok := s.byID[userID]
	if !ok || !p.enabled {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	ru := r.runtime[userID]
	if ru == nil {
		ru = &runtimeUser{}
		r.runtime[userID] = ru
	}
	if p.user.MaxSessions > 0 && ru.active >= p.user.MaxSessions {
		return false
	}
	ru.active++
	ru.lastSeen = time.Now()
	return true
}

func (r *Registry) ReleaseSession(userID string, rx, tx uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ru := r.runtime[userID]
	if ru == nil {
		ru = &runtimeUser{}
		r.runtime[userID] = ru
	}
	if ru.active > 0 {
		ru.active--
	}
	ru.rx += rx
	ru.tx += tx
}

func (r *Registry) List() []UserView {
	s := r.policy.Load()
	if s == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]UserView, 0, len(s.byID))
	for id, p := range s.byID {
		boards := make([]string, 0, len(p.boards))
		for board := range p.boards {
			boards = append(boards, board)
		}
		sort.Strings(boards)
		view := UserView{
			ID: id, Name: p.user.Name, Enabled: p.enabled,
			PublicKey: append([]byte(nil), p.public...), Boards: boards,
			MaxSessions: p.user.MaxSessions, MaxLanes: p.user.MaxLanes,
		}
		if ru := r.runtime[id]; ru != nil {
			view.ActiveSessions = ru.active
			view.RXBytes = ru.rx
			view.TXBytes = ru.tx
			view.LastSeen = ru.lastSeen
		}
		out = append(out, view)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (r *Registry) ActiveSessions(userID string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ru := r.runtime[userID]; ru != nil {
		return ru.active
	}
	return 0
}

// Totals returns traffic from every completed session since process start,
// including users that have since been removed from desired state. This keeps
// global since-start counters monotonic without retaining secret policy data.
func (r *Registry) Totals() (rx, tx uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, user := range r.runtime {
		rx += user.rx
		tx += user.tx
	}
	return rx, tx
}
