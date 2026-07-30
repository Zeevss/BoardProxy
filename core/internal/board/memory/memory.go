// Package memory is an in-process implementation of board.Session used to test
// the layers above L0 without touching a real whiteboard or the network.
//
// A Board holds named pages in memory. Sessions created from the same Board and
// subscribed to the same page see each other's changes, mimicking the real
// board's broadcast semantics: an event caused by participant P is delivered to
// every other subscriber of the page, never back to P (the author learns its
// own write succeeded from Put/Delete returning, and learns its object was
// deleted — the ACK — because the deleter is someone else).
package memory

import (
	"context"
	"sync"

	"bproxy-core/internal/board"
)

// eventBuffer is how many events a subscriber may lag before Put/Delete blocks.
const eventBuffer = 1024

// Board is an in-memory whiteboard shared by test peers.
type Board struct {
	mu    sync.Mutex
	pages map[string]*page
}

// NewBoard returns an empty in-memory whiteboard.
func NewBoard() *Board {
	return &Board{pages: make(map[string]*page)}
}

func (b *Board) page(name string) *page {
	b.mu.Lock()
	defer b.mu.Unlock()
	p, ok := b.pages[name]
	if !ok {
		p = &page{objects: make(map[string]board.Object)}
		b.pages[name] = p
	}
	return p
}

// NewSession returns a guest session on this board identified by participant.
func (b *Board) NewSession(participant string) *Session {
	return &Session{
		board:       b,
		participant: participant,
		events:      make(chan board.Event, eventBuffer),
	}
}

// page is a single shared whiteboard page.
type page struct {
	mu          sync.Mutex
	objects     map[string]board.Object
	subscribers []*Session
}

func (p *page) subscribe(s *Session) []board.Object {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.subscribers = append(p.subscribers, s)
	snapshot := make([]board.Object, 0, len(p.objects))
	for _, o := range p.objects {
		snapshot = append(snapshot, o)
	}
	return snapshot
}

func (p *page) unsubscribe(s *Session) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, sub := range p.subscribers {
		if sub == s {
			p.subscribers = append(p.subscribers[:i], p.subscribers[i+1:]...)
			return
		}
	}
}

// emit records the mutation and broadcasts the event to every subscriber except
// the author, matching the real board's "broadcast to others" behaviour.
func (p *page) emit(author *Session, ev board.Event) {
	p.mu.Lock()
	switch ev.Kind {
	case board.Created:
		p.objects[ev.Object.ID] = ev.Object
	case board.Deleted:
		delete(p.objects, ev.Object.ID)
	}
	subs := make([]*Session, len(p.subscribers))
	copy(subs, p.subscribers)
	p.mu.Unlock()

	for _, s := range subs {
		if s == author {
			continue
		}
		s.deliver(ev)
	}
}

// Session is an in-memory board.Session.
type Session struct {
	board       *Board
	participant string

	mu      sync.Mutex
	current *page

	// sendMu guards the events channel and the closed flag so a concurrent
	// Close never closes the channel while a broadcast is delivering into it
	// (a send-on-closed-channel race).
	sendMu sync.RWMutex
	closed bool
	events chan board.Event
}

var _ board.Session = (*Session)(nil)

// deliver pushes an event to this session unless it is closed.
func (s *Session) deliver(ev board.Event) {
	s.sendMu.RLock()
	defer s.sendMu.RUnlock()
	if s.closed {
		return
	}
	s.events <- ev
}

func (s *Session) Participant() string { return s.participant }

func (s *Session) Subscribe(_ context.Context, name string) ([]board.Object, error) {
	next := s.board.page(name)
	s.mu.Lock()
	prev := s.current
	s.current = next
	s.mu.Unlock()
	if prev != nil && prev != next {
		prev.unsubscribe(s)
	}
	return next.subscribe(s), nil
}

func (s *Session) Put(_ context.Context, obj board.Object) error {
	p, err := s.currentPage()
	if err != nil {
		return err
	}
	obj.CreatorHash = s.participant
	p.emit(s, board.Event{Kind: board.Created, Object: obj})
	return nil
}

func (s *Session) Delete(_ context.Context, ids ...string) error {
	if len(ids) == 0 {
		return nil
	}
	p, err := s.currentPage()
	if err != nil {
		return err
	}
	for _, id := range ids {
		p.emit(s, board.Event{Kind: board.Deleted, Object: board.Object{ID: id}})
	}
	return nil
}

func (s *Session) Events() <-chan board.Event { return s.events }

// Reconnects never fires: the in-memory session has no underlying connection to
// drop, so there is nothing to reconcile. A nil channel blocks forever in a
// select, which is exactly the "never reconnects" contract.
func (s *Session) Reconnects() <-chan []board.Object { return nil }

func (s *Session) Close() error {
	// Unsubscribe first so no further broadcast targets this session, then close
	// the channel under sendMu so no in-flight deliver races the close.
	s.mu.Lock()
	cur := s.current
	s.current = nil
	s.mu.Unlock()
	if cur != nil {
		cur.unsubscribe(s)
	}

	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	close(s.events)
	return nil
}

func (s *Session) currentPage() (*page, error) {
	s.sendMu.RLock()
	closed := s.closed
	s.sendMu.RUnlock()
	if closed {
		return nil, board.ErrClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		return nil, board.ErrNotSubscribed
	}
	return s.current, nil
}
