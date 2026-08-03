// Package netstats exposes raw kernel network counters. By default it reads the
// core process network namespace; Docker Compose can instead mount one bridge's
// read-only sysfs statistics directory. No Docker socket is needed.
package netstats

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultSampleInterval = 2 * time.Second

// Snapshot is a point-in-time view of raw interface counters and their recent
// rates. Counters are kernel interface counters, not BoardProxy payload bytes.
type Snapshot struct {
	Available         bool
	Scope             string
	Interfaces        []string
	StartedAt         time.Time
	SampledAt         time.Time
	RXBytes           uint64
	TXBytes           uint64
	RXBytesSinceStart uint64
	TXBytesSinceStart uint64
	RXBytesPerSecond  float64
	TXBytesPerSecond  float64
}

type counters struct {
	rx         uint64
	tx         uint64
	interfaces []string
	scope      string
}

// Monitor samples the default-route interfaces in the background. Snapshot is
// cheap and safe for concurrent API and log consumers.
type Monitor struct {
	mu       sync.RWMutex
	started  time.Time
	baseline counters
	last     counters
	lastAt   time.Time
	snapshot Snapshot
}

// Start creates a monitor tied to ctx. A failed read leaves Available=false;
// later samples keep retrying so a transient /proc failure is recoverable.
func Start(ctx context.Context) *Monitor {
	now := time.Now()
	m := &Monitor{started: now, lastAt: now}
	if c, err := readCounters(); err == nil {
		m.baseline = c
		m.last = c
		m.snapshot = buildSnapshot(now, now, c, c, c, 0)
	}
	go m.run(ctx, defaultSampleInterval)
	return m
}

func (m *Monitor) run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			m.sample(now)
		}
	}
}

func (m *Monitor) sample(now time.Time) {
	c, err := readCounters()
	if err != nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.snapshot.Available {
		m.baseline = c
		m.last = c
		m.lastAt = now
		m.snapshot = buildSnapshot(m.started, now, c, c, c, 0)
		return
	}
	elapsed := now.Sub(m.lastAt).Seconds()
	m.snapshot = buildSnapshot(m.started, now, m.baseline, m.last, c, elapsed)
	m.last = c
	m.lastAt = now
}

// Snapshot returns a detached copy of the latest sample.
func (m *Monitor) Snapshot() Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := m.snapshot
	out.Interfaces = append([]string(nil), out.Interfaces...)
	return out
}

func buildSnapshot(started, now time.Time, baseline, previous, current counters, elapsed float64) Snapshot {
	s := Snapshot{
		Available:  true,
		Scope:      current.scope,
		Interfaces: append([]string(nil), current.interfaces...),
		StartedAt:  started,
		SampledAt:  now,
		RXBytes:    current.rx,
		TXBytes:    current.tx,
	}
	if current.rx >= baseline.rx {
		s.RXBytesSinceStart = current.rx - baseline.rx
	}
	if current.tx >= baseline.tx {
		s.TXBytesSinceStart = current.tx - baseline.tx
	}
	if elapsed > 0 {
		if current.rx >= previous.rx {
			s.RXBytesPerSecond = float64(current.rx-previous.rx) / elapsed
		}
		if current.tx >= previous.tx {
			s.TXBytesPerSecond = float64(current.tx-previous.tx) / elapsed
		}
	}
	return s
}

// readCounters reads /proc/net/dev and selects interfaces carrying a default
// IPv4 route. This avoids double-counting docker/veth/tunnel interfaces on a
// host. If no default route is visible, all non-loopback interfaces are used.
func readCounters() (counters, error) {
	if dir := os.Getenv("BPROXY_NETWORK_STATS_DIR"); dir != "" {
		return readSysfsCounters(dir, os.Getenv("BPROXY_NETWORK_INTERFACE"))
	}
	raw, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return counters{}, err
	}
	all, err := parseNetDev(string(raw))
	if err != nil {
		return counters{}, err
	}
	preferred := defaultRouteInterfaces()
	var names []string
	for name := range all {
		if name == "lo" {
			continue
		}
		if len(preferred) == 0 || preferred[name] {
			names = append(names, name)
		}
	}
	if len(names) == 0 && len(preferred) > 0 {
		for name := range all {
			if name != "lo" {
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)
	var out counters
	out.interfaces = names
	out.scope = "process_network_namespace"
	for _, name := range names {
		out.rx += all[name][0]
		out.tx += all[name][1]
	}
	return out, nil
}

func readSysfsCounters(dir, name string) (counters, error) {
	rx, err := readCounterFile(filepath.Join(dir, "rx_bytes"))
	if err != nil {
		return counters{}, fmt.Errorf("read bridge rx counter: %w", err)
	}
	tx, err := readCounterFile(filepath.Join(dir, "tx_bytes"))
	if err != nil {
		return counters{}, fmt.Errorf("read bridge tx counter: %w", err)
	}
	if name == "" {
		name = filepath.Base(filepath.Dir(dir))
	}
	return counters{rx: rx, tx: tx, interfaces: []string{name}, scope: "host_bridge"}, nil
}

func readCounterFile(path string) (uint64, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
}

func parseNetDev(raw string) (map[string][2]uint64, error) {
	out := make(map[string][2]uint64)
	s := bufio.NewScanner(strings.NewReader(raw))
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		name := strings.TrimSpace(line[:colon])
		fields := strings.Fields(line[colon+1:])
		if name == "" || len(fields) < 9 {
			continue
		}
		rx, errRX := strconv.ParseUint(fields[0], 10, 64)
		tx, errTX := strconv.ParseUint(fields[8], 10, 64)
		if errRX != nil || errTX != nil {
			return nil, fmt.Errorf("parse /proc/net/dev interface %q", name)
		}
		out[name] = [2]uint64{rx, tx}
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("/proc/net/dev contains no interfaces")
	}
	return out, nil
}

func defaultRouteInterfaces() map[string]bool {
	raw, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return nil
	}
	out := make(map[string]bool)
	s := bufio.NewScanner(strings.NewReader(string(raw)))
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) < 4 || fields[0] == "Iface" || fields[1] != "00000000" {
			continue
		}
		flags, err := strconv.ParseUint(fields[3], 16, 64)
		if err == nil && flags&1 != 0 { // RTF_UP
			out[fields[0]] = true
		}
	}
	return out
}
