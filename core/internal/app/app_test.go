package app

import "testing"

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
