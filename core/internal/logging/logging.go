// Package logging provides the shared structured logger for BoardProxy.
//
// It is a thin wrapper over log/slog so every binary and layer logs in one
// consistent format and level, configured once at startup.
package logging

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

// New returns a slog.Logger writing to stderr at the given level.
// Recognised levels: "debug", "info", "warn", "error" (default "info").
func New(level string) *slog.Logger {
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: parseLevel(level)})
	return slog.New(h)
}

// NewWithBuffer работает как New, но дополнительно копит записи в кольцевой
// буфер в памяти — чтобы управляющий API (mgmt) мог отдавать последние строки
// лога в веб-панель, не читая stderr/файлы. stderr остаётся основным приёмником;
// буфер — только «хвост» для просмотра.
func NewWithBuffer(level string) (*slog.Logger, *Buffer) {
	buf := NewBuffer(defaultBufferCap)
	text := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: parseLevel(level)})
	return slog.New(&teeHandler{inner: text, buf: buf}), buf
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// defaultBufferCap — сколько последних записей держим в кольцевом буфере.
const defaultBufferCap = 1000

// Entry — одна запись лога в удобном для API виде.
type Entry struct {
	Time    time.Time `json:"ts"`
	Level   string    `json:"level"`
	Message string    `json:"msg"`
}

// Buffer — потокобезопасный кольцевой буфер последних записей лога. При
// переполнении затирает самые старые. Ноль-значение непригодно — используйте
// NewBuffer.
type Buffer struct {
	mu      sync.Mutex
	entries []Entry
	next    int // индекс следующей записи (по кольцу)
	full    bool
}

// NewBuffer создаёт буфер на cap записей (cap<=0 трактуется как 1).
func NewBuffer(capacity int) *Buffer {
	if capacity <= 0 {
		capacity = 1
	}
	return &Buffer{entries: make([]Entry, capacity)}
}

// add кладёт запись в кольцо.
func (b *Buffer) add(e Entry) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.entries[b.next] = e
	b.next++
	if b.next == len(b.entries) {
		b.next = 0
		b.full = true
	}
}

// Entries возвращает записи в хронологическом порядке (от старых к новым). Если
// limit>0, отдаёт только последние limit записей.
func (b *Buffer) Entries(limit int) []Entry {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := b.next
	if b.full {
		n = len(b.entries)
	}
	out := make([]Entry, 0, n)
	if b.full {
		// Начинаем с самой старой (b.next) и идём по кольцу.
		for i := 0; i < len(b.entries); i++ {
			out = append(out, b.entries[(b.next+i)%len(b.entries)])
		}
	} else {
		out = append(out, b.entries[:b.next]...)
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

// teeHandler — slog.Handler, который отдаёт запись во внутренний хендлер
// (stderr) и параллельно складывает её в кольцевой буфер. Атрибуты, добавленные
// через WithAttrs/WithGroup, склеиваются в текст сообщения для буфера — панели
// нужен читаемый «хвост», а не структурированный разбор.
type teeHandler struct {
	inner slog.Handler
	buf   *Buffer
	attrs []slog.Attr
}

func (h *teeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *teeHandler) Handle(ctx context.Context, r slog.Record) error {
	h.buf.add(Entry{
		Time:    r.Time,
		Level:   r.Level.String(),
		Message: formatRecord(r, h.attrs),
	})
	return h.inner.Handle(ctx, r)
}

func (h *teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	merged := make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	merged = append(merged, h.attrs...)
	merged = append(merged, attrs...)
	return &teeHandler{inner: h.inner.WithAttrs(attrs), buf: h.buf, attrs: merged}
}

func (h *teeHandler) WithGroup(name string) slog.Handler {
	return &teeHandler{inner: h.inner.WithGroup(name), buf: h.buf, attrs: h.attrs}
}

// formatRecord склеивает сообщение записи с её атрибутами (и унаследованными от
// WithAttrs) в одну строку "msg key=val key=val".
func formatRecord(r slog.Record, inherited []slog.Attr) string {
	var b strings.Builder
	b.WriteString(r.Message)
	for _, a := range inherited {
		writeAttr(&b, a)
	}
	r.Attrs(func(a slog.Attr) bool {
		writeAttr(&b, a)
		return true
	})
	return b.String()
}

func writeAttr(b *strings.Builder, a slog.Attr) {
	b.WriteByte(' ')
	b.WriteString(a.Key)
	b.WriteByte('=')
	b.WriteString(a.Value.String())
}
