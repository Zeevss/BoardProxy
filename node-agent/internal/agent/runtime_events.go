package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	coreEventCheckpointKey = "core-runtime-events"
	coreEventRetryMin      = time.Second
	coreEventRetryMax      = 30 * time.Second
)

type coreEventCursor struct {
	BootID   string `json:"boot_id"`
	Sequence uint64 `json:"sequence"`
}

func collectCoreRuntimeEvents(ctx context.Context, core *coremgr.Manager, store *localstore.Store, log *slog.Logger) {
	cursor, err := loadCoreEventCursor(store)
	if err != nil {
		log.Error("load core runtime event cursor", "err", err)
		return
	}
	delay := coreEventRetryMin
	for ctx.Err() == nil {
		stream, err := core.WatchEvents(ctx, cursor.BootID, cursor.Sequence)
		if err == nil {
			delay = coreEventRetryMin
			err = consumeCoreRuntimeEvents(ctx, core, stream, store, &cursor)
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

func consumeCoreRuntimeEvents(ctx context.Context, core *coremgr.Manager, stream coremgr.RuntimeEventStream, store *localstore.Store, cursor *coreEventCursor) error {
	allowGap := false
	for ctx.Err() == nil {
		event, err := stream.Recv()
		if err != nil {
			return err
		}
		if event.GetEventId() == "" || event.GetBootId() == "" {
			return errors.New("core runtime event is missing identity")
		}
		if event.GetStreamReset() != nil {
			snapshot, snapshotErr := core.RuntimeSnapshot(ctx)
			if snapshotErr != nil {
				return fmt.Errorf("capture core runtime snapshot after reset: %w", snapshotErr)
			}
			cursor.BootID, cursor.Sequence = snapshot.GetEventBootId(), snapshot.GetLatestEventSequence()
			if cursor.BootID != event.GetBootId() {
				return fmt.Errorf("core changed while capturing runtime snapshot: reset=%s snapshot=%s", event.GetBootId(), cursor.BootID)
			}
			if err := persistCoreResetAndSnapshot(store, *cursor, event, snapshot); err != nil {
				return err
			}
			allowGap = true
		} else {
			if event.GetSequence() == 0 {
				return errors.New("core runtime event sequence is zero")
			}
			if cursor.BootID != "" && cursor.BootID != event.GetBootId() {
				return fmt.Errorf("core boot changed without stream reset: %s -> %s", cursor.BootID, event.GetBootId())
			}
			if cursor.BootID == event.GetBootId() && event.GetSequence() <= cursor.Sequence {
				continue
			}
			if cursor.Sequence != 0 && event.GetSequence() != cursor.Sequence+1 && !allowGap {
				return fmt.Errorf("core runtime event sequence gap: after=%d next=%d", cursor.Sequence, event.GetSequence())
			}
			cursor.BootID, cursor.Sequence = event.GetBootId(), event.GetSequence()
			allowGap = false
		}
		if event.GetStreamReset() == nil {
			if err := persistCoreRuntimeEvent(store, *cursor, event); err != nil {
				return err
			}
		}
	}
	return ctx.Err()
}

func persistCoreResetAndSnapshot(store *localstore.Store, cursor coreEventCursor, reset *corev1.CoreRuntimeEvent, snapshot *corev1.RuntimeSnapshot) error {
	rawCursor, err := json.Marshal(cursor)
	if err != nil {
		return err
	}
	resetBatchID, snapshotBatchID := randomID(), randomID()
	return store.CommitOrderedCollection(
		map[string][]byte{coreEventCheckpointKey: rawCursor},
		[]localstore.OutboxEvent{
			{BatchID: resetBatchID, Event: runtimeEventNodeBatch(resetBatchID, mapCoreRuntimeEvent(reset))},
			{BatchID: snapshotBatchID, Event: runtimeSnapshotNodeBatch(snapshotBatchID, snapshot)},
		},
	)
}

func persistCoreRuntimeEvent(store *localstore.Store, cursor coreEventCursor, event *corev1.CoreRuntimeEvent) error {
	rawCursor, err := json.Marshal(cursor)
	if err != nil {
		return err
	}
	batchID := randomID()
	nodeEvent := runtimeEventNodeBatch(batchID, mapCoreRuntimeEvent(event))
	return store.CommitCollection(
		map[string][]byte{coreEventCheckpointKey: rawCursor},
		map[string]*nodev1.NodeEvent{batchID: nodeEvent},
	)
}

func runtimeEventNodeBatch(batchID string, event *nodev1.CoreRuntimeEvent) *nodev1.NodeEvent {
	return &nodev1.NodeEvent{Payload: &nodev1.NodeEvent_RuntimeEvents{RuntimeEvents: &nodev1.RuntimeEventBatch{
		BatchId: batchID, Events: []*nodev1.CoreRuntimeEvent{event},
	}}}
}

func runtimeSnapshotNodeBatch(batchID string, snapshot *corev1.RuntimeSnapshot) *nodev1.NodeEvent {
	value := &nodev1.RuntimeSnapshot{
		CoreBootId: snapshot.GetEventBootId(), LatestSequence: snapshot.GetLatestEventSequence(),
		RuntimeRevision: snapshot.GetRuntime().GetRevision(), CapturedAt: timestamppb.Now(),
	}
	for _, user := range snapshot.GetStats().GetUsers() {
		value.Users = append(value.Users, &nodev1.RuntimeUserSnapshot{
			UserTag: user.GetTag(), ActiveSessions: uint64(user.GetConnections()),
			RxBytes: user.GetRxBytesSinceStart(), TxBytes: user.GetTxBytesSinceStart(),
		})
	}
	for _, board := range snapshot.GetBoards() {
		value.Boards = append(value.Boards, &nodev1.RuntimeBoardSnapshot{
			BoardTag: board.GetConfig().GetTag(), State: board.GetState(), Error: board.GetError(),
		})
	}
	return &nodev1.NodeEvent{Payload: &nodev1.NodeEvent_RuntimeEvents{RuntimeEvents: &nodev1.RuntimeEventBatch{
		BatchId: batchID, Snapshot: value,
	}}}
}

func loadCoreEventCursor(store interface{ Checkpoint(string) ([]byte, error) }) (coreEventCursor, error) {
	raw, err := store.Checkpoint(coreEventCheckpointKey)
	if err != nil || len(raw) == 0 {
		return coreEventCursor{}, err
	}
	var cursor coreEventCursor
	return cursor, json.Unmarshal(raw, &cursor)
}

func mapCoreRuntimeEvent(event *corev1.CoreRuntimeEvent) *nodev1.CoreRuntimeEvent {
	out := &nodev1.CoreRuntimeEvent{
		EventId: event.GetEventId(), CoreBootId: event.GetBootId(), Sequence: event.GetSequence(),
		OccurredAt: event.GetOccurredAt(), RuntimeRevision: event.GetRuntimeRevision(),
	}
	switch payload := event.GetPayload().(type) {
	case *corev1.CoreRuntimeEvent_ResourceChanged:
		value := payload.ResourceChanged
		out.Payload = &nodev1.CoreRuntimeEvent_ResourceChanged{ResourceChanged: &nodev1.ResourceChanged{
			Kind: nodev1.ResourceKind(value.GetKind()), Operation: nodev1.ResourceOperation(value.GetOperation()), Tag: value.GetTag(),
		}}
	case *corev1.CoreRuntimeEvent_BoardStateChanged:
		value := payload.BoardStateChanged
		out.Payload = &nodev1.CoreRuntimeEvent_BoardStateChanged{BoardStateChanged: &nodev1.BoardStateChanged{
			BoardTag: value.GetBoardTag(), PreviousState: value.GetPreviousState(), State: value.GetState(), Error: value.GetError(),
		}}
	case *corev1.CoreRuntimeEvent_ClientSessionOpened:
		value := payload.ClientSessionOpened
		out.Payload = &nodev1.CoreRuntimeEvent_ClientSessionOpened{ClientSessionOpened: &nodev1.ClientSessionOpened{
			UserTag: value.GetUserTag(), BoardTag: value.GetBoardTag(), BundleId: value.GetBundleId(),
		}}
	case *corev1.CoreRuntimeEvent_ClientSessionClosed:
		value := payload.ClientSessionClosed
		out.Payload = &nodev1.CoreRuntimeEvent_ClientSessionClosed{ClientSessionClosed: &nodev1.ClientSessionClosed{
			UserTag: value.GetUserTag(), BoardTag: value.GetBoardTag(), BundleId: value.GetBundleId(),
			RxBytes: value.GetRxBytes(), TxBytes: value.GetTxBytes(), Reason: value.GetReason(),
		}}
	case *corev1.CoreRuntimeEvent_StreamReset:
		value := payload.StreamReset
		out.Payload = &nodev1.CoreRuntimeEvent_StreamReset{StreamReset: &nodev1.EventStreamReset{
			Reason: value.GetReason(), OldestAvailableSequence: value.GetOldestAvailableSequence(), LatestSequence: value.GetLatestSequence(),
		}}
	}
	return out
}
