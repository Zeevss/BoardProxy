package yandex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"bproxy-core/internal/board"
	"bproxy-core/internal/board/yandex/socketio"
	"bproxy-core/internal/netprotect"
)

// eventBuffer bounds how far a consumer of Events may lag.
const eventBuffer = 1024

// Параметры авто-реконнекта. При обрыве websocket'а сессия переподключается
// прозрачно для верхних слоёв: link/mux не видят разрыва, а после re-subscribe
// получают свежий снапшот на reconcile (см. board.Session.Reconnects).
const (
	reconnectMinBackoff = 500 * time.Millisecond
	reconnectMaxBackoff = 10 * time.Second
	reconnectDeadline   = 2 * time.Minute
	redialTimeout       = 30 * time.Second
)

// Options configures a Yandex board session.
type Options struct {
	// APIBase is the REST entry point, e.g. https://boards.yandex.ru/api.
	APIBase string
	// Hash is the board hash to join.
	Hash string
	// GuestName is the display name; a random suffix is appended per session.
	GuestName string
	// Protector excludes REST/WebSocket sockets from an OS VPN before connect.
	Protector netprotect.Protector
}

// socketConn — минимум, что сессия использует от Socket.IO-клиента. Абстракция
// нужна, чтобы переподключение тестировалось без реального websocket'а
// (см. dialFunc). Единственная боевая реализация — *socketio.Client.
type socketConn interface {
	Emit(ctx context.Context, event string, arg any) ([]json.RawMessage, error)
	Events() <-chan socketio.Message
	Close() error
}

// dialFunc устанавливает новое соединение. Боевая реализация — realDial (заново
// открывает websocket с тем же cookie); тесты подставляют свою.
type dialFunc func(ctx context.Context) (socketConn, error)

// Session is a board.Session backed by a Yandex Board guest connection. При
// обрыве нижележащего websocket'а она переподключается сама, не роняя канал
// событий, — операции (Put/Delete/Subscribe) блокируются на время
// переподключения и повторяются, а не отдают ошибку наверх.
type Session struct {
	rest        *restClient
	hash        string
	participant string
	info        *whiteboardInfo
	socketURL   string
	dial        dialFunc

	events     chan board.Event
	reconnects chan []board.Object

	mu   sync.Mutex
	page string

	closeOnce sync.Once
	closeCh   chan struct{}

	// connMu защищает текущее соединение. sio == nil на время переподключения;
	// connFail взводится, когда переподключиться так и не удалось. connWait
	// закрывается и заменяется при каждой смене соединения (или окончательном
	// провале) — на нём ждут вызовы emit, пока соединения нет.
	connMu   sync.Mutex
	sio      socketConn
	connFail bool
	connWait chan struct{}
}

var _ board.Session = (*Session)(nil)

// Join runs the guest join flow (REST auth + Socket.IO connect) and returns a
// session ready to Subscribe. It does not subscribe to any page yet.
func Join(ctx context.Context, opts Options) (*Session, error) {
	rest, err := newRESTClient(opts.APIBase, opts.Hash, opts.Protector)
	if err != nil {
		return nil, err
	}
	name := fmt.Sprintf("%s-%s", opts.GuestName, randSuffix())
	if err := rest.requestGuestToken(ctx, name); err != nil {
		return nil, fmt.Errorf("request-guest-token: %w", err)
	}
	info, err := rest.getWhiteboardInfo(ctx)
	if err != nil {
		return nil, err
	}

	s := &Session{
		rest:        rest,
		hash:        opts.Hash,
		participant: info.participantHash,
		info:        info,
		socketURL:   info.socketServers[0].URL(),
		events:      make(chan board.Event, eventBuffer),
		reconnects:  make(chan []board.Object, 1),
		closeCh:     make(chan struct{}),
		connWait:    make(chan struct{}),
	}
	s.dial = s.realDial

	sio, err := s.dial(ctx)
	if err != nil {
		return nil, fmt.Errorf("socket.io dial %s: %w", s.socketURL, err)
	}
	s.setConnected(sio)
	go s.manage()
	return s, nil
}

