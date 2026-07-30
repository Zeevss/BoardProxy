package board

import "errors"

var (
	// ErrClosed is returned by operations on a session that has been closed.
	ErrClosed = errors.New("board: session closed")
	// ErrNotSubscribed is returned by Put/Delete before Subscribe has been called.
	ErrNotSubscribed = errors.New("board: not subscribed to a page")
)
