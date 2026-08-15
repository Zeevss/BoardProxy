package app

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"bproxy-core/internal/board/yandex"
	"bproxy-core/internal/codec"
	"bproxy-core/internal/control"
	"bproxy-core/internal/controlapi"
	"bproxy-core/internal/crypto"
	"bproxy-core/internal/egress"
	"bproxy-core/internal/hub"
	"bproxy-core/internal/keylink"
	"bproxy-core/internal/logging"
	"bproxy-core/internal/mgmt"
	"bproxy-core/internal/runtimeevents"
	"bproxy-core/internal/serverconfig"
	"bproxy-core/internal/telemetry"
)

const (
	serverMetricsInterval = 30 * time.Second
	boardRetryMinBackoff  = 500 * time.Millisecond
	boardRetryMaxBackoff  = 30 * time.Second
	runtimeEventCapacity  = 4096
)

var ErrRevisionConflict = errors.New("app: config revision conflict")
var ErrAlreadyExists = errors.New("app: resource already exists")

type BoardState string

const (
	BoardStarting BoardState = "starting"
	BoardActive   BoardState = "active"
	BoardRetrying BoardState = "retrying"
	BoardDraining BoardState = "draining"
	BoardStopped  BoardState = "stopped"
)

type boardRuntime struct {
	mu       sync.RWMutex
	cfg      serverconfig.Board
	state    BoardState
	err      string
	srv      *hub.Server
	cancel   context.CancelFunc
	done     chan struct{}
	stopOnce sync.Once
}

func (b *boardRuntime) stop() {
	b.stopOnce.Do(func() {
		b.mu.Lock()
		b.state = BoardDraining
		cancel, srv, done := b.cancel, b.srv, b.done
		b.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		if srv != nil {
			_ = srv.Close()
		}
		if done != nil {
			<-done
		}
		b.mu.Lock()
		b.state = BoardStopped
		b.mu.Unlock()
	})
}

func (b *boardRuntime) snapshot() (serverconfig.Board, BoardState, string, *hub.Server) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.cfg, b.state, b.err, b.srv
}

func (b *boardRuntime) updateConfig(cfg serverconfig.Board) {
	b.mu.Lock()
	b.cfg = cfg
	b.mu.Unlock()
}

// ServerRuntime owns all process-local desired and live state. It never writes
// configuration or counters to disk. applyMu serializes config transactions;
// mu protects short state snapshots and never surrounds network operations.
type ServerRuntime struct {
	ctx    context.Context
	cancel context.CancelFunc

	applyMu sync.Mutex
	mu      sync.RWMutex
	cfg     serverconfig.Config
	source  string
	boards  map[string]*boardRuntime

	revision   atomic.Uint64
	started    time.Time
	log        *slog.Logger
	logs       *logging.Buffer
	registry   *control.Registry
	serverKP   crypto.Keypair
	reconnects *yandex.ReconnectMetrics
	events     *runtimeevents.Journal
	startBoard func(context.Context, serverconfig.Config, serverconfig.Board) (*hub.Server, string, int, error)
}

func NewServerRuntime(ctx context.Context, cfg serverconfig.Config, source string, log *slog.Logger, logs *logging.Buffer) (*ServerRuntime, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	private, err := serverconfig.DecodePrivateKey(cfg.Server.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("server identity: %w", err)
	}
	serverKP, err := crypto.KeypairFromPrivate(private)
	if err != nil {
		return nil, fmt.Errorf("server identity: %w", err)
	}
	registry, err := control.NewRegistry(cfg.Users)
	if err != nil {
		return nil, fmt.Errorf("compile users: %w", err)
	}
	rctx, cancel := context.WithCancel(ctx)
	r := &ServerRuntime{
		ctx: rctx, cancel: cancel, cfg: cloneConfig(cfg), source: source,
		boards: make(map[string]*boardRuntime), started: time.Now(),
		log: log, logs: logs, registry: registry, serverKP: serverKP,
		reconnects: yandex.NewReconnectMetrics(), events: runtimeevents.New(runtimeEventCapacity),
	}
	r.startBoard = r.startBoardAttempt
	r.revision.Store(1)
	for _, board := range cfg.Boards {
		if !board.IsEnabled() {
			r.boards[board.Tag] = &boardRuntime{cfg: board, state: BoardStopped}
			continue
		}
		r.boards[board.Tag] = r.launchBoard(cfg, board)
	}
	return r, nil
}

func (r *ServerRuntime) Close() {
	r.cancel()
	r.applyMu.Lock()
	r.mu.Lock()
	boards := make([]*boardRuntime, 0, len(r.boards))
	for _, br := range r.boards {
		boards = append(boards, br)
	}
	r.mu.Unlock()
	for _, br := range boards {
		r.stopBoard(br)
	}
	r.applyMu.Unlock()
}

