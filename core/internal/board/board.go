// Package board is the L0 ("PHY") transport abstraction of BoardProxy.
//
// A Session is one guest connection subscribed to exactly one page (slide) of a
// whiteboard. Everything above this layer (codec, link, mux) talks only to this
// interface and never to Socket.IO or Yandex specifics, so the real driver
// (board/yandex) and the in-process test double (board/memory) are
// interchangeable.
//
// The transport treats a whiteboard object as an opaque triple: an id, a text
// value carrying our codec payload, and the participant hash of whoever created
// it. Object creation delivers a packet; object deletion is its acknowledgement.
package board

import "context"

// Object is one whiteboard object (an mxCell) as the transport sees it.
type Object struct {
	// ID is the board object id. On Yandex it equals the object hash.
	ID string
	// Value is the text value; our codec payload lives here.
	Value string
	// CreatorHash is the participant hash of whoever created the object.
	CreatorHash string
}

// EventKind distinguishes object creation from deletion.
type EventKind int

const (
	// Created is emitted when a peer creates an object on the page.
	Created EventKind = iota
	// Deleted is emitted when any object on the page is removed. For our own
	// objects this deletion is the ACK; the id is always set, CreatorHash may
	// be empty since the object is already gone.
	Deleted
)

func (k EventKind) String() string {
	switch k {
	case Created:
		return "created"
	case Deleted:
		return "deleted"
	default:
		return "unknown"
	}
}

// Event is a change observed on the subscribed page.
type Event struct {
	Kind   EventKind
	Object Object
}

// Session is one guest connection subscribed to a single page.
//
// A driver delivers events for changes made by *other* participants, plus
// deletions of this session's own objects (needed for ACK); it does not echo
// this session's own creations back. Upper layers use Participant to attribute
// objects (peer vs self) where a finer distinction is needed.
type Session interface {
	// Participant returns this session's own participant hash. Objects created
	// by this session carry it as CreatorHash.
	Participant() string

	// Subscribe joins a page and returns the current snapshot of objects on it.
	// It may be called again to switch pages or to re-sync after a reconnect.
	Subscribe(ctx context.Context, page string) ([]Object, error)

	// Put creates (or replaces) an object on the currently subscribed page.
	// It returns once the board has acknowledged the write.
	Put(ctx context.Context, obj Object) error

	// Delete removes one or more objects by id from the currently subscribed
	// page. Drivers that support it send all ids in a single wire call.
	Delete(ctx context.Context, ids ...string) error

	// Events streams changes on the subscribed page. The channel is closed when
	// the session is closed or its transport reaches a terminal failure.
	Events() <-chan Event

	// Reconnects streams a fresh page snapshot each time the session
	// transparently re-establishes its underlying connection (see the yandex
	// driver's auto-reconnect). The reliability layer above (link) consumes it
	// to reconcile: free slots for objects acked during the gap and replay any
	// peer objects it missed. Drivers that never reconnect return a nil channel,
	// which simply never fires.
	Reconnects() <-chan []Object

	// Close tears down the session and releases its resources.
	Close() error
}
