package sqlite

import (
	"context"
	"crypto/sha256"
	"errors"
	"path/filepath"
	"testing"

	"bproxy-core/internal/store"
)

func TestAccessKeysCanBeIssuedValidatedAndRevoked(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	digest := sha256.Sum256([]byte("bpa_test-secret"))
	created, err := s.CreateAccessKey(ctx, "panel", "bpa_test", digest[:])
	if err != nil {
		t.Fatalf("CreateAccessKey: %v", err)
	}
	valid, err := s.AccessKeyValid(ctx, digest[:])
	if err != nil || !valid {
		t.Fatalf("AccessKeyValid = %v, %v", valid, err)
	}
	keys, err := s.ListAccessKeys(ctx)
	if err != nil || len(keys) != 1 || keys[0].Name != "panel" || keys[0].Prefix != "bpa_test" {
		t.Fatalf("ListAccessKeys = %+v, %v", keys, err)
	}
	if err := s.RevokeAccessKey(ctx, created.ID); err != nil {
		t.Fatalf("RevokeAccessKey: %v", err)
	}
	valid, err = s.AccessKeyValid(ctx, digest[:])
	if err != nil || valid {
		t.Fatalf("revoked AccessKeyValid = %v, %v", valid, err)
	}
}

// testStore открывает свежую БД во временном каталоге теста — в отличие от
// Postgres, SQLite не требует внешней СУБД, поэтому тесты идут всегда.
func testStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bproxy.db")
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOpenCreatesFileAndAppliesSchemaIdempotently(t *testing.T) {
	ctx := context.Background()
	// Каталог ещё не существует — Open обязан создать и путь, и файл.
	path := filepath.Join(t.TempDir(), "nested", "dir", "bproxy.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open(новый путь): %v", err)
	}
	if _, err := s.CreateUser(ctx, []byte("k"), "alice"); err != nil {
		t.Fatalf("CreateUser в свежей БД: %v", err)
	}
	s.Close()

	// Повторное открытие того же файла: схема применяется идемпотентно,
	// данные на месте.
	s2, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open(существующий файл): %v", err)
	}
	defer s2.Close()
	users, err := s2.ListUsers(ctx)
	if err != nil || len(users) != 1 {
		t.Fatalf("ListUsers после переоткрытия = %+v err=%v", users, err)
	}
}

func TestUsers(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	pub := []byte("user-pubkey-1")
	created, err := s.CreateUser(ctx, pub, "alice")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("CreateUser: ожидался ненулевой ID")
	}
	if created.Status != store.UserActive {
		t.Fatalf("CreateUser: status = %q, хочу %q", created.Status, store.UserActive)
	}
	if created.CreatedAt.IsZero() {
		t.Fatal("CreateUser: CreatedAt не заполнен")
	}

	got, err := s.UserByPublicKey(ctx, pub)
	if err != nil {
		t.Fatalf("UserByPublicKey: %v", err)
	}
	if got.ID != created.ID || got.Name != "alice" {
		t.Fatalf("UserByPublicKey = %+v, хочу совпадение с %+v", got, created)
	}
	if !got.CreatedAt.Equal(created.CreatedAt) {
		t.Fatalf("CreatedAt после чтения = %v, хочу %v", got.CreatedAt, created.CreatedAt)
	}

	if _, err := s.UserByPublicKey(ctx, []byte("unknown")); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("UserByPublicKey(unknown) = %v, хочу ErrNotFound", err)
	}

	if _, err := s.CreateUser(ctx, pub, "alice-2"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("CreateUser(дубликат public_key) = %v, хочу ErrConflict", err)
	}

	byID, err := s.UserByID(ctx, created.ID)
	if err != nil || byID.Name != "alice" {
		t.Fatalf("UserByID = %+v err=%v", byID, err)
	}
	if _, err := s.UserByID(ctx, 999999); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("UserByID(unknown) = %v, хочу ErrNotFound", err)
	}
	users, err := s.ListUsers(ctx)
	if err != nil || len(users) != 1 {
		t.Fatalf("ListUsers = %+v err=%v", users, err)
	}
	if err := s.SetUserStatus(ctx, created.ID, store.UserDisabled); err != nil {
		t.Fatalf("SetUserStatus: %v", err)
	}
	if u, _ := s.UserByID(ctx, created.ID); u.Status != store.UserDisabled {
		t.Fatalf("статус после SetUserStatus = %q", u.Status)
	}
	if err := s.SetUserStatus(ctx, 999999, store.UserActive); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("SetUserStatus(unknown) = %v, хочу ErrNotFound", err)
	}
}

