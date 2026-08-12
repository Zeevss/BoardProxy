package agent

import (
	"testing"
	"time"

	corev1 "bproxy-core/api/control/v1"
	"bproxy-node-agent/internal/localstore"
	nodev1 "bproxy-node-contracts/node/v1"

	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestPersistCoreRuntimeEventAdvancesCursorWithOutboxAtomically(t *testing.T) {
	store, err := localstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	event := &corev1.CoreRuntimeEvent{
		EventId: "boot-1:7", BootId: "boot-1", Sequence: 7, OccurredAt: timestamppb.New(time.Unix(10, 0)),
		RuntimeRevision: 3,
		Payload: &corev1.CoreRuntimeEvent_ClientSessionOpened{ClientSessionOpened: &corev1.ClientSessionOpened{
			UserTag: "alice", BoardTag: "main", BundleId: "bundle",
		}},
	}
	if err := persistCoreRuntimeEvent(store, coreEventCursor{BootID: "boot-1", Sequence: 7}, event); err != nil {
		t.Fatal(err)
	}
	cursor, err := loadCoreEventCursor(store)
	if err != nil || cursor.BootID != "boot-1" || cursor.Sequence != 7 {
		t.Fatalf("cursor=%+v err=%v", cursor, err)
	}
	pending, err := store.Pending()
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}
	got := pending[0].Event.GetRuntimeEvents().GetEvents()[0]
	if got.GetClientSessionOpened().GetUserTag() != "alice" || got.GetSequence() != 7 {
		t.Fatalf("event=%+v", got)
	}
}

func TestRuntimeEventOutboxWakeupIsImmediate(t *testing.T) {
	store, err := localstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	event := &nodev1.NodeEvent{Payload: &nodev1.NodeEvent_RuntimeEvents{RuntimeEvents: &nodev1.RuntimeEventBatch{
		BatchId: "batch", Events: []*nodev1.CoreRuntimeEvent{{EventId: "event", CoreBootId: "boot", Sequence: 1}},
	}}}
	if err := store.CommitCollection(nil, map[string]*nodev1.NodeEvent{"batch": event}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-store.Changes():
	case <-time.After(time.Second):
		t.Fatal("outbox did not publish wake-up")
	}
}

func TestResetAndSnapshotCommitAtomicallyInDeliveryOrder(t *testing.T) {
	store, err := localstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	reset := &corev1.CoreRuntimeEvent{
		EventId: "boot:reset", BootId: "boot", OccurredAt: timestamppb.Now(),
		Payload: &corev1.CoreRuntimeEvent_StreamReset{StreamReset: &corev1.EventStreamReset{Reason: "event_gap"}},
	}
	snapshot := &corev1.RuntimeSnapshot{
		EventBootId: "boot", LatestEventSequence: 9,
		Runtime: &corev1.RuntimeInfo{Revision: 4},
		Stats: &corev1.RuntimeStats{Users: []*corev1.UserRuntimeStats{{
			Tag: "alice", Connections: 2, RxBytesSinceStart: 10, TxBytesSinceStart: 20,
		}}},
		Boards: []*corev1.BoardInfo{{Config: &corev1.BoardSpec{Tag: "main"}, State: "active"}},
	}
	if err := persistCoreResetAndSnapshot(store, coreEventCursor{BootID: "boot", Sequence: 9}, reset, snapshot); err != nil {
		t.Fatal(err)
	}
	cursor, err := loadCoreEventCursor(store)
	if err != nil || cursor.Sequence != 9 {
		t.Fatalf("cursor=%+v err=%v", cursor, err)
	}
	pending, err := store.Pending()
	if err != nil || len(pending) != 2 {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}
	if pending[0].Event.GetRuntimeEvents().GetEvents()[0].GetStreamReset() == nil {
		t.Fatal("reset must be delivered before snapshot")
	}
	got := pending[1].Event.GetRuntimeEvents().GetSnapshot()
	if got.GetLatestSequence() != 9 || got.GetUsers()[0].GetActiveSessions() != 2 {
		t.Fatalf("snapshot=%+v", got)
	}
}
