// Пакет hub — управляющий слой (control plane) BoardProxy: сервер держит
// observer'а на hub-слайде и раздаёт клиентам свободные страницы из фиксированного
// пула, а клиент через него находит свою страницу и мигрирует на неё.
//
// Rendezvous идёт объектами на hub-странице (много участников). В HELLO клиент
// кладёт первое сообщение рукопожатия Noise IK; сервер, если пускает клиента,
// отвечает ASSIGN со вторым сообщением (id выданной страницы едет в нём
// зашифрованным), иначе DENIED. Сам обмен ключами и назначение страницы, таким
// образом, аутентифицированы и конфиденциальны; correlation nonce в открытом
// виде нужен только для маршрутизации ответа на общей hub-странице.
package hub

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"bproxy-core/internal/board"
	"bproxy-core/internal/bond"
	"bproxy-core/internal/codec"
	"bproxy-core/internal/crypto"
	"bproxy-core/internal/handshake"
	"bproxy-core/internal/link"
	"bproxy-core/internal/mux"
	"bproxy-core/internal/proto"
)

// ErrRendezvousDenied возвращается клиенту, когда сервер отклонил подключение.
// Причина намеренно не различается (нет авторизации, пул исчерпан, рукопожатие
// отклонено) — чтобы не давать оракул; настоящая причина логируется сервером.
var ErrRendezvousDenied = errors.New("hub: rendezvous denied by server")

// ErrRendezvousTimeout means the HELLO was not answered in time. A bounded
// wait is essential for reconnect: if the server is down while the board is
// still reachable, the client must delete its stale HELLO and retry later.
var ErrRendezvousTimeout = errors.New("hub: rendezvous timeout")

const (
	nonceLen          = 16
	rendezvousTimeout = 30 * time.Second
	helloCleanupTime  = 3 * time.Second
	versionMarker     = byte(0xff)
)

// Виды rendezvous-сообщений (первый байт тела). Значения читаемые и заведомо
// отличны от видов кадров link.
const (
	rvHello    = byte('H') // клиент→хаб: correlation nonce + msg1 рукопожатия
	rvAssign   = byte('A') // legacy v2 хаб→клиент: nonce + raw Noise msg2
	rvAssignV3 = byte('B') // negotiated хаб→клиент: nonce + version + Noise msg2
	rvDenied   = byte('D') // хаб→клиент: nonce; отказ без причины
)

// rvMsg — разобранное rendezvous-сообщение. body несёт сообщение рукопожатия
// (msg1 в HELLO, msg2 в ASSIGN); у DENIED оно пустое.
type rvMsg struct {
	kind  byte
	nonce [nonceLen]byte
	body  []byte
}

type helloEnvelope struct {
	minVersion byte
	maxVersion byte
	msg1       []byte
	legacy     bool
}

func encodeHello(nonce [nonceLen]byte, msg1 []byte) []byte {
	body := make([]byte, 3+len(msg1))
	body[0] = versionMarker
	body[1] = byte(proto.Version)
	body[2] = byte(proto.Version)
	copy(body[3:], msg1)
	return encodeRV(rvHello, nonce, body)
}

func encodeLegacyHello(nonce [nonceLen]byte, version byte, msg1 []byte) []byte {
	body := append([]byte{version}, msg1...)
	return encodeRV(rvHello, nonce, body)
}

func decodeHello(body []byte) (helloEnvelope, bool) {
	if len(body) < 2 {
		return helloEnvelope{}, false
	}
	if body[0] != versionMarker {
		return helloEnvelope{
			minVersion: body[0],
			maxVersion: body[0],
			msg1:       body[1:],
			legacy:     true,
		}, true
	}
	if len(body) < 4 || body[1] == 0 || body[1] > body[2] {
		return helloEnvelope{}, false
	}
	return helloEnvelope{
		minVersion: body[1],
		maxVersion: body[2],
		msg1:       body[3:],
	}, true
}

