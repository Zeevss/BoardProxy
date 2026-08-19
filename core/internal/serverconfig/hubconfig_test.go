package serverconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The hub compiles this TOML in Kotlin; core parses it in Go. Two independent
// implementations of the same format is risk R3 of the control-plane rewrite:
// a divergence would otherwise surface only in production.
//
// The fixture is shared. The Kotlin compiler test asserts it produces these
// exact bytes; this test asserts core accepts them and reads back what the hub
// meant. Break either side and one of the two tests fails.
const hubConfigFixture = "../../../control-plane/contracts/testdata/hub-config.toml"

func TestHubCompiledConfigIsAcceptedByCore(t *testing.T) {
	path, err := filepath.Abs(hubConfigFixture)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read hub fixture: %v", err)
	}

	config, err := Decode(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("core rejected hub-compiled config: %v", err)
	}

	// Durations are the sharpest edge: the hub reimplements Go's
	// time.Duration.String() by hand to emit them.
	if time.Duration(config.Server.IdleTimeout) != 90*time.Second {
		t.Fatalf("idle_timeout=%v, want 1m30s", config.Server.IdleTimeout)
	}
	if time.Duration(config.Transport.AckTimeout) != 6*time.Second {
		t.Fatalf("ack_timeout=%v, want 6s", config.Transport.AckTimeout)
	}
	if config.Transport.StreamIdleTimeout != 0 {
		t.Fatalf("stream_idle_timeout=%v, want 0s", config.Transport.StreamIdleTimeout)
	}

	if len(config.Boards) != 2 {
		t.Fatalf("boards=%d, want 2", len(config.Boards))
	}
	if len(config.Users) != 2 {
		t.Fatalf("users=%d, want 2", len(config.Users))
	}
	if config.Boards[0].Tag != "alpha" || config.Boards[1].Tag != "zeta" {
		t.Fatalf("boards are not in the order the hub emitted: %v", config.Boards)
	}
	if got := config.Users[0]; got.Tag != "alice" || len(got.Boards) != 2 {
		t.Fatalf("user alice did not survive the round trip: %+v", got)
	}
}
