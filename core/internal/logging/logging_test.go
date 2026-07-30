package logging

import (
	"log/slog"
	"testing"
	"time"
)

// TestBufferKeepsLastEntries проверяет кольцевую перезапись: при переполнении
// остаются только последние capacity записей в хронологическом порядке.
func TestBufferKeepsLastEntries(t *testing.T) {
	b := NewBuffer(3)
	for i := 1; i <= 5; i++ {
		b.add(Entry{Time: time.Unix(int64(i), 0), Level: "INFO", Message: string(rune('0' + i))})
	}
	got := b.Entries(0)
	if len(got) != 3 {
		t.Fatalf("len = %d, хочу 3", len(got))
	}
	want := []string{"3", "4", "5"}
	for i, w := range want {
		if got[i].Message != w {
			t.Fatalf("entries[%d] = %q, хочу %q", i, got[i].Message, w)
		}
	}
}

// TestBufferLimit ограничивает выдачу последними limit записями.
func TestBufferLimit(t *testing.T) {
	b := NewBuffer(10)
	for i := 0; i < 5; i++ {
		b.add(Entry{Message: string(rune('a' + i))})
	}
	got := b.Entries(2)
	if len(got) != 2 || got[0].Message != "d" || got[1].Message != "e" {
		t.Fatalf("limit = %+v", got)
	}
}

// TestNewWithBufferCaptures проверяет, что логгер из NewWithBuffer кладёт записи
// (вместе с атрибутами) в буфер.
func TestNewWithBufferCaptures(t *testing.T) {
	log, buf := NewWithBuffer("debug")
	log.Info("server ready", "board", "abc", "pages", 7)

	entries := buf.Entries(0)
	if len(entries) != 1 {
		t.Fatalf("len = %d, хочу 1", len(entries))
	}
	msg := entries[0].Message
	if msg != "server ready board=abc pages=7" {
		t.Fatalf("message = %q", msg)
	}
	if entries[0].Level != slog.LevelInfo.String() {
		t.Fatalf("level = %q", entries[0].Level)
	}
}

// TestTeeRespectsLevel — записи ниже уровня не попадают в буфер.
func TestTeeRespectsLevel(t *testing.T) {
	log, buf := NewWithBuffer("warn")
	log.Info("filtered out")
	log.Warn("kept")
	entries := buf.Entries(0)
	if len(entries) != 1 || entries[0].Message != "kept" {
		t.Fatalf("entries = %+v", entries)
	}
}