func (r *ServerRuntime) Revision() uint64        { return r.revision.Load() }
func (r *ServerRuntime) Source() string          { return r.source }
func (r *ServerRuntime) ServerPublicKey() []byte { return r.serverKP.Public() }

func (r *ServerRuntime) Config() serverconfig.Config {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return cloneConfig(r.cfg)
}

func (r *ServerRuntime) Users() []control.UserView { return r.registry.List() }

func (r *ServerRuntime) SubscribeEvents(bootID string, afterSequence uint64) runtimeevents.Subscription {
	return r.events.Subscribe(bootID, afterSequence)
}

func (r *ServerRuntime) EventPosition() (string, uint64) { return r.events.Position() }

func (r *ServerRuntime) Reload(expected uint64) error {
	if r.source == "stdin:" || r.source == "-" {
		return errors.New("app: stdin configuration cannot be reloaded")
	}
	// Capture before file I/O. Otherwise expected=0 could silently overwrite a
	// concurrent gRPC mutation that commits while the config is being read.
	if expected == 0 {
		expected = r.Revision()
	}
	cfg, err := serverconfig.Load(r.source, nil)
	if err != nil {
		return err
	}
	return r.ApplyConfig(expected, cfg)
}

func (r *ServerRuntime) Boards() []control.BoardView {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]control.BoardView, 0, len(r.boards))
	for _, br := range r.boards {
		cfg, state, errText, _ := br.snapshot()
		out = append(out, control.BoardView{Config: cfg, State: string(state), Error: errText})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Config.Tag < out[j].Config.Tag })
	return out
}

// launchBoard makes a board an independent lifecycle unit. Initial REST/socket
// setup can take up to the board dial timeout, so it must not block gRPC,
// config reconciliation or healthy boards.
func (r *ServerRuntime) launchBoard(root serverconfig.Config, board serverconfig.Board) *boardRuntime {
	bctx, cancel := context.WithCancel(r.ctx)
	br := &boardRuntime{cfg: board, state: BoardStarting, cancel: cancel, done: make(chan struct{})}
	r.publishBoardState(board.Tag, BoardStopped, BoardStarting, "")
	go r.runBoard(bctx, root, board, br)
	return br
}

func (r *ServerRuntime) stopBoard(board *boardRuntime) {
	config, previous, _, _ := board.snapshot()
	if previous != BoardDraining && previous != BoardStopped {
		r.publishBoardState(config.Tag, previous, BoardDraining, "")
	}
	board.stop()
	if previous != BoardStopped {
		r.publishBoardState(config.Tag, BoardDraining, BoardStopped, "")
	}
}

func (r *ServerRuntime) runBoard(bctx context.Context, root serverconfig.Config, board serverconfig.Board, br *boardRuntime) {
	defer close(br.done)
	backoff := boardRetryMinBackoff
	for {
		if bctx.Err() != nil {
			return
		}
		srv, hubSlide, pages, err := r.startBoard(bctx, root, board)
		if err != nil {
			if bctx.Err() != nil {
				return
			}
			br.mu.Lock()
			previous := br.state
			previousError := br.err
			changed := false
			if br.state != BoardDraining && br.state != BoardStopped {
				br.state = BoardRetrying
				br.err = err.Error()
				changed = previous != br.state || previousError != br.err
			}
			br.mu.Unlock()
			if changed {
				r.publishBoardState(board.Tag, previous, BoardRetrying, err.Error())
			}
			r.log.Warn("board unavailable; retrying", "tag", board.Tag, "hash", board.Hash, "err", err, "backoff", backoff)
			if !waitBoardRetry(bctx, backoff) {
				return
			}
			backoff = min(backoff*2, boardRetryMaxBackoff)
			br.mu.Lock()
			previous = br.state
			changed = false
			if br.state != BoardDraining && br.state != BoardStopped {
				br.state = BoardStarting
				br.err = ""
				changed = previous != br.state
			}
			br.mu.Unlock()
			if changed {
				r.publishBoardState(board.Tag, previous, BoardStarting, "")
			}
			continue
		}

		br.mu.Lock()
		if bctx.Err() != nil || br.state == BoardDraining || br.state == BoardStopped {
			br.mu.Unlock()
			_ = srv.Close()
			return
		}
		previous := br.state
		br.srv = srv
		br.state = BoardActive
		br.err = ""
		br.mu.Unlock()
		r.publishBoardState(board.Tag, previous, BoardActive, "")
		r.log.Info("board active", "tag", board.Tag, "hash", board.Hash, "hub", hubSlide,
			"pages", pages, "max_lanes", board.MaxLanes)

		err = egress.Serve(bctx, srv, r.log, egress.Options{AllowPrivate: root.Server.AllowPrivateEgress})
		br.mu.Lock()
		if br.srv == srv {
			br.srv = nil
		}
		br.mu.Unlock()
		_ = srv.Close()
		if bctx.Err() != nil {
			return
		}
		if err == nil {
			err = errors.New("egress stopped unexpectedly")
		}
		if errors.Is(err, context.Canceled) {
			return
		}
		br.mu.Lock()
		previous = br.state
		previousError := br.err
		changed := false
		if br.state != BoardDraining && br.state != BoardStopped {
			br.state = BoardRetrying
			br.err = fmt.Sprintf("egress stopped: %v", err)
			changed = previous != br.state || previousError != br.err
		}
		br.mu.Unlock()
		if changed {
			r.publishBoardState(board.Tag, previous, BoardRetrying, fmt.Sprintf("egress stopped: %v", err))
		}
		r.log.Warn("board egress stopped; retrying", "tag", board.Tag, "hash", board.Hash, "err", err,
			"backoff", boardRetryMinBackoff)
		if !waitBoardRetry(bctx, boardRetryMinBackoff) {
			return
		}
		br.mu.Lock()
		previous = br.state
		changed = false
		if br.state != BoardDraining && br.state != BoardStopped {
			br.state = BoardStarting
			br.err = ""
			changed = previous != br.state
		}
		br.mu.Unlock()
		if changed {
			r.publishBoardState(board.Tag, previous, BoardStarting, "")
		}
		// A board that had reached active gets a fresh short retry window.
		backoff = boardRetryMinBackoff
	}
}

