package hub

import (
	"context"
	"testing"
	"time"

	"bproxy-core/internal/crypto"
	"bproxy-core/internal/mux"
)

// TestDisconnectUserClosesLiveSessions проверяет, что DisconnectUser закрывает
// живые сессии конкретного пользователя (клиентская mux-сессия завершается,
// получив GOAWAY), а чужие сессии не трогает.
func TestDisconnectUserClosesLiveSessions(t *testing.T) {
	h := newTestHub(t, []string{"p1", "p2"})
	serveEcho(h.srv)

	c1, id1 := h.dialProvisioned(t)
	defer c1.Close()
	c2, id2 := h.dialProvisioned(t)
	defer c2.Close()

	if err := waitCond(func() bool {
		return h.srv.userSessionCount(id1) == 1 && h.srv.userSessionCount(id2) == 1
	}); err != nil {
		t.Fatalf("сессии не зарегистрировались в byUser: %v", err)
	}

	// Отключаем первого — его сессия должна закрыться, второго не трогаем.
	if n := h.srv.DisconnectUser(context.Background(), id1); n != 1 {
		t.Fatalf("DisconnectUser вернул %d, хочу 1", n)
	}
	select {
	case <-c1.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("сессия отключённого клиента не завершилась")
	}
	select {
	case <-c2.Done():
		t.Fatal("сессия постороннего клиента ошибочно завершилась")
	case <-time.After(300 * time.Millisecond):
	}
}

func (s *Server) userSessionCount(userID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.byUser[userID])
}

func waitCond(cond func() bool) error {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return context.DeadlineExceeded
}

// dialProvisioned подключает клиента и возвращает сессию вместе с его user_id.
func (h *testHub) dialProvisioned(t *testing.T) (*mux.Session, string) {
	t.Helper()
	client, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	h.users.provision(client.Public())
	id := h.users.idOf(client.Public())
	m, err := h.dialWith(t, client)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return m, id
}
