package hub

import (
	"io"
	"testing"
)

// TestUserConnectionsReportsLiveSession проверяет, что UserConnections видит
// живое соединение клиента: страницу, открытый стрим с целью и ненулевой
// трафик после обмена данными.
func TestUserConnectionsReportsLiveSession(t *testing.T) {
	h := newTestHub(t, []string{"p1", "p2"})
	serveEcho(h.srv)

	m, id := h.dialProvisioned(t)
	defer m.Close()

	st, err := m.OpenStream("example.com:443")
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	payload := []byte("hello-connections")
	if _, err := st.Write(payload); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := st.CloseWrite(); err != nil {
		t.Fatalf("CloseWrite: %v", err)
	}
	echoed, err := io.ReadAll(st)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(echoed) != string(payload) {
		t.Fatalf("echo = %q, хочу %q", echoed, payload)
	}

	var conns []ConnectionInfo
	if err := waitCond(func() bool {
		conns = h.srv.UserConnections(id)
		return len(conns) == 1 && conns[0].Written > 0 && conns[0].Received > 0
	}); err != nil {
		t.Fatalf("UserConnections не отразил трафик: %v (conns=%+v)", err, conns)
	}
	if conns[0].Page == "" {
		t.Fatal("ConnectionInfo.Page пуст")
	}
	if conns[0].BundleID == "" || conns[0].LaneID != 1 || conns[0].Epoch != 1 {
		t.Fatalf("ConnectionInfo не содержит v3 bundle identity: %+v", conns[0])
	}

	// Неизвестный пользователь — пустой список, не ошибка.
	if got := h.srv.UserConnections("missing"); len(got) != 0 {
		t.Fatalf("UserConnections(неизвестный) = %+v, хочу пусто", got)
	}
}

// TestSessionCloseAddsUserTraffic проверяет, что при закрытии клиентской
// сессии hub персистит её финальный трафик через UserStore.AddUserTraffic —
// и что после закрытия UserConnections для него снова пуст.
func TestSessionCloseAddsUserTraffic(t *testing.T) {
	h := newTestHub(t, []string{"p1", "p2"})
	serveEcho(h.srv)

	m, id := h.dialProvisioned(t)

	st, err := m.OpenStream("target:80")
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	payload := []byte("some-bytes-to-count")
	if _, err := st.Write(payload); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := st.CloseWrite(); err != nil {
		t.Fatalf("CloseWrite: %v", err)
	}
	if _, err := io.ReadAll(st); err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	// Close всегда возвращает mux.ErrClosed как сигнальное значение (не признак
	// сбоя) — по этому же коду весь остальной проект просто игнорирует его.
	_ = m.Close()

	if err := waitCond(func() bool {
		rx, tx := h.users.trafficOf(id)
		return rx >= uint64(len(payload)) && tx >= uint64(len(payload))
	}); err != nil {
		rx, tx := h.users.trafficOf(id)
		t.Fatalf("трафик не сохранился после закрытия сессии: %v (rx=%d tx=%d)", err, rx, tx)
	}

	if err := waitCond(func() bool { return len(h.srv.UserConnections(id)) == 0 }); err != nil {
		t.Fatal("UserConnections должен опустеть после закрытия сессии")
	}
}
