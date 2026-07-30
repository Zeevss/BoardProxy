// Package socketio is a minimal Engine.IO v4 / Socket.IO client speaking only
// what BoardProxy needs against Yandex Board: the pure-websocket transport
// (EIO=4), the default namespace, event emits with ack correlation, and
// ping/pong. It deliberately avoids a full Socket.IO implementation.
package socketio

import (
	"strconv"
)

// Engine.IO packet types (the first character of a websocket text frame).
const (
	eioOpen    = '0'
	eioClose   = '1'
	eioPing    = '2'
	eioPong    = '3'
	eioMessage = '4' // wraps a Socket.IO packet
)

// Socket.IO packet types (the character after eioMessage).
const (
	sioConnect      = '0'
	sioDisconnect   = '1'
	sioEvent        = '2'
	sioAck          = '3'
	sioConnectError = '4'
)

// packet is a decoded incoming websocket text frame.
type packet struct {
	engine byte // one of the eio* constants
	sio    byte // valid only when engine == eioMessage
	ackID  int  // ack correlation id; -1 when absent
	body   []byte
}

// parsePacket decodes a raw Engine.IO/Socket.IO text frame.
//
// Layout for message frames: '4' <sioType> [ackDigits] [jsonBody].
// Examples: "40{...}" connect, "42[...]" event, "42<id>[...]" event-with-ack,
// "43<id>[...]" ack. Non-message frames ("0{...}", "2", "3") carry no sio type.
func parsePacket(raw []byte) packet {
	p := packet{ackID: -1}
	if len(raw) == 0 {
		return p
	}
	p.engine = raw[0]
	if p.engine != eioMessage {
		p.body = raw[1:]
		return p
	}
	if len(raw) < 2 {
		return p
	}
	p.sio = raw[1]
	rest := raw[2:]

	// Optional leading digits are the ack id (present on EVENT/ACK frames).
	i := 0
	for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
		i++
	}
	if i > 0 {
		if id, err := strconv.Atoi(string(rest[:i])); err == nil {
			p.ackID = id
		}
	}
	p.body = rest[i:]
	return p
}

// encodeEvent builds an EVENT frame, optionally carrying an ack id so the
// server echoes it back on its ACK.
func encodeEvent(ackID int, payload []byte) []byte {
	head := []byte{eioMessage, sioEvent}
	if ackID >= 0 {
		head = append(head, []byte(strconv.Itoa(ackID))...)
	}
	return append(head, payload...)
}