func negotiateVersion(h helloEnvelope) (byte, bool) {
	lo := max(h.minVersion, byte(proto.MinVersion))
	hi := min(h.maxVersion, byte(proto.Version))
	if lo > hi {
		return 0, false
	}
	return hi, true
}

func encodeAssign(nonce [nonceLen]byte, version byte, legacy bool, msg2 []byte) []byte {
	if legacy {
		return encodeRV(rvAssign, nonce, msg2)
	}
	body := make([]byte, 1+len(msg2))
	body[0] = version
	copy(body[1:], msg2)
	return encodeRV(rvAssignV3, nonce, body)
}

type assignEnvelope struct {
	version byte
	msg2    []byte
	legacy  bool
}

func decodeAssign(kind byte, body []byte) (assignEnvelope, bool) {
	if len(body) == 0 {
		return assignEnvelope{}, false
	}
	if kind == rvAssign {
		return assignEnvelope{version: 2, msg2: body, legacy: true}, true
	}
	if kind != rvAssignV3 || len(body) < 2 {
		return assignEnvelope{}, false
	}
	return assignEnvelope{version: body[0], msg2: body[1:]}, true
}
func encodeDenied(nonce [nonceLen]byte) []byte { return encodeRV(rvDenied, nonce, nil) }

func encodeRV(kind byte, nonce [nonceLen]byte, body []byte) []byte {
	b := make([]byte, 1+nonceLen+len(body))
	b[0] = kind
	copy(b[1:], nonce[:])
	copy(b[1+nonceLen:], body)
	return b
}

func decodeRV(b []byte) (rvMsg, bool) {
	if len(b) < 1+nonceLen {
		return rvMsg{}, false
	}
	var m rvMsg
	m.kind = b[0]
	copy(m.nonce[:], b[1:1+nonceLen])
	m.body = b[1+nonceLen:]
	return m, true
}

// ClientConfig конфигурирует клиентский rendezvous.
type ClientConfig struct {
	// Session — уже присоединённая сессия доски; Dial переиспользует её, переключая
	// подписку с hub-слайда на выданную страницу.
	Session board.Session
	// Dialer creates independent board sessions for additional bonded lanes.
	// Nil keeps the legacy single-lane behaviour.
	Dialer Dialer
	// HubSlide — хэш hub-слайда для rendezvous.
	HubSlide string
	// Codec — базовый кодек rendezvous-объектов; трафик после рукопожатия
	// шифруется поверх него (crypto.Sealed).
	Codec codec.Codec
	Link  link.Options
	// ClientStatic — статическая пара ключей клиента (из keylink).
	ClientStatic crypto.Keypair
	// ServerPublic — статический публичный ключ сервера (из keylink); клиент
	// проверяет им сервер и начинает Noise IK.
	ServerPublic []byte
	// MaxPayload, StreamWindow, CoalesceTarget и StreamIdleTimeout передаются в
	// mux клиента.
	MaxPayload      int
	StreamWindow    int
	MaxStreamWindow int
	// CoalesceTarget — 0 = полностью адаптивно (см. mux.Options.CoalesceTarget),
	// >0 = ручной потолок.
	CoalesceTarget    int
	StreamIdleTimeout time.Duration
	// TargetLanes is the number of lanes opened synchronously. Zero means one.
	// It is retained for fixed-lane callers and tests.
	TargetLanes int
	// MaxLanes enables adaptive scale-up above TargetLanes. Zero disables it.
	MaxLanes int
}

// BundleInfo is the authenticated identity returned by a v3 rendezvous. The
// join token remains inside core and must never be logged.
type BundleInfo struct {
	ID        bond.BundleID
	LaneID    bond.LaneID
	Epoch     bond.Epoch
	JoinToken bond.JoinToken
	MaxLanes  int
	Page      string
}

type DialResult struct {
	Session *mux.Session
	Bundle  BundleInfo
	Lanes   []BundleInfo
}

