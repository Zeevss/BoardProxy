// Package events contains the in-process wake-up mechanism used by connected
// node streams. Durable desired state remains in the repository; notifications
// are hints and periodic reconciliation is the recovery path.
package events

import "sync"

type Bus struct {
	mu          sync.Mutex
	nextID      uint64
	subscribers map[string]map[uint64]chan struct{}
}

func New() *Bus { return &Bus{subscribers: make(map[string]map[uint64]chan struct{})} }

func (b *Bus) Subscribe(nodeID string) (<-chan struct{}, func()) {
	b.mu.Lock()
	b.nextID++
	id := b.nextID
	channel := make(chan struct{}, 1)
	if b.subscribers[nodeID] == nil {
		b.subscribers[nodeID] = make(map[uint64]chan struct{})
	}
	b.subscribers[nodeID][id] = channel
	b.mu.Unlock()
	var once sync.Once
	return channel, func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subscribers[nodeID], id)
			if len(b.subscribers[nodeID]) == 0 {
				delete(b.subscribers, nodeID)
			}
			b.mu.Unlock()
		})
	}
}

func (b *Bus) Notify(nodeID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, subscriber := range b.subscribers[nodeID] {
		select {
		case subscriber <- struct{}{}:
		default: // one queued wake-up is sufficient; desired state is durable
		}
	}
}
