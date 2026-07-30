package yandex

import "testing"

func TestDropCells(t *testing.T) {
	cells := dropCells([]string{"a", "b", "c"})
	if len(cells) != 3 {
		t.Fatalf("got %d cells, want 3", len(cells))
	}
	for i, id := range []string{"a", "b", "c"} {
		if cells[i].Hash != id || cells[i].Attributes.ID != id {
			t.Fatalf("cell %d = %+v, want id %q", i, cells[i], id)
		}
	}
}

func TestDropCellsEmpty(t *testing.T) {
	if cells := dropCells(nil); len(cells) != 0 {
		t.Fatalf("got %d cells, want 0", len(cells))
	}
}