func (r *ServerRuntime) startBoardAttempt(bctx context.Context, root serverconfig.Config, board serverconfig.Board) (*hub.Server, string, int, error) {
	laneOptions := boardOptions(board)
	laneOptions.Role = "server-lane"
	laneOptions.Metrics = r.reconnects
	laneOptions.Log = r.log.With("component", "board", "role", "server-lane", "board", board.Tag)
	hubOptions := laneOptions
	hubOptions.ReconnectForever = true
	hubOptions.Role = "hub-control"
	hubOptions.Log = r.log.With("component", "board", "role", "hub-control", "board", board.Tag)
	hubSession, err := yandex.Join(bctx, hubOptions)
	if err != nil {
		return nil, "", 0, fmt.Errorf("join board: %w", err)
	}
	hubSlide, err := resolveHubSlide(board.HubSlide, hubSession)
	if err != nil {
		_ = hubSession.Close()
		return nil, "", 0, fmt.Errorf("resolve hub page: %w", err)
	}
	pool := poolExcluding(hubSession.Slides(), hubSlide)
	if len(pool) == 0 {
		_ = hubSession.Close()
		return nil, "", 0, errors.New("board has no free pages besides the hub slide")
	}
	srv, err := hub.NewServer(bctx, hub.ServerConfig{
		BoardTag: board.Tag, HubSession: hubSession, HubSlide: hubSlide, Pool: pool,
		Dialer: yandexDialer{laneOptions}, ServerStatic: r.serverKP, Users: r.registry,
		Events: r,
		Codec:  codec.Z85Codec{}, Link: linkOptions(root, r.log),
		MaxPayload: root.Transport.MaxFramePayload, StreamWindow: root.Transport.StreamWindow,
		MaxStreamWindow: root.Transport.MaxStreamWindow, CoalesceTarget: root.Transport.CoalesceTarget,
		StreamIdleTimeout: root.Transport.StreamIdleTimeout.Duration(), IdleTimeout: root.Server.IdleTimeout.Duration(),
		MaxLanes: board.MaxLanes,
	})
	if err != nil {
		_ = hubSession.Close()
		return nil, "", 0, fmt.Errorf("start hub: %w", err)
	}
	return srv, hubSlide, len(pool), nil
}

func (r *ServerRuntime) SessionOpened(event hub.SessionOpened) {
	r.events.Publish(runtimeevents.Event{
		Type: runtimeevents.ClientSessionOpened, RuntimeRevision: r.Revision(),
		UserTag: event.UserID, BoardTag: event.BoardTag, BundleID: event.BundleID,
	})
}

func (r *ServerRuntime) SessionClosed(event hub.SessionClosed) {
	r.events.Publish(runtimeevents.Event{
		Type: runtimeevents.ClientSessionClosed, RuntimeRevision: r.Revision(),
		UserTag: event.UserID, BoardTag: event.BoardTag, BundleID: event.BundleID,
		RXBytes: event.RXBytes, TXBytes: event.TXBytes, Reason: event.Reason,
	})
}

