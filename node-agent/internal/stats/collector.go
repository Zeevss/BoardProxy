package stats

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	nodev1 "bproxy-control-plane/api/node/v1"
	corev1 "bproxy-core/api/control/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	interfaceCheckpointKey = "interface"
	userCheckpointKey      = "users"
)

type Checkpoints interface {
	Checkpoint(key string) ([]byte, error)
}

type Collector struct {
	interfaces  []string
	sysClassNet string
	namespaceID string
	users       UserStatsSource
	checkpoints Checkpoints
}

type UserStatsSource interface {
	Snapshot(context.Context) (*corev1.RuntimeStats, error)
}

type grpcUserStatsSource struct{ address string }

type interfaceCheckpoint struct {
	SampledAt   time.Time                   `json:"sampled_at"`
	NamespaceID string                      `json:"namespace_id"`
	Counters    map[string]interfaceCounter `json:"counters"`
}

type interfaceCounter struct {
	RXBytes, TXBytes, RXPackets, TXPackets   uint64
	RXErrors, TXErrors, RXDropped, TXDropped uint64
}

type userCheckpoint struct {
	SampledAt   time.Time              `json:"sampled_at"`
	CoreStarted time.Time              `json:"core_started_at"`
	Users       map[string]userCounter `json:"users"`
}

type userCounter struct{ RXBytes, TXBytes uint64 }

func New(interfaces []string, sysClassNet, coreAddress string, checkpoints Checkpoints) *Collector {
	return NewWithUserSource(interfaces, sysClassNet, &grpcUserStatsSource{address: coreAddress}, checkpoints)
}

func NewWithUserSource(interfaces []string, sysClassNet string, users UserStatsSource, checkpoints Checkpoints) *Collector {
	namespaceID, _ := os.Readlink("/proc/self/ns/net")
	return &Collector{interfaces: interfaces, sysClassNet: sysClassNet, namespaceID: namespaceID, users: users, checkpoints: checkpoints}
}

// Collect returns checkpoint updates and outbox events that must be committed
// in one local-store transaction.
func (c *Collector) Collect(ctx context.Context, now time.Time) (map[string][]byte, map[string]*nodev1.NodeEvent, error) {
	checkpoints := make(map[string][]byte)
	events := make(map[string]*nodev1.NodeEvent)
	var collectionErrors []error
	interfaceRaw, interfaceEvent, err := c.collectInterfaces(now)
	if err == nil {
		checkpoints[interfaceCheckpointKey] = interfaceRaw
		if interfaceEvent != nil {
			events[interfaceEvent.GetInterfaceTraffic().GetBatchId()] = interfaceEvent
		}
	} else {
		collectionErrors = append(collectionErrors, err)
	}
	userRaw, userEvent, err := c.collectUsers(ctx, now)
	if err == nil {
		checkpoints[userCheckpointKey] = userRaw
		if userEvent != nil {
			events[userEvent.GetUserTraffic().GetBatchId()] = userEvent
		}
	} else {
		collectionErrors = append(collectionErrors, fmt.Errorf("stats: user traffic: %w", err))
	}
	return checkpoints, events, errors.Join(collectionErrors...)
}

func (c *Collector) collectInterfaces(now time.Time) ([]byte, *nodev1.NodeEvent, error) {
	previous := interfaceCheckpoint{Counters: make(map[string]interfaceCounter)}
	if raw, err := c.checkpoints.Checkpoint(interfaceCheckpointKey); err != nil {
		return nil, nil, err
	} else if len(raw) > 0 {
		if err := json.Unmarshal(raw, &previous); err != nil {
			return nil, nil, err
		}
	}
	current := interfaceCheckpoint{SampledAt: now, NamespaceID: c.namespaceID, Counters: make(map[string]interfaceCounter)}
	var deltas []*nodev1.InterfaceTrafficDelta
	for _, name := range c.interfaces {
		counter, err := readInterface(filepath.Join(c.sysClassNet, name, "statistics"))
		if err != nil {
			return nil, nil, fmt.Errorf("stats: interface %s: %w", name, err)
		}
		current.Counters[name] = counter
		before := previous.Counters[name]
		deltas = append(deltas, &nodev1.InterfaceTrafficDelta{
			Interface: name, RxBytes: delta(counter.RXBytes, before.RXBytes), TxBytes: delta(counter.TXBytes, before.TXBytes),
			RxPackets: delta(counter.RXPackets, before.RXPackets), TxPackets: delta(counter.TXPackets, before.TXPackets),
			RxErrors: delta(counter.RXErrors, before.RXErrors), TxErrors: delta(counter.TXErrors, before.TXErrors),
			RxDropped: delta(counter.RXDropped, before.RXDropped), TxDropped: delta(counter.TXDropped, before.TXDropped),
		})
	}
	raw, err := json.Marshal(current)
	if err != nil {
		return nil, nil, err
	}
	if previous.SampledAt.IsZero() || previous.NamespaceID == "" || previous.NamespaceID != current.NamespaceID {
		return raw, nil, nil // establish a baseline; do not bill pre-agent traffic
	}
	batch := &nodev1.InterfaceTrafficBatch{
		BatchId: newBatchID(), IntervalStart: timestamppb.New(previous.SampledAt), IntervalEnd: timestamppb.New(now), Interfaces: deltas,
	}
	return raw, &nodev1.NodeEvent{Payload: &nodev1.NodeEvent_InterfaceTraffic{InterfaceTraffic: batch}}, nil
}

