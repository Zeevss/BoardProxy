package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"time"

	corev1 "bproxy-core/api/control/v1"
	"bproxy-node-agent/internal/coremgr"
	"bproxy-node-agent/internal/localstore"
	nodev1 "bproxy-node-contracts/node/v1"

	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	coreEventRetryMin  = time.Second
	coreEventRetryMax  = 30 * time.Second
	runtimeSnapshotGap = 30 * time.Second
)

// Runtime state travels as a whole snapshot, not as a replayable event log. The
// node already knows its state, so there is nothing for the hub to reconstruct —
// and therefore no sequences, gap detection, resets or replay to get right.
//
// Events are still forwarded, but only as an activity log for the panel: they
// project nothing, so a lost event is harmless.
func collectCoreRuntimeEvents(ctx context.Context, core *coremgr.Manager, store *localstore.Store, log *slog.Logger) {
	delay := coreEventRetryMin
	for ctx.Err() == nil {
		stream, err := core.WatchEvents(ctx, "", 0)
		if err == nil {
			delay = coreEventRetryMin
			err = consumeCoreRuntimeEvents(ctx, core, stream, store, log)
			stream.Close()
		}
		if ctx.Err() != nil {
			return
		}
		if err != nil && !errors.Is(err, io.EOF) {
			log.Debug("core runtime event stream unavailable", "err", err, "retry_in", delay)
		}
		if wait(ctx, delay) != nil {
			return
		}
		delay = min(delay*2, coreEventRetryMax)
	}
}

func consumeCoreRuntimeEvents(
	ctx context.Context,
	core *coremgr.Manager,
	stream coremgr.RuntimeEventStream,
	store *localstore.Store,
	log *slog.Logger,
) error {
	lastSnapshot := time.Time{}
	for ctx.Err() == nil {
		event, err := stream.Recv()
		if err != nil {
			return err
		}
		reports := make([]localstore.OutboxEvent, 0, 2)

		if mapped := mapCoreRuntimeEvent(event); mapped != nil {
			batchID := randomID()
			reports = append(reports, localstore.OutboxEvent{
				BatchID: batchID,
				Event:   &nodev1.ReportRequest{BatchId: batchID, Events: []*nodev1.RuntimeEvent{mapped}},
			})
		}

		// A snapshot is cheap and self-correcting, so it is taken on a timer
		// rather than in reaction to a detected gap: there are no gaps to detect.
		if time.Since(lastSnapshot) >= runtimeSnapshotGap {
			snapshot, err := core.RuntimeSnapshot(ctx)
			if err != nil {
				log.Debug("capture core runtime snapshot", "err", err)
			} else {
				batchID := randomID()
				reports = append(reports, localstore.OutboxEvent{
					BatchID: batchID,
					Event:   &nodev1.ReportRequest{BatchId: batchID, Runtime: mapRuntimeSnapshot(snapshot)},
				})
				lastSnapshot = time.Now()
			}
		}

		if len(reports) == 0 {
			continue
		}
		if err := store.CommitOrderedCollection(nil, reports); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func mapRuntimeSnapshot(snapshot *corev1.RuntimeSnapshot) *nodev1.RuntimeSnapshot {
	if snapshot == nil {
		return nil
	}
	users := make([]*nodev1.RuntimeUserSnapshot, 0, len(snapshot.GetUsers()))
	for _, user := range snapshot.GetUsers() {
		// ActiveLanes остаётся нулём: ядро отдаёт только лимит полос, а
		// выдавать лимит за наблюдение — врать панели.
		users = append(users, &nodev1.RuntimeUserSnapshot{
			UserId:         user.GetTag(),
			ActiveSessions: uint32(max(user.GetActiveSessions(), 0)),
		})
	}
	boards := make([]*nodev1.RuntimeBoardSnapshot, 0, len(snapshot.GetBoards()))
	for _, board := range snapshot.GetBoards() {
		boards = append(boards, &nodev1.RuntimeBoardSnapshot{
			BoardId:   board.GetConfig().GetTag(),
			State:     board.GetState(),
			LastError: board.GetError(),
		})
	}
	return &nodev1.RuntimeSnapshot{
		CoreBootId: snapshot.GetEventBootId(),
		CapturedAt: timestamppb.Now(),
		Users:      users,
		Boards:     boards,
	}
}

// mapCoreRuntimeEvent renders a core event as a typed line for the panel.
// nil means the event carries nothing worth logging.
func mapCoreRuntimeEvent(event *corev1.CoreRuntimeEvent) *nodev1.RuntimeEvent {
	if event == nil || event.GetStreamReset() != nil {
		return nil
	}
	kind, payload := describeCoreRuntimeEvent(event)
	if kind == "" {
		return nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		raw = []byte("{}")
	}
	occurredAt := event.GetOccurredAt()
	if occurredAt == nil {
		occurredAt = timestamppb.Now()
	}
	return &nodev1.RuntimeEvent{Type: kind, OccurredAt: occurredAt, PayloadJson: string(raw)}
}

func describeCoreRuntimeEvent(event *corev1.CoreRuntimeEvent) (string, map[string]any) {
	switch {
	case event.GetResourceChanged() != nil:
		changed := event.GetResourceChanged()
		return "resource.changed", map[string]any{
			"kind":      changed.GetKind().String(),
			"tag":       changed.GetTag(),
			"operation": changed.GetOperation().String(),
		}
	case event.GetBoardStateChanged() != nil:
		changed := event.GetBoardStateChanged()
		return "board.state-changed", map[string]any{
			"boardId":  changed.GetBoardTag(),
			"previous": changed.GetPreviousState(),
			"state":    changed.GetState(),
			"error":    changed.GetError(),
		}
	case event.GetClientSessionOpened() != nil:
		opened := event.GetClientSessionOpened()
		return "session.opened", map[string]any{
			"userId":   opened.GetUserTag(),
			"boardId":  opened.GetBoardTag(),
			"bundleId": opened.GetBundleId(),
		}
	case event.GetClientSessionClosed() != nil:
		closed := event.GetClientSessionClosed()
		return "session.closed", map[string]any{
			"userId":   closed.GetUserTag(),
			"boardId":  closed.GetBoardTag(),
			"bundleId": closed.GetBundleId(),
			"rxBytes":  closed.GetRxBytes(),
			"txBytes":  closed.GetTxBytes(),
			"reason":   closed.GetReason(),
		}
	default:
		return "", nil
	}
}