func (r *ServerRuntime) publishBoardState(tag string, previous, current BoardState, errText string) {
	if previous == current && errText == "" {
		return
	}
	r.events.Publish(runtimeevents.Event{
		Type: runtimeevents.BoardStateChanged, RuntimeRevision: r.Revision(),
		BoardTag: tag, PreviousState: string(previous), State: string(current), Error: errText,
	})
}

func waitBoardRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// ApplyConfig reconciles a validated desired-state snapshot. User policy is
// published atomically. Board resources are independently replaced and report
// failed state without taking healthy boards down.
func (r *ServerRuntime) ApplyConfig(expected uint64, candidate serverconfig.Config) error {
	if err := candidate.Validate(); err != nil {
		return err
	}
	r.applyMu.Lock()
	defer r.applyMu.Unlock()
	if expected != 0 && expected != r.Revision() {
		return ErrRevisionConflict
	}
	private, err := serverconfig.DecodePrivateKey(candidate.Server.PrivateKey)
	if err != nil {
		return err
	}
	kp, err := crypto.KeypairFromPrivate(private)
	if err != nil {
		return err
	}
	if !equalBytes(kp.Public(), r.serverKP.Public()) {
		return errors.New("app: server.private_key cannot be changed at runtime")
	}

	// Deny removed/disabled users before disconnecting their current sessions.
	oldConfig := r.Config()
	if oldConfig.Management != candidate.Management {
		return errors.New("app: management listeners cannot be changed at runtime")
	}
	if oldConfig.Observability != candidate.Observability {
		return errors.New("app: observability settings cannot be changed at runtime")
	}
	globalCompatible := runtimeGlobalsCompatible(oldConfig, candidate)
	oldUsers := indexUsers(oldConfig.Users)
	if err := r.registry.Replace(candidate.Users); err != nil {
		return err
	}
	r.mu.Lock()
	oldBoards := r.boards
	r.cfg = cloneConfig(candidate)
	r.mu.Unlock()

	for _, user := range candidate.Users {
		old, existed := oldUsers[user.Tag]
		if !user.IsEnabled() || (existed && userPolicyChanged(old, user)) {
			r.disconnectUser(user.Tag)
		}
		delete(oldUsers, user.Tag)
	}
	for removed := range oldUsers {
		r.disconnectUser(removed)
	}
	r.mu.Lock()
	r.boards = make(map[string]*boardRuntime, len(candidate.Boards))
	r.mu.Unlock()

	for _, board := range candidate.Boards {
		old := oldBoards[board.Tag]
		delete(oldBoards, board.Tag)
		if !board.IsEnabled() {
			if old != nil {
				r.stopBoard(old)
			}
			r.mu.Lock()
			r.boards[board.Tag] = &boardRuntime{cfg: board, state: BoardStopped}
			r.mu.Unlock()
			continue
		}
		oldCfg, oldState, _, oldServer := serverconfig.Board{}, BoardStopped, "", (*hub.Server)(nil)
		if old != nil {
			oldCfg, oldState, _, oldServer = old.snapshot()
		}
		if old != nil && globalCompatible && boardRuntimeCompatible(oldCfg, board) && oldState == BoardActive {
			old.updateConfig(board)
			oldServer.SetMaxLanes(board.MaxLanes)
			r.mu.Lock()
			r.boards[board.Tag] = old
			r.mu.Unlock()
			continue
		}
		if old != nil && globalCompatible && boardRuntimeCompatible(oldCfg, board) &&
			oldCfg.MaxLanes == board.MaxLanes && (oldState == BoardStarting || oldState == BoardRetrying) {
			old.updateConfig(board)
			r.mu.Lock()
			r.boards[board.Tag] = old
			r.mu.Unlock()
			continue
		}
		if old != nil {
			r.stopBoard(old)
		}
		br := r.launchBoard(candidate, board)
		r.mu.Lock()
		r.boards[board.Tag] = br
		r.mu.Unlock()
	}
	for _, removed := range oldBoards {
		r.stopBoard(removed)
	}
	revision := r.revision.Add(1)
	r.publishConfigChanges(oldConfig, candidate, revision)
	return nil
}