// Dial выполняет rendezvous: подписывается на hub, шлёт HELLO с первым
// сообщением рукопожатия, ждёт ASSIGN, завершает рукопожатие (получая ключи и
// id страницы), мигрирует на страницу и возвращает клиентскую mux-сессию с уже
// зашифрованным транспортом.
func Dial(ctx context.Context, cfg ClientConfig) (*mux.Session, error) {
	result, err := DialBundle(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return result.Session, nil
}

// DialBundle creates one logical bundle. Optional initial lanes are joined
// synchronously; adaptive lanes are managed in the background.
func DialBundle(ctx context.Context, cfg ClientConfig) (*DialResult, error) {
	sess := cfg.Session
	if _, err := sess.Subscribe(ctx, cfg.HubSlide); err != nil {
		return nil, err
	}

	bundleID, err := bond.NewBundleID()
	if err != nil {
		return nil, fmt.Errorf("hub: bundle id: %w", err)
	}
	init, err := handshake.InitiateWithPayload(
		cfg.ClientStatic,
		cfg.ServerPublic,
		encodeNewBundleRequest(bundleID),
	)
	if err != nil {
		return nil, fmt.Errorf("hub: handshake init: %w", err)
	}

	var nonce [nonceLen]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	helloVal, err := cfg.Codec.Encode(encodeHello(nonce, init.Message()))
	if err != nil {
		return nil, err
	}
	helloID := board.NewID()
	if err := sess.Put(ctx, board.Object{ID: helloID, Value: helloVal}); err != nil {
		return nil, err
	}

	waitCtx, cancelWait := context.WithTimeout(ctx, rendezvousTimeout)
	assign, err := waitAssign(waitCtx, sess, cfg.Codec, nonce)
	cancelWait()
	// Удаляем HELLO, пока сессия ещё подписана на hub-слайд. Обычно сервер уже
	// удалил его сам; повторный Delete идемпотентен. При timeout это не даёт
	// старым HELLO копиться и занимать страницы после позднего старта сервера.
	cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), helloCleanupTime)
	_ = sess.Delete(cleanupCtx, helloID)
	cancelCleanup()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			return nil, ErrRendezvousTimeout
		}
		return nil, err
	}
	keys, assignmentBytes, err := init.Complete(assign.msg2)
	if err != nil {
		return nil, fmt.Errorf("hub: handshake complete: %w", err)
	}
	if assign.version < 3 || assign.version > byte(proto.Version) {
		return nil, fmt.Errorf("hub: server selected unsupported protocol version %d", assign.version)
	}
	assignment, ok := decodeBundleAssignment(assignmentBytes, assign.version)
	if !ok || assignment.id != bundleID {
		return nil, errors.New("hub: invalid bundle assignment")
	}
	page := assignment.page

	sealed, err := crypto.NewSealed(cfg.Codec, keys.Send, keys.Recv)
	if err != nil {
		return nil, fmt.Errorf("hub: sealed codec: %w", err)
	}
	rvLog(cfg.Link.Log).Info("hub: assigned page",
		"participant", sess.Participant(),
		"bundle", assignment.id.String(),
		"lane", assignment.lane,
		"epoch", assignment.epoch,
		"page", page,
	)

	// Мигрируем на выданную страницу и поднимаем на ней link + bond + mux. Снапшот от
	// Subscribe может уже содержать объекты пира (сервер поднимает свой link и
	// шлёт первый advertise раньше, чем клиент успевает подписаться) — их не
	// будет в Events(), поэтому обязательно прогоняем через Reconcile, иначе
	// реальный rwnd пира станет известен только на первом keepalive, до 30с.
	//
	// Важен порядок: mux.New должен запуститься ДО Reconcile. Reconcile
	// проигрывает снапшот на горутине run(), и если в снапшоте окажется больше
	// data-payload'ов, чем помещается в буфер recvCh (например, страница ещё не
	// вычищена от предыдущего использования), run() заблокируется на отправке в
	// recvCh — а разбирает его reader() мультиплексора, который иначе ещё не
	// запущен. Получился бы дедлок ровно в этом месте.
	snapshot, err := sess.Subscribe(ctx, page)
	if err != nil {
		return nil, err
	}
	l := link.New(sess, sealed, laneLinkOptions(cfg.Link, assignment.id, assignment.lane))
	b := bond.New(bond.Options{})
	if err := b.AddLane(assignment.lane, l); err != nil {
		_ = l.Close()
		return nil, err
	}
	m := mux.New(b, mux.Options{
		Version:           int(assign.version),
		Client:            true,
		MaxPayload:        cfg.MaxPayload,
		StreamWindow:      cfg.StreamWindow,
		MaxStreamWindow:   cfg.MaxStreamWindow,
		CoalesceTarget:    cfg.CoalesceTarget,
		StreamIdleTimeout: cfg.StreamIdleTimeout,
	})
	if err := l.Reconcile(ctx, snapshot); err != nil {
		_ = m.Close()
		return nil, err
	}
	result := &DialResult{
		Session: m,
		Bundle: BundleInfo{
			ID:        assignment.id,
			LaneID:    assignment.lane,
			Epoch:     assignment.epoch,
			JoinToken: assignment.token,
			MaxLanes:  int(assignment.maxLanes),
			Page:      assignment.page,
		},
	}
	result.Lanes = append(result.Lanes, result.Bundle)

	targetLanes := cfg.TargetLanes
	if targetLanes <= 0 {
		targetLanes = 1
	}
	maxLanes := cfg.MaxLanes
	if assign.version >= 5 {
		maxLanes = int(assignment.maxLanes)
	}
	if maxLanes > 0 && targetLanes > maxLanes {
		targetLanes = maxLanes
	}
	if targetLanes > absoluteMaxBundleLanes {
		targetLanes = absoluteMaxBundleLanes
	}
	for len(result.Lanes) < targetLanes && cfg.Dialer != nil {
		laneInfo, err := joinBundleLane(ctx, cfg, b, result.Bundle)
		if err != nil {
			rvLog(cfg.Link.Log).Warn("hub: additional lane unavailable; continuing degraded",
				"bundle", result.Bundle.ID.String(), "active_lanes", b.LaneCount(), "err", err)
			break
		}
		result.Lanes = append(result.Lanes, laneInfo)
	}
	if maxLanes > absoluteMaxBundleLanes {
		maxLanes = absoluteMaxBundleLanes
	}
	if maxLanes > targetLanes {
		startAdaptiveLanes(cfg, b, m, result.Bundle, maxLanes)
	}
	return result, nil
}