// realDial открывает websocket заново, переиспользуя тот же guest-cookie (и,
// значит, того же участника) — переподключение не должно менять нашу личность
// на доске.
func (s *Session) realDial(ctx context.Context) (socketConn, error) {
	cookie, err := s.rest.cookieHeader(s.socketURL)
	if err != nil {
		return nil, err
	}
	wsHTTP := *s.rest.http
	wsHTTP.Jar = nil // cookie is forwarded explicitly in the handshake header
	c, err := socketio.Dial(ctx, s.socketURL, cookie, &wsHTTP)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// CurrentSlide returns the board's current slide hash from the join info — the
// default page to subscribe to.
func (s *Session) CurrentSlide() string { return s.info.currentSlide }

// Slides returns the hashes of all slides on the board (the page pool), in
// the order the board API returned them (not sorted). The hub picks a
// deterministic slide from this set as its rendezvous page — see
// app.resolveHubSlide — and hands out the rest.
func (s *Session) Slides() []string { return s.info.slides }

func (s *Session) Participant() string { return s.participant }

// Subscribe joins a page and returns its current object snapshot, which the
// board delivers inside the subscribe ack (SPEC §5.3).
func (s *Session) Subscribe(ctx context.Context, page string) ([]board.Object, error) {
	args, err := s.emit(ctx, "dashboard", s.subscribeEnvelope(page))
	if err != nil {
		return nil, fmt.Errorf("subscribe %s: %w", page, err)
	}
	s.mu.Lock()
	s.page = page
	s.mu.Unlock()
	return parseSnapshot(args), nil
}

func (s *Session) subscribeEnvelope(page string) dashboardEnvelope {
	return dashboardEnvelope{
		Action:      "subscribe-slide-dashboard",
		Participant: s.participant,
		Data: subscribeData{
			Session:             "sess-" + randSuffix(),
			Dashboard:           page,
			Presentation:        s.hash,
			Properties:          s.info.properties,
			Participant:         s.participant,
			ParticipantTeamRole: -1,
			Options: subscribeOptions{
				Type:         "landing",
				Participant:  s.info.participant,
				Intermediate: nil,
				Device:       map[string]any{},
			},
		},
	}
}

// Put creates or updates an object on the subscribed page via modify-objects.
func (s *Session) Put(ctx context.Context, obj board.Object) error {
	if obj.ID == "" {
		obj.ID = newObjectID()
	}
	obj.CreatorHash = s.participant
	env := dashboardEnvelope{
		Action:      "modify-objects",
		Participant: s.participant,
		Data:        modifyData{Objects: []mxCell{buildCell(obj)}},
	}
	if _, err := s.emit(ctx, "dashboard", env); err != nil {
		return fmt.Errorf("modify-objects: %w", err)
	}
	return nil
}

// Delete removes one or more objects by id via a single drop-objects call. The
// peer observes it as a server-drop-objects broadcast carrying every id
// (verified against captured board traffic); that broadcast is what the link
// layer treats as an ACK, one event per id.
func (s *Session) Delete(ctx context.Context, ids ...string) error {
	if len(ids) == 0 {
		return nil
	}
	env := dashboardEnvelope{
		Action:      "drop-objects",
		Participant: s.participant,
		Data:        modifyData{Objects: dropCells(ids)},
	}
	if _, err := s.emit(ctx, "dashboard", env); err != nil {
		return fmt.Errorf("drop-objects: %w", err)
	}
	return nil
}

// dropCells builds the mxCell slice for a drop-objects call over ids.
func dropCells(ids []string) []mxCell {
	cells := make([]mxCell, len(ids))
	for i, id := range ids {
		cells[i] = mxCell{Attributes: cellAttrs{ID: id}, Hash: id}
	}
	return cells
}

func (s *Session) Events() <-chan board.Event { return s.events }

// Reconnects streams a fresh page snapshot each time the session transparently
// reconnects (см. board.Session.Reconnects).
func (s *Session) Reconnects() <-chan []board.Object { return s.reconnects }

func (s *Session) Close() error {
	var err error
	s.closeOnce.Do(func() {
		close(s.closeCh)
		s.connMu.Lock()
		sio := s.sio
		s.sio = nil
		s.connMu.Unlock()
		if sio != nil {
			err = sio.Close()
		}
	})
	return err
}

func (s *Session) isClosed() bool {
	select {
	case <-s.closeCh:
		return true
	default:
		return false
	}
}

// emit шлёт событие через текущее соединение, а на время переподключения ждёт
// его восстановления и повторяет — так обрыв websocket'а не превращается в
// ошибку наверх (иначе link, получив ошибку Put, заглушил бы себя). Put/Delete
// на доске идемпотентны по id объекта, поэтому повтор безопасен.
func (s *Session) emit(ctx context.Context, event string, arg any) ([]json.RawMessage, error) {
	for {
		sio, failed, wait := s.connState()
		if failed || s.isClosed() {
			return nil, board.ErrClosed
		}
		if sio == nil {
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-s.closeCh:
				return nil, board.ErrClosed
			}
		}
		args, err := sio.Emit(ctx, event, arg)
		if err == nil {
			return args, nil
		}
		if ctx.Err() != nil || !isConnError(err) {
			return nil, err
		}
		// Соединение оборвалось на этом emit — ждём следующего (пере)подключения
		// и повторяем. wait закроется, когда manage опубликует новое соединение
		// (или окончательно провалится).
		select {
		case <-wait:
		case <-ctx.Done():
			return nil, err
		case <-s.closeCh:
			return nil, board.ErrClosed
		}
	}
}