// ApplyChanges builds a candidate configuration in memory and commits it with
// one ApplyConfig call. A validation or revision failure leaves every resource
// untouched.
func (r *ServerRuntime) ApplyChanges(expected uint64, changes []serverconfig.Change) error {
	if expected == 0 {
		expected = r.Revision()
	}
	cfg := r.Config()
	seen := make(map[string]bool, len(changes))
	for index, change := range changes {
		if change.ID == "" || seen[change.ID] {
			return fmt.Errorf("app: changes[%d] has an empty or duplicate id", index)
		}
		seen[change.ID] = true
		var err error
		switch change.Kind {
		case serverconfig.UpsertUser:
			if change.User == nil {
				return fmt.Errorf("app: changes[%d] user is required", index)
			}
			cfg.Users = upsertUser(cfg.Users, *change.User)
		case serverconfig.RemoveUser:
			cfg.Users, err = removeUser(cfg.Users, change.Tag)
		case serverconfig.SetUserEnabled:
			err = setUserEnabled(cfg.Users, change.Tag, change.Enabled)
		case serverconfig.UpsertBoard:
			if change.Board == nil {
				return fmt.Errorf("app: changes[%d] board is required", index)
			}
			cfg.Boards = upsertBoard(cfg.Boards, *change.Board)
		case serverconfig.RemoveBoard:
			cfg.Boards, err = removeBoard(cfg.Boards, change.Tag)
		case serverconfig.SetBoardEnabled:
			err = setBoardEnabled(cfg.Boards, change.Tag, change.Enabled)
		default:
			return fmt.Errorf("app: changes[%d] has unsupported kind %q", index, change.Kind)
		}
		if err != nil {
			return err
		}
	}
	return r.ApplyConfig(expected, cfg)
}

func (r *ServerRuntime) ReplaceUser(expected uint64, user serverconfig.User) error {
	if expected == 0 {
		expected = r.Revision()
	}
	cfg := r.Config()
	replaced := false
	for i := range cfg.Users {
		if cfg.Users[i].Tag == user.Tag {
			cfg.Users[i] = user
			replaced = true
			break
		}
	}
	if !replaced {
		cfg.Users = append(cfg.Users, user)
	}
	return r.ApplyConfig(expected, cfg)
}

func (r *ServerRuntime) AddUser(expected uint64, user serverconfig.User) error {
	if expected == 0 {
		expected = r.Revision()
	}
	cfg := r.Config()
	for _, existing := range cfg.Users {
		if existing.Tag == user.Tag {
			return fmt.Errorf("%w: user %q", ErrAlreadyExists, user.Tag)
		}
	}
	cfg.Users = append(cfg.Users, user)
	return r.ApplyConfig(expected, cfg)
}

func (r *ServerRuntime) SetUserEnabled(expected uint64, tag string, enabled bool) error {
	if expected == 0 {
		expected = r.Revision()
	}
	cfg := r.Config()
	for i := range cfg.Users {
		if cfg.Users[i].Tag == tag {
			cfg.Users[i].Enabled = boolPointer(enabled)
			return r.ApplyConfig(expected, cfg)
		}
	}
	return fmt.Errorf("app: user %q not found", tag)
}

func (r *ServerRuntime) RemoveUser(expected uint64, tag string) error {
	if expected == 0 {
		expected = r.Revision()
	}
	cfg := r.Config()
	out := cfg.Users[:0]
	found := false
	for _, user := range cfg.Users {
		if user.Tag == tag {
			found = true
			continue
		}
		out = append(out, user)
	}
	if !found {
		return fmt.Errorf("app: user %q not found", tag)
	}
	cfg.Users = out
	return r.ApplyConfig(expected, cfg)
}

func (r *ServerRuntime) ReplaceBoard(expected uint64, board serverconfig.Board) error {
	if expected == 0 {
		expected = r.Revision()
	}
	cfg := r.Config()
	replaced := false
	for i := range cfg.Boards {
		if cfg.Boards[i].Tag == board.Tag {
			cfg.Boards[i] = board
			replaced = true
			break
		}
	}
	if !replaced {
		cfg.Boards = append(cfg.Boards, board)
	}
	return r.ApplyConfig(expected, cfg)
}

func (r *ServerRuntime) AddBoard(expected uint64, board serverconfig.Board) error {
	if expected == 0 {
		expected = r.Revision()
	}
	cfg := r.Config()
	for _, existing := range cfg.Boards {
		if existing.Tag == board.Tag {
			return fmt.Errorf("%w: board %q", ErrAlreadyExists, board.Tag)
		}
	}
	cfg.Boards = append(cfg.Boards, board)
	return r.ApplyConfig(expected, cfg)
}

func (r *ServerRuntime) SetBoardEnabled(expected uint64, tag string, enabled bool) error {
	if expected == 0 {
		expected = r.Revision()
	}
	cfg := r.Config()
	for i := range cfg.Boards {
		if cfg.Boards[i].Tag == tag {
			cfg.Boards[i].Enabled = boolPointer(enabled)
			return r.ApplyConfig(expected, cfg)
		}
	}
	return fmt.Errorf("app: board %q not found", tag)
}