func TestHubs(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	h, err := s.UpsertHub(ctx, "board-1", "My Board", "slide-hub")
	if err != nil {
		t.Fatalf("UpsertHub(create): %v", err)
	}
	if h.Status != store.HubActive {
		t.Fatalf("UpsertHub: status = %q, хочу %q", h.Status, store.HubActive)
	}
	if h.MaxLanes != 8 {
		t.Fatalf("UpsertHub: max lanes = %d, хочу 8", h.MaxLanes)
	}
	if err := s.SetHubMaxLanes(ctx, "board-1", 8); err != nil {
		t.Fatalf("SetHubMaxLanes: %v", err)
	}
	if h.CreatedAt.IsZero() {
		t.Fatal("UpsertHub: CreatedAt не заполнен")
	}

	h2, err := s.UpsertHub(ctx, "board-1", "My Board Renamed", "slide-hub-2")
	if err != nil {
		t.Fatalf("UpsertHub(update): %v", err)
	}
	if h2.Name != "My Board Renamed" || h2.HubSlide != "slide-hub-2" {
		t.Fatalf("UpsertHub(update) = %+v, поля не обновились", h2)
	}
	if h2.MaxLanes != 8 {
		t.Fatalf("UpsertHub(update) сбросил max lanes: %d", h2.MaxLanes)
	}
	if !h2.CreatedAt.Equal(h.CreatedAt) {
		t.Fatalf("UpsertHub(update): CreatedAt изменился (%v -> %v), хотя не должен", h.CreatedAt, h2.CreatedAt)
	}

	hubs, err := s.ListHubs(ctx)
	if err != nil || len(hubs) != 1 {
		t.Fatalf("ListHubs = %+v err=%v", hubs, err)
	}
	if err := s.SetHubStatus(ctx, "board-1", store.HubDisabled); err != nil {
		t.Fatalf("SetHubStatus: %v", err)
	}
	if hubs, _ := s.ListHubs(ctx); hubs[0].Status != store.HubDisabled {
		t.Fatalf("статус после SetHubStatus = %q", hubs[0].Status)
	}
	if err := s.SetHubStatus(ctx, "nope", store.HubActive); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("SetHubStatus(unknown) = %v, хочу ErrNotFound", err)
	}
}

// TestBackupProducesReadableSnapshot проверяет, что Backup делает цельный файл-
// снимок, который открывается как самостоятельная БД с теми же пользователями.
func TestBackupProducesReadableSnapshot(t *testing.T) {
	ctx := context.Background()
	src := testStore(t)
	if _, err := src.CreateUser(ctx, []byte("key-a"), "alice"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := src.CreateUser(ctx, []byte("key-b"), "bob"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	dst := filepath.Join(t.TempDir(), "snapshot.db")
	if err := src.Backup(ctx, dst); err != nil {
		t.Fatalf("Backup: %v", err)
	}

	// Снимок открывается как отдельная БД и содержит тех же пользователей.
	snap, err := Open(ctx, dst)
	if err != nil {
		t.Fatalf("Open(snapshot): %v", err)
	}
	defer snap.Close()
	users, err := snap.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers(snapshot): %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("в снимке %d пользователей, хочу 2", len(users))
	}
}

// TestTouchUserSetsLastSeen проверяет, что TouchUser проставляет last_seen.
func TestTouchUserSetsLastSeen(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	u, err := s.CreateUser(ctx, []byte("k"), "alice")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if !u.LastSeen.IsZero() {
		t.Fatalf("свежий пользователь: last_seen должен быть нулевым, got %v", u.LastSeen)
	}
	if err := s.TouchUser(ctx, u.ID); err != nil {
		t.Fatalf("TouchUser: %v", err)
	}
	got, err := s.UserByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if got.LastSeen.IsZero() {
		t.Fatal("TouchUser не проставил last_seen")
	}
	if err := s.TouchUser(ctx, 9999); err == nil {
		t.Fatal("TouchUser(неизвестный) должен вернуть ошибку")
	}
}

// TestDeleteUserRemovesRow проверяет жёсткое удаление пользователя.
func TestDeleteUserRemovesRow(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	u, err := s.CreateUser(ctx, []byte("k"), "bob")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.DeleteUser(ctx, u.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, err := s.UserByID(ctx, u.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("после удаления UserByID = %v, хочу ErrNotFound", err)
	}
	if err := s.DeleteUser(ctx, u.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("повторное удаление = %v, хочу ErrNotFound", err)
	}
}

// TestDeleteHubRemovesRow проверяет жёсткое удаление хаба.
func TestDeleteHubRemovesRow(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	if _, err := s.UpsertHub(ctx, "board-x", "X", "slide"); err != nil {
		t.Fatalf("UpsertHub: %v", err)
	}
	if err := s.DeleteHub(ctx, "board-x"); err != nil {
		t.Fatalf("DeleteHub: %v", err)
	}
	hubs, err := s.ListHubs(ctx)
	if err != nil {
		t.Fatalf("ListHubs: %v", err)
	}
	if len(hubs) != 0 {
		t.Fatalf("после удаления хабов %d, хочу 0", len(hubs))
	}
	if err := s.DeleteHub(ctx, "board-x"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("повторное удаление = %v, хочу ErrNotFound", err)
	}
}
