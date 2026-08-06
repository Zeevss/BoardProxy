package mgmt

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebAuthRevocableTokenValidator(t *testing.T) {
	active := true
	h := WebAuth(WebAuthConfig{TokenValidator: func(_ context.Context, token string) bool {
		return active && token == "bpa_valid"
	}}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	req.Header.Set("Authorization", "Bearer bpa_valid")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("active key status = %d", w.Code)
	}

	active = false
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("revoked key status = %d", w.Code)
	}
}

func TestWebAuthRateLimitsLogin(t *testing.T) {
	h := WebAuth(WebAuthConfig{UIPassword: "secret"}, http.NotFoundHandler())
	for i := 0; i < loginBurst; i++ {
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"password":"wrong"}`))
		req.RemoteAddr = "192.0.2.1:1234"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want 401", i+1, w.Code)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"password":"wrong"}`))
	req.RemoteAddr = "192.0.2.1:5678"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("overflow status = %d, want 429", w.Code)
	}
}

func TestLoginBodyIsBounded(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/login",
		strings.NewReader(`{"password":"`+strings.Repeat("x", maxLoginBody)+`"}`))
	w := httptest.NewRecorder()
	handleLogin(w, req, "secret", []byte("key"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHTTPSLoginSetsSecureCookie(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "https://panel.example/login", strings.NewReader(`{"password":"secret"}`))
	w := httptest.NewRecorder()
	handleLogin(w, req, "secret", []byte("key"))
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	result := w.Result()
	if cookies := result.Cookies(); len(cookies) != 1 || !cookies[0].Secure {
		t.Fatalf("cookies = %+v, want one Secure cookie", cookies)
	}
}