func (r *ServerRuntime) ApplySnapshot(expected uint64, users []serverconfig.User, boards []serverconfig.Board) error {
	if expected == 0 {
		expected = r.Revision()
	}
	cfg := r.Config()
	cfg.Users = make([]serverconfig.User, len(users))
	for i, user := range users {
		cfg.Users[i] = user
		cfg.Users[i].Boards = append([]string(nil), user.Boards...)
	}
	cfg.Boards = append([]serverconfig.Board(nil), boards...)
	return r.ApplyConfig(expected, cfg)
}

func (r *ServerRuntime) RemoveBoard(expected uint64, tag string) error {
	if expected == 0 {
		expected = r.Revision()
	}
	cfg := r.Config()
	out := cfg.Boards[:0]
	found := false
	for _, board := range cfg.Boards {
		if board.Tag == tag {
			found = true
			continue
		}
		out = append(out, board)
	}
	if !found {
		return fmt.Errorf("app: board %q not found", tag)
	}
	cfg.Boards = out
	return r.ApplyConfig(expected, cfg)
}

func (r *ServerRuntime) disconnectUser(tag string) int {
	r.mu.RLock()
	boards := make([]*boardRuntime, 0, len(r.boards))
	for _, br := range r.boards {
		_, _, _, srv := br.snapshot()
		if srv != nil {
			boards = append(boards, br)
		}
	}
	r.mu.RUnlock()
	total := 0
	for _, br := range boards {
		_, _, _, srv := br.snapshot()
		total += srv.DisconnectUser(r.ctx, tag)
	}
	return total
}

func (r *ServerRuntime) Keylink(tag string) (string, error) {
	cfg := r.Config()
	boardHashes := make(map[string]string, len(cfg.Boards))
	for _, board := range cfg.Boards {
		if board.IsEnabled() {
			boardHashes[board.Tag] = board.Hash
		}
	}
	for _, user := range cfg.Users {
		if user.Tag != tag {
			continue
		}
		identity, err := user.Identity()
		if err != nil {
			return "", err
		}
		if len(identity.Private) == 0 {
			return "", errors.New("app: legacy public-key-only user has no recoverable keylink")
		}
		var boards []string
		for _, boardTag := range user.Boards {
			if hash := boardHashes[boardTag]; hash != "" {
				boards = append(boards, hash)
			}
		}
		return keylink.Build(identity.Private, r.serverKP.Public(), boards, user.Name)
	}
	return "", fmt.Errorf("app: user %q not found", tag)
}

func (r *ServerRuntime) Stats() telemetry.Stats {
	r.mu.RLock()
	cfg := cloneConfig(r.cfg)
	boards := make(map[string]*boardRuntime, len(r.boards))
	for tag, br := range r.boards {
		boards[tag] = br
	}
	r.mu.RUnlock()
	out := telemetry.Stats{StartedAt: r.started, Revision: r.Revision(), UsersConfigured: len(cfg.Users), BoardsConfigured: len(cfg.Boards)}
	out.RXBytesSinceStart, out.TXBytesSinceStart = r.registry.Totals()
	users := r.registry.List()
	for _, user := range users {
		us := telemetry.UserStats{
			Tag: user.ID, Name: user.Name, Enabled: user.Enabled,
			Connections: user.ActiveSessions, Online: user.ActiveSessions > 0,
			RXBytes: user.RXBytes, TXBytes: user.TXBytes,
			MaxSessions: user.MaxSessions, MaxLanes: user.MaxLanes,
		}
		if user.Enabled {
			out.UsersEnabled++
		}
		if us.Online {
			out.UsersOnline++
		}
		if !user.LastSeen.IsZero() {
			last := user.LastSeen
			us.LastSeen = &last
		}
		for _, br := range boards {
			_, _, _, srv := br.snapshot()
			if srv == nil {
				continue
			}
			for _, conn := range srv.UserConnections(user.ID) {
				us.RXBytes += conn.Received
				us.TXBytes += conn.Written
				us.Lanes += len(conn.Lanes)
				us.Streams += len(conn.Streams)
			}
		}
		out.ActiveConnections += us.Connections
		out.ActiveLanes += us.Lanes
		out.ActiveStreams += us.Streams
		out.Users = append(out.Users, us)
	}
	for _, board := range cfg.Boards {
		if board.IsEnabled() {
			out.BoardsEnabled++
		}
		bs := telemetry.BoardStats{Tag: board.Tag, Name: board.Name, Hash: board.Hash, Enabled: board.IsEnabled()}
		if br := boards[board.Tag]; br != nil {
			_, state, errText, srv := br.snapshot()
			bs.State, bs.Error = string(state), errText
			if state == BoardActive {
				out.BoardsRunning++
			}
			if srv != nil {
				hs := srv.Stats()
				bs.Clients, bs.FreePages = hs.Clients, hs.FreePages
				bs.RXBytes, bs.TXBytes = hs.Received, hs.Written
				out.RXBytesSinceStart += hs.Received
				out.TXBytesSinceStart += hs.Written
				bs.PageCleanupRuns, bs.PageCleanupDeleted = hs.PageCleanupRuns, hs.PageCleanupDeleted
				bs.PageCleanupFailures, bs.PageCleanupQuarantined = hs.PageCleanupFailures, hs.PageCleanupQuarantined
			}
		}
		out.Boards = append(out.Boards, bs)
	}
	t := r.reconnects.Snapshot()
	out.Transport = telemetry.TransportStats{
		DisconnectsTotal: t.DisconnectsTotal, ReconnectsTotal: t.ReconnectsTotal,
		ReconnectAttemptsFailed: t.ReconnectAttemptsFailed, CircuitOpenTotal: t.CircuitOpenTotal,
		SnapshotObjectsTotal: t.SnapshotObjectsTotal, SnapshotBytesTotal: t.SnapshotBytesTotal,
		ReconnectsLastMinute: t.ReconnectsLastMinute, SnapshotBytesLastMinute: t.SnapshotBytesLastMinute,
		LastDisconnectAt: timePointer(t.LastDisconnectAt), LastDisconnectReason: t.LastDisconnectReason,
		LastReconnectAt: timePointer(t.LastReconnectAt), LastDowntimeMillis: t.LastDowntime.Milliseconds(),
	}
	return out
}

