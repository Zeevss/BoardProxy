package socketio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// readLimit caps one decompressed WebSocket message. Real board state packets
// can exceed 16 MiB; 64 MiB lets the page lifecycle inspect and clean them
// while retaining a hard memory bound.
const readLimit = 64 << 20

const writeTimeout = 10 * time.Second

const (
	eventQueueCapacity = 4096
	eventQueueMaxBytes = 256 << 20
)

// heartbeatGrace tolerates scheduler and network jitter on top of the
// Engine.IO-advertised ping interval + timeout. A missing ping beyond this
// deadline means the underlying TCP path is stale (sleep/interface switch),
// even when the OS has not reported EOF yet.
var heartbeatGrace = 5 * time.Second

var (
	// ErrClosed is returned by Emit once the client is closed.
	ErrClosed = errors.New("socketio: client closed")
	// ErrConnClosed is returned when the connection dropped while awaiting an ack.
	ErrConnClosed = errors.New("socketio: connection closed while awaiting ack")
	// ErrEventBacklog forces a controlled reconnect when application events are
	// no longer being drained. The Engine.IO reader must remain able to pong.
	ErrEventBacklog = errors.New("socketio: application event backlog overflow")
)

// IsMessageTooBig identifies a local read-limit failure without leaking the
// websocket implementation into the board session package.
func IsMessageTooBig(err error) bool { return errors.Is(err, websocket.ErrMessageTooBig) }

// ReadLimit exposes the configured safety bound for diagnostics.
func ReadLimit() int64 { return readLimit }

// Message is a server-initiated Socket.IO event: the event name and its
// remaining JSON arguments.
type Message struct {
	Event string
	Args  []json.RawMessage
}

type queuedMessage struct {
	message Message
	bytes   int64
}

// Client is a minimal Socket.IO (default namespace) connection over websocket.
type Client struct {
	conn         *websocket.Conn
	acks         *ackRegistry
	incoming     chan Message
	events       chan queuedMessage
	eventBytes   atomic.Int64
	dispatchDone chan struct{}

	writeMu sync.Mutex

	ctx    context.Context
	cancel context.CancelFunc

	closeOnce        sync.Once
	closeErr         error
	done             chan struct{}
	heartbeatTimeout time.Duration
}

// Dial connects to a Socket.IO server, forwarding cookie on the handshake
// (required by Yandex Board, SPEC §5.1), and completes the Engine.IO open plus
// default-namespace connect before returning.
func Dial(ctx context.Context, baseURL, cookie string, httpClient *http.Client) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse socket url: %w", err)
	}
	switch u.Scheme {
	case "https", "wss":
		u.Scheme = "wss"
	case "http", "ws":
		u.Scheme = "ws"
	default:
		return nil, fmt.Errorf("unsupported socket url scheme %q", u.Scheme)
	}
	u.Path = "/socket.io/"
	q := u.Query()
	q.Set("EIO", "4")
	q.Set("transport", "websocket")
	u.RawQuery = q.Encode()

	hdr := http.Header{}
	if cookie != "" {
		hdr.Set("Cookie", cookie)
	}
	hdr.Set("Origin", "https://boards.yandex.ru")

	conn, _, err := websocket.Dial(ctx, u.String(), &websocket.DialOptions{
		HTTPClient: httpClient,
		HTTPHeader: hdr,
	})
	if err != nil {
		return nil, fmt.Errorf("ws dial: %w", err)
	}
	conn.SetReadLimit(readLimit)

	c := &Client{
		conn:         conn,
		acks:         newAckRegistry(),
		incoming:     make(chan Message, 256),
		events:       make(chan queuedMessage, eventQueueCapacity),
		dispatchDone: make(chan struct{}),
		done:         make(chan struct{}),
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())

	if err := c.handshake(ctx); err != nil {
		_ = conn.Close(websocket.StatusProtocolError, "handshake failed")
		return nil, err
	}

	go c.dispatchLoop()
	go c.readLoop()
	return c, nil
}

// handshake reads the Engine.IO open packet, sends the default-namespace
// connect, and waits for its confirmation, answering pings meanwhile.
func (c *Client) handshake(ctx context.Context) error {
	raw, err := c.readFrame(ctx)
	if err != nil {
		return fmt.Errorf("read open: %w", err)
	}
	p := parsePacket(raw)
	if p.engine != eioOpen {
		return fmt.Errorf("expected open packet, got %q", string(raw))
	}
	timeout, err := parseHeartbeatTimeout(p.body)
	if err != nil {
		return err
	}
	c.heartbeatTimeout = timeout

	if err := c.writeFrame(ctx, []byte{eioMessage, sioConnect}); err != nil {
		return fmt.Errorf("send connect: %w", err)
	}
	for {
		raw, err := c.readFrame(ctx)
		if err != nil {
			return fmt.Errorf("await connect: %w", err)
		}
		p := parsePacket(raw)
		switch {
		case p.engine == eioPing:
			if err := c.writeFrame(ctx, []byte{eioPong}); err != nil {
				return err
			}
		case p.engine == eioMessage && p.sio == sioConnect:
			return nil
		case p.engine == eioMessage && p.sio == sioConnectError:
			return fmt.Errorf("connect error: %s", string(p.body))
		}
	}
}

func parseHeartbeatTimeout(body []byte) (time.Duration, error) {
	var open struct {
		PingInterval int64 `json:"pingInterval"`
		PingTimeout  int64 `json:"pingTimeout"`
	}
	if err := json.Unmarshal(body, &open); err != nil {
		return 0, fmt.Errorf("decode open packet: %w", err)
	}
	if open.PingInterval > 0 && open.PingTimeout > 0 {
		return time.Duration(open.PingInterval+open.PingTimeout)*time.Millisecond + heartbeatGrace, nil
	}
	return 0, errors.New("socketio: open packet has invalid heartbeat settings")
}

