package mobile

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"bproxy-core/pkg/bproxy"
)

func newBackgroundContext() context.Context { return context.Background() }

type metricStream struct {
	ID        int64  `json:"id"`
	Target    string `json:"target"`
	Tx        int64  `json:"tx"`
	Rx        int64  `json:"rx"`
	StartedMS int64  `json:"started_ms"`
}

type metricSnapshot struct {
	Status          string         `json:"status"`
	RTTMS           int64          `json:"rtt_ms"`
	Streams         int            `json:"streams"`
	Datagrams       int            `json:"datagrams"`
	TotalTx         int64          `json:"total_tx"`
	TotalRx         int64          `json:"total_rx"`
	RateTx          int64          `json:"rate_tx"`
	RateRx          int64          `json:"rate_rx"`
	TransportAcked  int64          `json:"transport_acked_bytes"`
	RateConfirmedTx int64          `json:"rate_confirmed_tx"`
	BacklogFrames   int            `json:"backlog_frames"`
	BacklogBytes    int            `json:"backlog_bytes"`
	BlockedWriters  int            `json:"blocked_writers"`
	Details         []metricStream `json:"details"`
}

func metricsDTO(m bproxy.Metrics) metricSnapshot {
	out := metricSnapshot{
		Status:          string(m.Status),
		RTTMS:           m.RTT.Milliseconds(),
		Streams:         m.Streams,
		Datagrams:       m.Datagrams,
		TotalTx:         int64(m.TotalTx),
		TotalRx:         int64(m.TotalRx),
		RateTx:          int64(m.RateTx),
		RateRx:          int64(m.RateRx),
		TransportAcked:  int64(m.TransportAcked),
		RateConfirmedTx: int64(m.RateConfirmedTx),
		BacklogFrames:   m.BacklogFrames,
		BacklogBytes:    m.BacklogBytes,
		BlockedWriters:  m.BlockedWriters,
		Details:         make([]metricStream, 0, len(m.Details)),
	}
	for _, stream := range m.Details {
		out.Details = append(out.Details, metricStream{
			ID:        int64(stream.ID),
			Target:    stream.Target,
			Tx:        int64(stream.Tx),
			Rx:        int64(stream.Rx),
			StartedMS: stream.StartedAt.UnixMilli(),
		})
	}
	return out
}

// callbackHandler keeps slog-specific types behind the Go boundary and emits a
// compact string to the Java Listener.
type callbackHandler struct {
	listener Listener
	minLevel slog.Level
	attrs    []slog.Attr
	group    string
}

func (h *callbackHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.minLevel
}

func (h *callbackHandler) Handle(_ context.Context, record slog.Record) error {
	if h.listener == nil {
		return nil
	}
	parts := []string{record.Message}
	appendAttr := func(attr slog.Attr) {
		if attr.Equal(slog.Attr{}) {
			return
		}
		key := attr.Key
		if h.group != "" {
			key = h.group + "." + key
		}
		parts = append(parts, fmt.Sprintf("%s=%v", key, attr.Value.Any()))
	}
	for _, attr := range h.attrs {
		appendAttr(attr)
	}
	record.Attrs(func(attr slog.Attr) bool {
		appendAttr(attr)
		return true
	})
	h.listener.OnLog(strings.ToLower(record.Level.String()), strings.Join(parts, " "))
	return nil
}

func (h *callbackHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := *h
	clone.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return &clone
}

func (h *callbackHandler) WithGroup(name string) slog.Handler {
	clone := *h
	if clone.group == "" {
		clone.group = name
	} else {
		clone.group += "." + name
	}
	return &clone
}

func parseLogLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
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
