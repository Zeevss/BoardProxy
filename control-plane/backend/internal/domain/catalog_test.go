package domain

import (
	"crypto/ecdh"
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

func TestNewCatalogAssignsVersionsAndValidatesRelationships(t *testing.T) {
	catalog := testCatalog(t)
	if catalog.Version != 1 || catalog.Node.Version != 1 || catalog.Boards[0].Version != 1 ||
		catalog.Users[0].Version != 1 || catalog.Assignment.Version != 1 {
		t.Fatalf("unexpected versions: %+v", catalog)
	}
	broken := catalog
	broken.Assignment.Users[0].BoardIDs = []string{"missing"}
	if err := broken.Validate(); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("error=%v, want ErrInvalidState", err)
	}
}

func TestReplaceBoardUsesOptimisticResourceVersion(t *testing.T) {
	catalog := testCatalog(t)
	board := catalog.Boards[0]
	board.Name = "Renamed"
	next, err := catalog.ReplaceBoard(board, 1, time.Unix(200, 0))
	if err != nil {
		t.Fatal(err)
	}
	if next.Version != 2 || next.Boards[0].Version != 2 || next.Boards[0].Name != "Renamed" {
		t.Fatalf("unexpected updated catalog: %+v", next)
	}
	if _, err := next.ReplaceBoard(board, 1, time.Unix(300, 0)); !errors.Is(err, ErrConflict) {
		t.Fatalf("error=%v, want ErrConflict", err)
	}
}

func TestCatalogRejectsDuplicateUserIdentity(t *testing.T) {
	catalog := testCatalog(t)
	private, err := decodeKey(catalog.Users[0].PrivateKey, true)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdh.X25519().NewPrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	catalog.Users = append(catalog.Users, User{
		ID: "duplicate", Name: "Duplicate", PublicKey: encodeKey(key.PublicKey().Bytes()),
		State: ResourceEnabled, MaxLanes: 1, Version: 1,
	})
	if err := catalog.Validate(); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("error=%v, want duplicate identity rejection", err)
	}
}

func TestRevokedResourceCannotBeRestored(t *testing.T) {
	catalog := testCatalog(t)
	user := catalog.Users[0]
	user.State = ResourceRevoked
	revoked, err := catalog.ReplaceUser(user, user.Version, time.Unix(200, 0))
	if err != nil {
		t.Fatal(err)
	}
	user = revoked.Users[0]
	user.State = ResourceEnabled
	if _, err := revoked.ReplaceUser(user, user.Version, time.Unix(300, 0)); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("error=%v, want terminal revoked state", err)
	}
}

func testCatalog(t *testing.T) Catalog {
	t.Helper()
	now := time.Unix(100, 0).UTC()
	catalog, err := NewCatalog(
		Node{ID: "node-1", Name: "Node 1", State: ResourceEnabled, Core: DefaultCoreSettings(testPrivateKey(1))},
		[]Board{{ID: "primary", Name: "Primary", Hash: "board-hash", State: ResourceEnabled, MaxLanes: 2}},
		[]User{{ID: "alice", Name: "Alice", PrivateKey: testPrivateKey(2), State: ResourceEnabled, MaxSessions: 2, MaxLanes: 2}},
		NodeAssignment{NodeID: "node-1", BoardIDs: []string{"primary"}, Users: []AssignedUser{{UserID: "alice", BoardIDs: []string{"primary"}}}},
		now,
	)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func testPrivateKey(value byte) string {
	return encodeKey(bytesOf(value))
}

func bytesOf(value byte) []byte {
	raw := make([]byte, 32)
	for index := range raw {
		raw[index] = value
	}
	return raw
}

func encodeKey(raw []byte) string { return "base64:" + base64.StdEncoding.EncodeToString(raw) }
