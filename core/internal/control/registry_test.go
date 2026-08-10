package control

import (
	"context"
	"encoding/base64"
	"testing"

	"bproxy-core/internal/crypto"
	"bproxy-core/internal/serverconfig"
)

func testUser(t *testing.T, max int) (serverconfig.User, []byte) {
	t.Helper()
	kp, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	return serverconfig.User{
		Tag: "alice", Name: "Alice",
		PrivateKey: "base64:" + base64.StdEncoding.EncodeToString(kp.Private()),
		Boards:     []string{"main"}, MaxSessions: max, MaxLanes: 4,
	}, kp.Public()
}

func TestRegistryAuthorizationAndGlobalLimit(t *testing.T) {
	u, public := testUser(t, 1)
	r, err := NewRegistry([]serverconfig.User{u})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Authorize(context.Background(), public, "main"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Authorize(context.Background(), public, "other"); err == nil {
		t.Fatal("unexpected authorization on another board")
	}
	if !r.AcquireSession("alice") || r.AcquireSession("alice") {
		t.Fatal("session limit was not enforced")
	}
	r.ReleaseSession("alice", 10, 20)
	if !r.AcquireSession("alice") {
		t.Fatal("released session was not reusable")
	}
}

func TestRegistryDisableIsImmediate(t *testing.T) {
	u, public := testUser(t, 2)
	r, err := NewRegistry([]serverconfig.User{u})
	if err != nil {
		t.Fatal(err)
	}
	disabled := false
	u.Enabled = &disabled
	if err := r.Replace([]serverconfig.User{u}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Authorize(context.Background(), public, "main"); err == nil {
		t.Fatal("disabled user remained authorized")
	}
}

func TestRegistryTotalsSurvivePolicyRemoval(t *testing.T) {
	u, _ := testUser(t, 1)
	r, err := NewRegistry([]serverconfig.User{u})
	if err != nil {
		t.Fatal(err)
	}
	if !r.AcquireSession("alice") {
		t.Fatal("session was not acquired")
	}
	r.ReleaseSession("alice", 10, 20)
	if err := r.Replace(nil); err != nil {
		t.Fatal(err)
	}
	rx, tx := r.Totals()
	if rx != 10 || tx != 20 {
		t.Fatalf("totals after removal = %d/%d", rx, tx)
	}
}