func (r *ServerRuntime) RunMetricsLog(ctx context.Context) {
	ticker := time.NewTicker(serverMetricsInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s := r.Stats()
			r.log.Info("server stats", "revision", s.Revision, "boards_running", s.BoardsRunning,
				"online_users", s.UsersOnline, "connections", s.ActiveConnections,
				"lanes", s.ActiveLanes, "streams", s.ActiveStreams,
				"rx_bytes_since_start", s.RXBytesSinceStart, "tx_bytes_since_start", s.TXBytesSinceStart,
				"reconnects_1m", s.Transport.ReconnectsLastMinute)
		}
	}
}

// RunServer starts the config-driven runtime and its two adapters: the gRPC
// control plane and the optional read-only HTTP observability endpoint.
func RunServer(ctx context.Context, cfg serverconfig.Config, source string, log *slog.Logger, logs *logging.Buffer) error {
	runtime, err := NewServerRuntime(ctx, cfg, source, log, logs)
	if err != nil {
		return err
	}
	defer runtime.Close()
	errCh := make(chan error, 2)
	go func() { errCh <- controlapi.Serve(ctx, cfg.Management.GRPCListen, runtime, log) }()
	if cfg.Management.HTTPListen != "" {
		go func() {
			log.Info("HTTP observability listening", "address", cfg.Management.HTTPListen)
			errCh <- mgmt.Serve(ctx, cfg.Management.HTTPListen, mgmt.Handler(runtime, logs))
		}()
	}
	if cfg.Observability.Enabled {
		go runtime.RunMetricsLog(ctx)
	}
	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		return err
	}
}

func indexUsers(users []serverconfig.User) map[string]serverconfig.User {
	out := make(map[string]serverconfig.User, len(users))
	for _, user := range users {
		out[user.Tag] = user
	}
	return out
}

func userPolicyChanged(a, b serverconfig.User) bool {
	if a.IsEnabled() != b.IsEnabled() || a.MaxSessions != b.MaxSessions || a.MaxLanes != b.MaxLanes || len(a.Boards) != len(b.Boards) {
		return true
	}
	for i := range a.Boards {
		if a.Boards[i] != b.Boards[i] {
			return true
		}
	}
	return a.PrivateKey != b.PrivateKey || a.PublicKey != b.PublicKey
}

func upsertUser(users []serverconfig.User, candidate serverconfig.User) []serverconfig.User {
	for index := range users {
		if users[index].Tag == candidate.Tag {
			users[index] = candidate
			return users
		}
	}
	return append(users, candidate)
}

func removeUser(users []serverconfig.User, tag string) ([]serverconfig.User, error) {
	for index := range users {
		if users[index].Tag == tag {
			return append(users[:index], users[index+1:]...), nil
		}
	}
	return users, fmt.Errorf("app: user %q not found", tag)
}

func setUserEnabled(users []serverconfig.User, tag string, enabled bool) error {
	for index := range users {
		if users[index].Tag == tag {
			users[index].Enabled = boolPointer(enabled)
			return nil
		}
	}
	return fmt.Errorf("app: user %q not found", tag)
}

