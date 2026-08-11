package stats

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	corev1 "bproxy-core/api/control/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type memoryCheckpoints map[string][]byte

func (m memoryCheckpoints) Checkpoint(key string) ([]byte, error) { return m[key], nil }

type fakeUserSource struct {
	stats *corev1.RuntimeStats
	err   error
}

func (f *fakeUserSource) Snapshot(context.Context) (*corev1.RuntimeStats, error) {
	return f.stats, f.err
}

func TestInterfaceTrafficUsesDeltasAndResetSafeCounters(t *testing.T) {
	root := t.TempDir()
	checkpoint := memoryCheckpoints{}
	writeCounters(t, root, "eth0", []uint64{100, 200, 10, 20, 1, 2, 3, 4})
	collector := New([]string{"eth0"}, root, "unused", checkpoint)
	start := time.Unix(100, 0).UTC()
	raw, event, err := collector.collectInterfaces(start)
	if err != nil || event != nil {
		t.Fatalf("baseline event=%v err=%v", event, err)
	}
	checkpoint[interfaceCheckpointKey] = raw
	writeCounters(t, root, "eth0", []uint64{150, 260, 15, 27, 1, 3, 5, 7})
	raw, event, err = collector.collectInterfaces(start.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	delta := event.GetInterfaceTraffic().GetInterfaces()[0]
	if delta.GetRxBytes() != 50 || delta.GetTxBytes() != 60 || delta.GetRxPackets() != 5 || delta.GetTxDropped() != 3 {
		t.Fatalf("unexpected delta: %+v", delta)
	}
	checkpoint[interfaceCheckpointKey] = raw
	writeCounters(t, root, "eth0", []uint64{7, 9, 1, 1, 0, 0, 0, 0})
	_, event, err = collector.collectInterfaces(start.Add(2 * time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	delta = event.GetInterfaceTraffic().GetInterfaces()[0]
	if delta.GetRxBytes() != 7 || delta.GetTxBytes() != 9 {
		t.Fatalf("counter reset delta: %+v", delta)
	}
}

func TestNetworkNamespaceChangeEstablishesNewBaseline(t *testing.T) {
	root := t.TempDir()
	checkpoint := memoryCheckpoints{}
	writeCounters(t, root, "eth0", []uint64{100, 200, 1, 2, 0, 0, 0, 0})
	collector := NewWithUserSource([]string{"eth0"}, root, &fakeUserSource{err: errors.New("unused")}, checkpoint)
	collector.namespaceID = "net:[first]"
	start := time.Unix(500, 0).UTC()
	raw, _, err := collector.collectInterfaces(start)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint[interfaceCheckpointKey] = raw
	collector.namespaceID = "net:[second]"
	writeCounters(t, root, "eth0", []uint64{500, 600, 5, 6, 0, 0, 0, 0})
	raw, event, err := collector.collectInterfaces(start.Add(time.Minute))
	if err != nil || event != nil {
		t.Fatalf("namespace replacement must be a baseline: event=%v err=%v", event, err)
	}
	checkpoint[interfaceCheckpointKey] = raw
	writeCounters(t, root, "eth0", []uint64{505, 609, 7, 9, 0, 0, 0, 0})
	_, event, err = collector.collectInterfaces(start.Add(2 * time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	delta := event.GetInterfaceTraffic().GetInterfaces()[0]
	if delta.GetRxBytes() != 5 || delta.GetTxBytes() != 9 {
		t.Fatalf("new namespace delta: %+v", delta)
	}
}

func TestUserTrafficUsesPerTagDeltasAndCoreEpoch(t *testing.T) {
	checkpoint := memoryCheckpoints{}
	started := time.Unix(200, 0).UTC()
	source := &fakeUserSource{stats: userStats(started, 100, 200)}
	collector := NewWithUserSource(nil, "unused", source, checkpoint)
	raw, event, err := collector.collectUsers(context.Background(), started.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	delta := event.GetUserTraffic().GetUsers()[0]
	if delta.GetUserTag() != "alice" || delta.GetRxBytes() != 100 || delta.GetTxBytes() != 200 {
		t.Fatalf("initial user delta: %+v", delta)
	}
	checkpoint[userCheckpointKey] = raw
	source.stats = userStats(started, 125, 260)
	raw, event, err = collector.collectUsers(context.Background(), started.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	delta = event.GetUserTraffic().GetUsers()[0]
	if delta.GetRxBytes() != 25 || delta.GetTxBytes() != 60 {
		t.Fatalf("incremental user delta: %+v", delta)
	}
	checkpoint[userCheckpointKey] = raw
	source.stats = userStats(started.Add(3*time.Minute), 7, 9)
	_, event, err = collector.collectUsers(context.Background(), started.Add(4*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	delta = event.GetUserTraffic().GetUsers()[0]
	if delta.GetRxBytes() != 7 || delta.GetTxBytes() != 9 {
		t.Fatalf("new core epoch delta: %+v", delta)
	}
}

func TestBrokenInterfaceDoesNotBlockUserStream(t *testing.T) {
	checkpoint := memoryCheckpoints{}
	started := time.Unix(300, 0).UTC()
	collector := NewWithUserSource([]string{"missing"}, t.TempDir(), &fakeUserSource{stats: userStats(started, 11, 13)}, checkpoint)
	checkpoints, events, err := collector.Collect(context.Background(), started.Add(time.Minute))
	if err == nil {
		t.Fatal("missing interface error was not reported")
	}
	if len(checkpoints[userCheckpointKey]) == 0 || len(events) != 1 {
		t.Fatalf("user stream was blocked: checkpoints=%v events=%v", checkpoints, events)
	}
}

func TestBrokenUserSourceDoesNotBlockInterfaceStream(t *testing.T) {
	root := t.TempDir()
	checkpoint := memoryCheckpoints{}
	writeCounters(t, root, "eth0", []uint64{1, 2, 3, 4, 0, 0, 0, 0})
	collector := NewWithUserSource([]string{"eth0"}, root, &fakeUserSource{err: errors.New("core unavailable")}, checkpoint)
	start := time.Unix(400, 0).UTC()
	checkpoints, _, err := collector.Collect(context.Background(), start)
	if err == nil || len(checkpoints[interfaceCheckpointKey]) == 0 {
		t.Fatalf("interface baseline was blocked: checkpoints=%v err=%v", checkpoints, err)
	}
	checkpoint[interfaceCheckpointKey] = checkpoints[interfaceCheckpointKey]
	writeCounters(t, root, "eth0", []uint64{6, 9, 5, 8, 0, 0, 0, 0})
	_, events, err := collector.Collect(context.Background(), start.Add(time.Minute))
	if err == nil || len(events) != 1 {
		t.Fatalf("interface event was blocked: events=%v err=%v", events, err)
	}
}

func userStats(started time.Time, rx, tx uint64) *corev1.RuntimeStats {
	return &corev1.RuntimeStats{StartedAt: timestamppb.New(started), Users: []*corev1.UserRuntimeStats{{
		Tag: "alice", RxBytesSinceStart: rx, TxBytesSinceStart: tx,
	}}}
}

func writeCounters(t *testing.T, root, name string, values []uint64) {
	t.Helper()
	dir := filepath.Join(root, name, "statistics")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for index, field := range []string{"rx_bytes", "tx_bytes", "rx_packets", "tx_packets", "rx_errors", "tx_errors", "rx_dropped", "tx_dropped"} {
		if err := os.WriteFile(filepath.Join(dir, field), []byte(strconv.FormatUint(values[index], 10)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
