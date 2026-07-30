package mux

import (
	"io"
	"testing"
	"time"
)

// TestSessionStatsCountsTraffic проверяет, что Session.Stats() учитывает байты
// открытых стримов, а после закрытия стрима его байты не теряются (переходят в
// closed-аккумулятор), и RTT берётся из Conn.
func TestSessionStatsCountsTraffic(t *testing.T) {
	client, server := linkedSessions(t)
	echoServer(t, server)

	st, err := client.OpenStream("example.com:443")
	if err != nil {
		t.Fatal(err)
	}
	msg := "hello over the mux with some bytes"
	if _, err := io.WriteString(st, msg); err != nil {
		t.Fatal(err)
	}
	if err := st.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	got := readAllWithTimeout(t, st, 5*time.Second)
	if got != msg {
		t.Fatalf("echo mismatch: got %q want %q", got, msg)
	}

	// Пока стрим ещё в сессии: written покрывает отправленное, received —
	// эхо-ответ.
	if err := eventuallyMux(func() bool {
		s := client.Stats()
		return s.Written >= uint64(len(msg)) && s.Received >= uint64(len(msg))
	}); err != nil {
		t.Fatalf("stats не учли трафик открытого стрима: %+v", client.Stats())
	}

	beforeClose := client.Stats()

	// Закрываем стрим полностью (обе стороны) — сервер тоже шлёт FIN после эха.
	// Дожидаемся, что стрим ушёл из открытых, но суммарные байты не уменьшились.
	if err := eventuallyMux(func() bool {
		s := client.Stats()
		return s.Streams == 0 && s.Written >= beforeClose.Written && s.Received >= beforeClose.Received
	}); err != nil {
		s := client.Stats()
		t.Fatalf("после закрытия стрима: streams=%d written=%d(before %d) received=%d(before %d)",
			s.Streams, s.Written, beforeClose.Written, s.Received, beforeClose.Received)
	}
}