func upsertBoard(boards []serverconfig.Board, candidate serverconfig.Board) []serverconfig.Board {
	for index := range boards {
		if boards[index].Tag == candidate.Tag {
			boards[index] = candidate
			return boards
		}
	}
	return append(boards, candidate)
}

func removeBoard(boards []serverconfig.Board, tag string) ([]serverconfig.Board, error) {
	for index := range boards {
		if boards[index].Tag == tag {
			return append(boards[:index], boards[index+1:]...), nil
		}
	}
	return boards, fmt.Errorf("app: board %q not found", tag)
}

func setBoardEnabled(boards []serverconfig.Board, tag string, enabled bool) error {
	for index := range boards {
		if boards[index].Tag == tag {
			boards[index].Enabled = boolPointer(enabled)
			return nil
		}
	}
	return fmt.Errorf("app: board %q not found", tag)
}

func (r *ServerRuntime) publishConfigChanges(before, after serverconfig.Config, revision uint64) {
	oldUsers := indexUsers(before.Users)
	for _, user := range after.Users {
		old, exists := oldUsers[user.Tag]
		operation := "added"
		if exists {
			operation = "updated"
			if old.IsEnabled() != user.IsEnabled() {
				if user.IsEnabled() {
					operation = "enabled"
				} else {
					operation = "disabled"
				}
			} else if !userConfigChanged(old, user) {
				delete(oldUsers, user.Tag)
				continue
			}
		}
		r.events.Publish(runtimeevents.Event{
			Type: runtimeevents.ResourceChanged, RuntimeRevision: revision,
			ResourceKind: "user", ResourceOperation: operation, Tag: user.Tag,
		})
		delete(oldUsers, user.Tag)
	}
	for tag := range oldUsers {
		r.events.Publish(runtimeevents.Event{
			Type: runtimeevents.ResourceChanged, RuntimeRevision: revision,
			ResourceKind: "user", ResourceOperation: "removed", Tag: tag,
		})
	}

	oldBoards := make(map[string]serverconfig.Board, len(before.Boards))
	for _, board := range before.Boards {
		oldBoards[board.Tag] = board
	}
	for _, board := range after.Boards {
		old, exists := oldBoards[board.Tag]
		operation := "added"
		if exists {
			operation = "updated"
			if old.IsEnabled() != board.IsEnabled() {
				if board.IsEnabled() {
					operation = "enabled"
				} else {
					operation = "disabled"
				}
			} else if !boardConfigChanged(old, board) {
				delete(oldBoards, board.Tag)
				continue
			}
		}
		r.events.Publish(runtimeevents.Event{
			Type: runtimeevents.ResourceChanged, RuntimeRevision: revision,
			ResourceKind: "board", ResourceOperation: operation, Tag: board.Tag,
		})
		delete(oldBoards, board.Tag)
	}
	for tag := range oldBoards {
		r.events.Publish(runtimeevents.Event{
			Type: runtimeevents.ResourceChanged, RuntimeRevision: revision,
			ResourceKind: "board", ResourceOperation: "removed", Tag: tag,
		})
	}
}

func userConfigChanged(a, b serverconfig.User) bool {
	return a.Name != b.Name || userPolicyChanged(a, b)
}

func boardConfigChanged(a, b serverconfig.Board) bool {
	return a.Name != b.Name || a.Hash != b.Hash || a.HubSlide != b.HubSlide ||
		a.APIBase != b.APIBase || a.GuestName != b.GuestName ||
		a.IsEnabled() != b.IsEnabled() || a.MaxLanes != b.MaxLanes
}

func boardRuntimeCompatible(a, b serverconfig.Board) bool {
	return a.Hash == b.Hash && a.HubSlide == b.HubSlide && a.APIBase == b.APIBase && a.GuestName == b.GuestName
}

func runtimeGlobalsCompatible(a, b serverconfig.Config) bool {
	return a.Server == b.Server && a.Transport == b.Transport
}

func cloneConfig(in serverconfig.Config) serverconfig.Config {
	out := in
	out.Boards = append([]serverconfig.Board(nil), in.Boards...)
	out.Users = make([]serverconfig.User, len(in.Users))
	for i, user := range in.Users {
		out.Users[i] = user
		out.Users[i].Boards = append([]string(nil), user.Boards...)
	}
	return out
}

func equalBytes(a, b []byte) bool {
	return base64.RawStdEncoding.EncodeToString(a) == base64.RawStdEncoding.EncodeToString(b)
}

func timePointer(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func boolPointer(value bool) *bool { return &value }
