package app

import (
	"context"
	"path/filepath"
	"testing"

	"bproxy-core/internal/store"
	"bproxy-core/internal/store/sqlite"
)

func TestFirstSlideSortedEmpty(t *testing.T) {
	if _, err := firstSlideSorted(nil); err == nil {
		t.Fatal("expected error for empty slide list")
	}
}

func TestFirstSlideSortedSingle(t *testing.T) {
	got, err := firstSlideSorted([]string{"only"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "only" {
		t.Fatalf("got %q, want %q", got, "only")
	}
}

func TestFirstSlideSortedPicksAlphabeticallyFirst(t *testing.T) {
	got, err := firstSlideSorted([]string{"c3", "a1", "b2"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "a1" {
		t.Fatalf("got %q, want %q", got, "a1")
	}
}

func TestFirstSlideSortedDoesNotMutateInput(t *testing.T) {
	in := []string{"c3", "a1", "b2"}
	orig := append([]string(nil), in...)
	if _, err := firstSlideSorted(in); err != nil {
		t.Fatal(err)
	}
	for i := range in {
		if in[i] != orig[i] {
			t.Fatalf("input mutated: got %v, want %v", in, orig)
		}
	}
}

func TestResolveBoards(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	// Активный хаб из БД + отключённый (не должен попасть).
	if _, err := st.UpsertHub(ctx, "db-active", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertHub(ctx, "db-off", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := st.SetHubStatus(ctx, "db-off", store.HubDisabled); err != nil {
		t.Fatal(err)
	}

	// Флаг: одна новая доска, одна дублирующая активный хаб, плюс пробелы/пустые.
	got := resolveBoards(ctx, " flag-new , db-active ,", st)

	want := map[string]bool{"db-active": true, "flag-new": true}
	if len(got) != len(want) {
		t.Fatalf("resolveBoards = %v, хочу 2 без дублей/отключённых", got)
	}
	for _, b := range got {
		if !want[b] {
			t.Fatalf("resolveBoards вернул неожиданную доску %q (%v)", b, got)
		}
	}
	// db-active должна идти первой (из store, раньше флага).
	if got[0] != "db-active" {
		t.Fatalf("порядок: первым ждём db-active, got %v", got)
	}
}

func TestResolveBoardsEmpty(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	if got := resolveBoards(ctx, "", st); len(got) != 0 {
		t.Fatalf("board-less: resolveBoards = %v, хочу пусто", got)
	}
}