func joinBundleLane(ctx context.Context, cfg ClientConfig, b *bond.Conn, bundle BundleInfo) (BundleInfo, error) {
	sess, err := cfg.Dialer.Join(ctx)
	if err != nil {
		return BundleInfo{}, fmt.Errorf("hub: join board for lane: %w", err)
	}
	keepSession := false
	defer func() {
		if !keepSession {
			_ = sess.Close()
		}
	}()
	if _, err := sess.Subscribe(ctx, cfg.HubSlide); err != nil {
		return BundleInfo{}, err
	}
	init, err := handshake.InitiateWithPayload(
		cfg.ClientStatic,
		cfg.ServerPublic,
		encodeJoinBundleRequest(bundle.ID, bundle.Epoch, bundle.JoinToken),
	)
	if err != nil {
		return BundleInfo{}, fmt.Errorf("hub: join lane handshake init: %w", err)
	}
	var nonce [nonceLen]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return BundleInfo{}, err
	}
	value, err := cfg.Codec.Encode(encodeHello(nonce, init.Message()))
	if err != nil {
		return BundleInfo{}, err
	}
	helloID := board.NewID()
	if err := sess.Put(ctx, board.Object{ID: helloID, Value: value}); err != nil {
		return BundleInfo{}, err
	}
	waitCtx, cancelWait := context.WithTimeout(ctx, rendezvousTimeout)
	assign, err := waitAssign(waitCtx, sess, cfg.Codec, nonce)
	cancelWait()
	cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), helloCleanupTime)
	_ = sess.Delete(cleanupCtx, helloID)
	cancelCleanup()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			return BundleInfo{}, ErrRendezvousTimeout
		}
		return BundleInfo{}, err
	}
	keys, assignmentBytes, err := init.Complete(assign.msg2)
	if err != nil {
		return BundleInfo{}, fmt.Errorf("hub: join lane handshake complete: %w", err)
	}
	if assign.version < 3 || assign.version > byte(proto.Version) {
		return BundleInfo{}, fmt.Errorf("hub: server selected unsupported protocol version %d", assign.version)
	}
	assignment, ok := decodeBundleAssignment(assignmentBytes, assign.version)
	if !ok || assignment.id != bundle.ID || assignment.epoch != bundle.Epoch ||
		!assignment.token.Equal(bundle.JoinToken) || assignment.lane == bundle.LaneID ||
		(assign.version >= 5 && int(assignment.maxLanes) != bundle.MaxLanes) {
		return BundleInfo{}, errors.New("hub: invalid joined lane assignment")
	}
	sealed, err := crypto.NewSealed(cfg.Codec, keys.Send, keys.Recv)
	if err != nil {
		return BundleInfo{}, fmt.Errorf("hub: joined lane sealed codec: %w", err)
	}
	snapshot, err := sess.Subscribe(ctx, assignment.page)
	if err != nil {
		return BundleInfo{}, err
	}
	l := link.New(sess, sealed, laneLinkOptions(cfg.Link, assignment.id, assignment.lane))
	if err := b.AddLane(assignment.lane, l); err != nil {
		_ = l.Close()
		return BundleInfo{}, err
	}
	if err := l.Reconcile(ctx, snapshot); err != nil {
		b.RemoveLane(assignment.lane)
		return BundleInfo{}, err
	}
	keepSession = true
	rvLog(cfg.Link.Log).Info("hub: joined additional lane",
		"participant", sess.Participant(), "bundle", assignment.id.String(),
		"lane", assignment.lane, "epoch", assignment.epoch, "page", assignment.page)
	return BundleInfo{
		ID: assignment.id, LaneID: assignment.lane, Epoch: assignment.epoch,
		JoinToken: assignment.token, MaxLanes: int(assignment.maxLanes), Page: assignment.page,
	}, nil
}

