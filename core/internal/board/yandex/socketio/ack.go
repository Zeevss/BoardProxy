package socketio

import (
	"encoding/json"
	"sync"
)

// ackRegistry correlates emitted events that expect an ack with the incoming
// ACK frame carrying the same id.
type ackRegistry struct {
	mu      sync.Mutex
	next    int
	waiters map[int]chan []json.RawMessage
}

func newAckRegistry() *ackRegistry {
	return &ackRegistry{next: 1, waiters: make(map[int]chan []json.RawMessage)}
}

// register allocates an ack id and its one-shot delivery channel.
func (r *ackRegistry) register() (int, chan []json.RawMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := r.next
	r.next++
	ch := make(chan []json.RawMessage, 1)
	r.waiters[id] = ch
	return id, ch
}

// resolve delivers args to the waiter for id, if any, and forgets it.
func (r *ackRegistry) resolve(id int, args []json.RawMessage) {
	r.mu.Lock()
	ch, ok := r.waiters[id]
	delete(r.waiters, id)
	r.mu.Unlock()
	if ok {
		ch <- args
	}
}

// cancel forgets a waiter (e.g. on timeout) so its id cannot be resolved later.
func (r *ackRegistry) cancel(id int) {
	r.mu.Lock()
	delete(r.waiters, id)
	r.mu.Unlock()
}

// failAll delivers a nil result to every outstanding waiter, unblocking callers
// when the connection dies.
func (r *ackRegistry) failAll() {
	r.mu.Lock()
	waiters := r.waiters
	r.waiters = make(map[int]chan []json.RawMessage)
	r.mu.Unlock()
	for _, ch := range waiters {
		ch <- nil
	}
}
