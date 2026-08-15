package recoveryworker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Zeevss/BoardProxy/subscribe/internal/controlplane"
	"github.com/Zeevss/BoardProxy/subscribe/internal/yandex"
	"github.com/Zeevss/BoardProxy/subscribe/protocol"
	"github.com/Zeevss/BoardProxy/subscribe/recovery"
)

type Resolver interface {
	ResolveRecoveryKey(ctx context.Context, publicKey string) (protocol.Subscription, error)
}

type Worker struct {
	yandexURL string
	keyID     string
	private   []byte
	resolver  Resolver
	logger    *slog.Logger
	ready     atomic.Bool
	mu        sync.Mutex
	seen      map[string]time.Time
}

func New(yandexURL, keyID string, private []byte, resolver Resolver, logger *slog.Logger) *Worker {
	return &Worker{
		yandexURL: yandexURL, keyID: keyID, private: append([]byte(nil), private...),
		resolver: resolver, logger: logger, seen: make(map[string]time.Time),
	}
}

func (w *Worker) Ready() bool { return w.ready.Load() }

func (w *Worker) Run(ctx context.Context) error {
	backoff := time.Second
	for ctx.Err() == nil {
		err := w.runSession(ctx)
		w.ready.Store(false)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		w.logger.Error("Yandex recovery session stopped", "error", err, "retry", backoff)
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
	return ctx.Err()
}

func (w *Worker) runSession(ctx context.Context) error {
	client, err := yandex.Open(ctx, w.yandexURL)
	if err != nil {
		return err
	}
	for _, thread := range client.Threads("") {
		if hasResponse(thread) {
			continue
		}
		if err := w.handle(ctx, client, thread.Root); err != nil {
			w.logger.Warn("cannot handle recovered Yandex hello", "comment", thread.Root.ID, "error", err)
		}
	}
	events := make(chan yandex.Event, 64)
	ready := make(chan struct{}, 1)
	watchResult := make(chan error, 1)
	go func() { watchResult <- client.Watch(ctx, ready, events) }()
	select {
	case <-ready:
		w.ready.Store(true)
		w.logger.Info("Yandex recovery watcher is ready", "resource", client.ResourceName())
	case err := <-watchResult:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
	for {
		select {
		case event := <-events:
			if event.Comment.ParentID != "" {
				continue
			}
			if err := w.handle(ctx, client, event.Comment); err != nil {
				w.logger.Warn("cannot answer Yandex recovery hello", "bundle", event.BundleID, "error", err)
			}
		case err := <-watchResult:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func hasResponse(thread yandex.Thread) bool {
	hello, err := protocol.DecodeFrame(thread.Root.Text)
	if err != nil || hello.Type != "hello" {
		return true
	}
	for _, reply := range thread.Replies {
		frame, err := protocol.DecodeFrame(reply.Text)
		if err == nil && frame.Type == "response" && frame.RequestID == hello.RequestID {
			return true
		}
	}
	return false
}

func (w *Worker) handle(ctx context.Context, client *yandex.Client, comment yandex.Comment) error {
	frame, err := protocol.DecodeFrame(comment.Text)
	if err != nil || frame.Type != "hello" || frame.KeyID != w.keyID || frame.Parts != 1 {
		return nil
	}
	if w.duplicate(frame.RequestID) {
		return nil
	}
	responder, err := recovery.Respond(w.private, frame.Payload)
	if err != nil {
		return err
	}
	var hello protocol.ClientHello
	if err := json.Unmarshal(responder.Payload(), &hello); err != nil {
		return fmt.Errorf("decode recovery hello: %w", err)
	}
	if hello.Version != 1 || hello.RequestID != frame.RequestID || hello.RequestedFormat != "boardproxy-keylink" {
		return errors.New("recovery hello metadata mismatch")
	}
	if delta := time.Since(hello.CreatedAt); delta < -time.Minute || delta > 10*time.Minute {
		return errors.New("recovery hello is outside the accepted time window")
	}
	snapshot, resolveErr := w.resolver.ResolveRecoveryKey(ctx, protocol.EncodeKey(responder.PeerStatic()))
	answer := protocol.ServerHello{Version: 1, RequestID: frame.RequestID, Subscription: snapshot}
	if resolveErr != nil {
		answer.Subscription = protocol.Subscription{}
		answer.Error = recoveryError(resolveErr)
	}
	payload, err := json.Marshal(answer)
	if err != nil {
		return err
	}
	message, err := responder.Accept(payload)
	if err != nil {
		return err
	}
	frames, err := responseFrames(frame.RequestID, w.keyID, message)
	if err != nil {
		return err
	}
	for index, text := range frames {
		if _, err := client.AddReply(ctx, comment.ID, text); err == nil {
			continue
		} else if !errors.Is(err, yandex.ErrNotFound) {
			return err
		}
		if err := createStandalone(ctx, client, frame.RequestID, index, text); err != nil {
			return err
		}
	}
	w.remember(frame.RequestID)
	w.logger.Info("answered Yandex recovery request", "request", frame.RequestID, "parts", len(frames), "keys", len(snapshot.Keys), "error", answer.Error)
	return nil
}

func recoveryError(err error) *protocol.RecoveryError {
	var status *controlplane.StatusError
	if !errors.As(err, &status) {
		return &protocol.RecoveryError{Code: "temporary", Message: "subscription backend unavailable"}
	}
	switch status.Status {
	case http.StatusNotFound:
		return &protocol.RecoveryError{Code: "not_found", Message: "subscription not found"}
	case http.StatusForbidden:
		return &protocol.RecoveryError{Code: "disabled", Message: "subscription is disabled"}
	case http.StatusGone:
		return &protocol.RecoveryError{Code: "revoked", Message: "subscription is revoked"}
	default:
		return &protocol.RecoveryError{Code: "temporary", Message: "subscription backend unavailable"}
	}
}

func responseFrames(requestID, keyID string, message []byte) ([]string, error) {
	const chunkSize = 6 << 10
	parts := (len(message) + chunkSize - 1) / chunkSize
	if parts < 1 || parts > 16 {
		return nil, errors.New("recovery response is too large")
	}
	result := make([]string, 0, parts)
	for index := 0; index < parts; index++ {
		end := (index + 1) * chunkSize
		if end > len(message) {
			end = len(message)
		}
		text, err := protocol.EncodeFrame(protocol.Frame{
			Type: "response", RequestID: requestID, KeyID: keyID,
			Part: index + 1, Parts: parts, Payload: message[index*chunkSize : end],
		})
		if err != nil {
			return nil, err
		}
		result = append(result, text)
	}
	return result, nil
}

func createStandalone(ctx context.Context, client *yandex.Client, requestID string, part int, text string) error {
	for attempt := 0; attempt < 12; attempt++ {
		cell := protocol.MailboxCell(requestID+":response:"+strconv.Itoa(part), attempt)
		_, err := client.CreateThread(ctx, cell, text)
		if err == nil {
			return nil
		}
		if !errors.Is(err, yandex.ErrThreadExists) && !errors.Is(err, yandex.ErrConflict) {
			return err
		}
	}
	return errors.New("cannot allocate a standalone Yandex response cell")
}

func (w *Worker) duplicate(requestID string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := time.Now()
	for id, expiry := range w.seen {
		if now.After(expiry) {
			delete(w.seen, id)
		}
	}
	_, exists := w.seen[strings.ToLower(requestID)]
	return exists
}

func (w *Worker) remember(requestID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.seen[strings.ToLower(requestID)] = time.Now().Add(15 * time.Minute)
}