// rvLog возвращает переданный логгер или slog.Default(), если он не задан.
func rvLog(log *slog.Logger) *slog.Logger {
	if log == nil {
		return slog.Default()
	}
	return log
}

func laneLinkOptions(options link.Options, bundleID bond.BundleID, laneID bond.LaneID) link.Options {
	options.Log = rvLog(options.Log).With(
		"bundle", bundleID.String(),
		"lane", laneID,
	)
	return options
}

// waitAssign читает события hub-страницы, пока не придёт ASSIGN/DENIED с нашим
// nonce, и возвращает тело ASSIGN (msg2 рукопожатия). Ответный объект удаляется
// (ack), пока мы ещё на hub-странице.
func waitAssign(ctx context.Context, sess board.Session, c codec.Codec, nonce [nonceLen]byte) (assignEnvelope, error) {
	self := sess.Participant()
	for {
		select {
		case <-ctx.Done():
			return assignEnvelope{}, ctx.Err()
		case ev, ok := <-sess.Events():
			if !ok {
				return assignEnvelope{}, link.ErrClosed
			}
			if ev.Kind != board.Created || ev.Object.CreatorHash == self {
				continue
			}
			frame, err := c.Decode(ev.Object.Value)
			if err != nil {
				continue
			}
			m, ok := decodeRV(frame)
			if !ok || m.nonce != nonce {
				continue
			}
			switch m.kind {
			case rvAssign, rvAssignV3:
				_ = sess.Delete(ctx, ev.Object.ID)
				assign, ok := decodeAssign(m.kind, m.body)
				if !ok {
					return assignEnvelope{}, errors.New("hub: malformed ASSIGN")
				}
				return assign, nil
			case rvDenied:
				_ = sess.Delete(ctx, ev.Object.ID)
				return assignEnvelope{}, ErrRendezvousDenied
			}
		}
	}
}
