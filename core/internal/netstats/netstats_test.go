package netstats

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseNetDev(t *testing.T) {
	raw := `Inter-| Receive | Transmit
 face |bytes packets errs drop fifo frame compressed multicast|bytes packets errs drop fifo colls carrier compressed
    lo: 100 1 0 0 0 0 0 0 100 1 0 0 0 0 0 0
  eth0: 1234 2 0 0 0 0 0 0 5678 3 0 0 0 0 0 0
`
	got, err := parseNetDev(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got["eth0"] != [2]uint64{1234, 5678} {
		t.Fatalf("eth0 = %v", got["eth0"])
	}
}

func TestBuildSnapshotHandlesCounterReset(t *testing.T) {
	base := counters{rx: 100, tx: 200, interfaces: []string{"eth0"}, scope: "test"}
	current := counters{rx: 10, tx: 20, interfaces: []string{"eth0"}, scope: "test"}
	got := buildSnapshot(timeZero, timeZero, base, base, current, 2)
	if got.RXBytesSinceStart != 0 || got.TXBytesSinceStart != 0 || got.RXBytesPerSecond != 0 || got.TXBytesPerSecond != 0 {
		t.Fatalf("counter reset produced deltas: %+v", got)
	}
}

func TestReadSysfsCounters(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rx_bytes"), []byte("123\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tx_bytes"), []byte("456\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readSysfsCounters(dir, "bproxy0")
	if err != nil {
		t.Fatal(err)
	}
	if got.rx != 123 || got.tx != 456 || got.scope != "host_bridge" || got.interfaces[0] != "bproxy0" {
		t.Fatalf("counters = %+v", got)
	}
}

var timeZero = func() (z time.Time) { return }()