// Emit sends an event with an ack and blocks until the server acks, the context
// is done, or the connection closes. It returns the ack's JSON arguments.
func (c *Client) Emit(ctx context.Context, event string, arg any) ([]json.RawMessage, error) {
	payload, err := json.Marshal([]any{event, arg})
	if err != nil {
		return nil, fmt.Errorf("marshal event %s: %w", event, err)
	}
	id, ch := c.acks.register()
	if err := c.writeFrame(ctx, encodeEvent(id, payload)); err != nil {
		c.acks.cancel(id)
		return nil, err
	}
	select {
	case args := <-ch:
		if args == nil {
			return nil, ErrConnClosed
		}
		return args, nil
	case <-ctx.Done():
		c.acks.cancel(id)
		return nil, ctx.Err()
	case <-c.done:
		c.acks.cancel(id)
		return nil, ErrClosed
	}
}

// Events returns the stream of server-initiated events. It is closed when the
// client closes.
func (c *Client) Events() <-chan Message { return c.incoming }

// Err returns the terminal reason after the event stream has closed. It is
// used by the board session to distinguish heartbeat timeouts, peer closes and
// local shutdowns in transport diagnostics.
func (c *Client) Err() error {
	<-c.done
	return c.closeErr
}

// Close shuts the connection down and waits for the read loop to finish.
func (c *Client) Close() error {
	c.fail(ErrClosed)
	<-c.done
	return c.closeErr
}

func (c *Client) readLoop() {
	var loopErr error
	defer func() {
		c.fail(loopErr)
		close(c.events)
		<-c.dispatchDone
		close(c.done)
	}()
	for {
		raw, err := c.readFrame(c.ctx)
		if err != nil {
			loopErr = err
			return
		}
		p := parsePacket(raw)
		switch p.engine {
		case eioPing:
			if err := c.writeFrame(c.ctx, []byte{eioPong}); err != nil {
				loopErr = err
				return
			}
		case eioClose:
			loopErr = ErrConnClosed
			return
		case eioMessage:
			if err := c.handleMessage(p); err != nil {
				loopErr = err
				return
			}
		}
	}
}

func (c *Client) handleMessage(p packet) error {
	switch p.sio {
	case sioEvent:
		var arr []json.RawMessage
		if err := json.Unmarshal(p.body, &arr); err != nil || len(arr) == 0 {
			return nil
		}
		var name string
		_ = json.Unmarshal(arr[0], &name)
		// If the server expects an ack, answer with an empty one so it does not
		// wait; we never need to return data on broadcasts.
		if p.ackID >= 0 {
			frame := append([]byte{eioMessage, sioAck}, []byte(strconv.Itoa(p.ackID))...)
			frame = append(frame, '[', ']')
			_ = c.writeFrame(c.ctx, frame)
		}
		message := Message{Event: name, Args: arr[1:]}
		return c.enqueueEvent(message)
	case sioAck:
		var arr []json.RawMessage
		_ = json.Unmarshal(p.body, &arr)
		c.acks.resolve(p.ackID, arr)
	case sioConnectError:
		c.fail(fmt.Errorf("socketio: connect error: %s", string(p.body)))
	}
	return nil
}

func (c *Client) enqueueEvent(message Message) error {
	size := int64(len(message.Event))
	for _, arg := range message.Args {
		size += int64(len(arg))
	}
	if c.eventBytes.Add(size) > eventQueueMaxBytes {
		c.eventBytes.Add(-size)
		return ErrEventBacklog
	}
	select {
	case c.events <- queuedMessage{message: message, bytes: size}:
		return nil
	case <-c.ctx.Done():
		c.eventBytes.Add(-size)
		return c.ctx.Err()
	default:
		c.eventBytes.Add(-size)
		return ErrEventBacklog
	}
}

func (c *Client) dispatchLoop() {
	defer close(c.dispatchDone)
	defer close(c.incoming)
	for {
		select {
		case queued, ok := <-c.events:
			if !ok {
				return
			}
			select {
			case c.incoming <- queued.message:
				c.eventBytes.Add(-queued.bytes)
			case <-c.ctx.Done():
				c.eventBytes.Add(-queued.bytes)
				return
			}
		case <-c.ctx.Done():
			return
		}
	}
}

func (c *Client) fail(err error) {
	c.closeOnce.Do(func() {
		if err == nil {
			err = ErrClosed
		}
		c.closeErr = err
		c.cancel()
		_ = c.conn.Close(websocket.StatusNormalClosure, "")
		c.acks.failAll()
	})
}

// readFrame reads one text websocket message, skipping binary frames.
func (c *Client) readFrame(ctx context.Context) ([]byte, error) {
	for {
		readCtx := ctx
		cancel := func() {}
		if c.heartbeatTimeout > 0 {
			readCtx, cancel = context.WithTimeout(ctx, c.heartbeatTimeout)
		}
		typ, data, err := c.conn.Read(readCtx)
		cancel()
		if err != nil {
			if ctx.Err() == nil && errors.Is(readCtx.Err(), context.DeadlineExceeded) {
				return nil, fmt.Errorf("%w: engine.io heartbeat timeout", ErrConnClosed)
			}
			return nil, err
		}
		if typ == websocket.MessageText {
			return data, nil
		}
	}
}

func (c *Client) writeFrame(ctx context.Context, data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	writeCtx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return c.conn.Write(writeCtx, websocket.MessageText, data)
}