func isConnError(err error) bool {
	return errors.Is(err, socketio.ErrConnClosed) || errors.Is(err, socketio.ErrClosed)
}

func (s *Session) connState() (sio socketConn, failed bool, wait chan struct{}) {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	return s.sio, s.connFail, s.connWait
}

func (s *Session) setConnected(sio socketConn) {
	s.connMu.Lock()
	s.sio = sio
	close(s.connWait)
	s.connWait = make(chan struct{})
	s.connMu.Unlock()
}

func (s *Session) setDisconnected() {
	s.connMu.Lock()
	s.sio = nil
	s.connMu.Unlock()
}

func (s *Session) setFailed() {
	s.connMu.Lock()
	s.connFail = true
	s.sio = nil
	close(s.connWait)
	s.connWait = make(chan struct{})
	s.connMu.Unlock()
}

// manage прокачивает события текущего соединения в s.events; при неожиданном
// обрыве переподключается и продолжает — тем же каналом событий, прозрачно для
// верхних слоёв.
func (s *Session) manage() {
	defer close(s.events)
	for {
		sio, _, _ := s.connState()
		if sio == nil {
			return
		}
		for msg := range sio.Events() {
			s.dispatch(msg)
		}
		if s.isClosed() {
			return
		}
		s.setDisconnected()
		if !s.reconnect() {
			s.setFailed()
			return
		}
	}
}

// dispatch translates one incoming Socket.IO "dashboard" broadcast into
// board.Events.
func (s *Session) dispatch(msg socketio.Message) {
	if msg.Event != "dashboard" || len(msg.Args) == 0 {
		return
	}
	var env incomingEnvelope
	if err := json.Unmarshal(msg.Args[0], &env); err != nil {
		return
	}
	for _, ev := range env.toEvents() {
		select {
		case s.events <- ev:
		case <-s.closeCh:
			return
		}
	}
}

// reconnect повторяет redial с экспоненциальным backoff до успеха, закрытия
// сессии или общего дедлайна. true — переподключились.
func (s *Session) reconnect() bool {
	deadline := time.Now().Add(reconnectDeadline)
	backoff := reconnectMinBackoff
	for {
		// Первую попытку делаем сразу: транзиентный блип чаще всего лечится
		// немедленным повтором, backoff нужен только между неудачами.
		if err := s.redial(); err == nil {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-s.closeCh:
			return false
		case <-time.After(backoff):
		}
		if backoff *= 2; backoff > reconnectMaxBackoff {
			backoff = reconnectMaxBackoff
		}
	}
}

// redial открывает новое соединение, заново подписывается на текущую страницу и
// отдаёт свежий снапшот на reconcile. Подписка идёт напрямую по свежему
// соединению (не через устойчивый emit), чтобы не ждать самих себя.
func (s *Session) redial() error {
	ctx, cancel := s.opContext()
	defer cancel()

	sio, err := s.dial(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	page := s.page
	s.mu.Unlock()

	var snapshot []board.Object
	if page != "" {
		args, err := sio.Emit(ctx, "dashboard", s.subscribeEnvelope(page))
		if err != nil {
			_ = sio.Close()
			return err
		}
		snapshot = parseSnapshot(args)
	}
	s.setConnected(sio)
	if page != "" {
		s.deliverReconnect(snapshot)
	}
	return nil
}

// deliverReconnect кладёт снапшот в reconnects, оставляя в канале только самый
// свежий и не блокируясь, если его никто не читает (например, hub-сессия, у
// которой нет link'а сверху).
func (s *Session) deliverReconnect(snapshot []board.Object) {
	for {
		select {
		case s.reconnects <- snapshot:
			return
		default:
		}
		select {
		case <-s.reconnects:
		default:
		}
	}
}

// opContext возвращает контекст с таймаутом, отменяемый также закрытием сессии.
func (s *Session) opContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), redialTimeout)
	go func() {
		select {
		case <-s.closeCh:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

func randSuffix() string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 6)
	for i := range b {
		b[i] = alphabet[rand.Intn(len(alphabet))]
	}
	return string(b)
}