func (c *Collector) collectUsers(ctx context.Context, now time.Time) ([]byte, *nodev1.NodeEvent, error) {
	stats, err := c.users.Snapshot(ctx)
	if err != nil {
		return nil, nil, err
	}
	previous := userCheckpoint{Users: make(map[string]userCounter)}
	if raw, err := c.checkpoints.Checkpoint(userCheckpointKey); err != nil {
		return nil, nil, err
	} else if len(raw) > 0 {
		if err := json.Unmarshal(raw, &previous); err != nil {
			return nil, nil, err
		}
	}
	started := stats.GetStartedAt().AsTime()
	if !previous.CoreStarted.Equal(started) {
		previous.Users = make(map[string]userCounter)
		previous.SampledAt = started
	}
	current := userCheckpoint{SampledAt: now, CoreStarted: started, Users: make(map[string]userCounter, len(previous.Users))}
	for tag, counter := range previous.Users {
		current.Users[tag] = counter
	}
	var deltas []*nodev1.UserTrafficDelta
	for _, user := range stats.GetUsers() {
		counter := userCounter{RXBytes: user.GetRxBytesSinceStart(), TXBytes: user.GetTxBytesSinceStart()}
		current.Users[user.GetTag()] = counter
		before := previous.Users[user.GetTag()]
		rx, tx := delta(counter.RXBytes, before.RXBytes), delta(counter.TXBytes, before.TXBytes)
		if rx != 0 || tx != 0 {
			deltas = append(deltas, &nodev1.UserTrafficDelta{UserTag: user.GetTag(), RxBytes: rx, TxBytes: tx})
		}
	}
	raw, err := json.Marshal(current)
	if err != nil {
		return nil, nil, err
	}
	if len(deltas) == 0 {
		return raw, nil, nil
	}
	batch := &nodev1.UserTrafficBatch{
		BatchId: newBatchID(), IntervalStart: timestamppb.New(previous.SampledAt), IntervalEnd: timestamppb.New(now), Users: deltas,
	}
	return raw, &nodev1.NodeEvent{Payload: &nodev1.NodeEvent_UserTraffic{UserTraffic: batch}}, nil
}

func (s *grpcUserStatsSource) Snapshot(ctx context.Context) (*corev1.RuntimeStats, error) {
	conn, err := grpc.NewClient(s.address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return corev1.NewControlServiceClient(conn).GetStats(callCtx, &emptypb.Empty{})
}

func readInterface(dir string) (interfaceCounter, error) {
	read := func(name string) (uint64, error) {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return 0, err
		}
		return strconv.ParseUint(string(bytesTrimSpace(raw)), 10, 64)
	}
	values := make([]uint64, 8)
	for index, name := range []string{"rx_bytes", "tx_bytes", "rx_packets", "tx_packets", "rx_errors", "tx_errors", "rx_dropped", "tx_dropped"} {
		value, err := read(name)
		if err != nil {
			return interfaceCounter{}, err
		}
		values[index] = value
	}
	return interfaceCounter{
		RXBytes: values[0], TXBytes: values[1], RXPackets: values[2], TXPackets: values[3],
		RXErrors: values[4], TXErrors: values[5], RXDropped: values[6], TXDropped: values[7],
	}, nil
}

func delta(current, previous uint64) uint64 {
	if current < previous {
		return current
	}
	return current - previous
}

func newBatchID() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(raw)
}

func bytesTrimSpace(raw []byte) []byte {
	start, end := 0, len(raw)
	for start < end && (raw[start] == ' ' || raw[start] == '\n' || raw[start] == '\r' || raw[start] == '\t') {
		start++
	}
	for end > start && (raw[end-1] == ' ' || raw[end-1] == '\n' || raw[end-1] == '\r' || raw[end-1] == '\t') {
		end--
	}
	return raw[start:end]
}
