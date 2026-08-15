// Package runtimeevents contains the bounded, process-local event journal used
// by the core management stream. It is deliberately not durable: node-agent is
// responsible for at-least-once forwarding and a stream gap forces a snapshot.
package runtimeevents

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

type Type string

const (
	ResourceChanged     Type = "resource_changed"
	BoardStateChanged   Type = "board_state_changed"
	ClientSessionOpened Type = "client_session_opened"
	ClientSessionClosed Type = "client_session_closed"
)

type Event struct {
	ID              string
	BootID          string
	Sequence        uint64
	OccurredAt      time.Time
	RuntimeRevision uint64
	Type            Type

	ResourceKind      string
	ResourceOperation string
	Tag               string

	BoardTag      string
	PreviousState string
	State         string
	Error         string

	UserTag  string
	BundleID string
	RXBytes  uint64
	TXBytes  uint64
	Reason   string
}

type Reset struct {
	Reason                  string
	OldestAvailableSequence uint64
	LatestSequence          uint64
}

type Subscription struct {
	BootID string
	Replay []Event
	Reset  *Reset
	Events <-chan Event
	Close  func()
}

type Journal struct {
	mu          sync.Mutex
	bootID      string
	capacity    int
	next        uint64
	events      []Event
	nextSubID   uint64
	subscribers map[uint64]chan Event
}

func New(capacity int) *Journal {
	if capacity < 1 {
		capacity = 1
	}
	return &Journal{
		bootID: randomBootID(), capacity: capacity,
		subscribers: make(map[uint64]chan Event),
	}
}

func (j *Journal) Position() (string, uint64) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.bootID, j.next
}

func (j *Journal) Publish(event Event) Event {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.next++
	event.Sequence = j.next
	event.BootID = j.bootID
	event.ID = fmt.Sprintf("%s:%d", j.bootID, event.Sequence)
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	} else {
		event.OccurredAt = event.OccurredAt.UTC()
	}
	j.events = append(j.events, event)
	if len(j.events) > j.capacity {
		copy(j.events, j.events[len(j.events)-j.capacity:])
		j.events = j.events[:j.capacity]
	}
	for id, subscriber := range j.subscribers {
		select {
		case subscriber <- event:
		default:
			// A slow management consumer must never stall the data plane. Closing
			// forces it to reconnect and either replay the ring or observe a gap.
			close(subscriber)
			delete(j.subscribers, id)
		}
	}
	return event
}

func (j *Journal) Subscribe(bootID string, afterSequence uint64) Subscription {
	j.mu.Lock()
	defer j.mu.Unlock()
	latest := j.next
	oldest := latest + 1
	if len(j.events) > 0 {
		oldest = j.events[0].Sequence
	}
	var reset *Reset
	if bootID != "" && bootID != j.bootID {
		reset = &Reset{Reason: "core_restarted", OldestAvailableSequence: oldest, LatestSequence: latest}
		afterSequence = 0
	} else if afterSequence > latest {
		reset = &Reset{Reason: "cursor_ahead", OldestAvailableSequence: oldest, LatestSequence: latest}
		afterSequence = 0
	} else if afterSequence+1 < oldest {
		reset = &Reset{Reason: "event_gap", OldestAvailableSequence: oldest, LatestSequence: latest}
		afterSequence = oldest - 1
	}
	replay := make([]Event, 0, len(j.events))
	for _, event := range j.events {
		if event.Sequence > afterSequence {
			replay = append(replay, event)
		}
	}
	j.nextSubID++
	id := j.nextSubID
	stream := make(chan Event, 128)
	j.subscribers[id] = stream
	var once sync.Once
	return Subscription{
		BootID: j.bootID, Replay: replay, Reset: reset, Events: stream,
		Close: func() {
			once.Do(func() {
				j.mu.Lock()
				if existing, ok := j.subscribers[id]; ok {
					delete(j.subscribers, id)
					close(existing)
				}
				j.mu.Unlock()
			})
		},
	}
}

func randomBootID() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err == nil {
		return hex.EncodeToString(raw)
	}
	return fmt.Sprintf("boot-%d", time.Now().UnixNano())
}
