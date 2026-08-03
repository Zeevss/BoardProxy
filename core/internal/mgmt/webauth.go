package mgmt

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// WebAuthConfig конфигурирует аутентификацию web-API (в отличие от unix-сокета,
// где граница доступа — права файловой системы, поэтому там аутентификации нет).
type WebAuthConfig struct {
	// Token, если задан, разрешает доступ по заголовку
	// "Authorization: Bearer <token>" — для скриптов/CLI поверх TCP.
	Token string
	// UIPassword, если задан, включает вход в веб-панель по паролю: POST /login
	// заводит сессионную cookie, которая дальше пускает к API. Пусто — вход по
	// паролю выключен.
	UIPassword string
}

const (
	sessionCookie = "bproxy_session"
	sessionTTL    = 12 * time.Hour
	maxLoginBody  = 4 << 10
	loginWindow   = time.Minute
	loginBurst    = 5
)

type loginAttempt struct {
	window time.Time
	count  int
}

type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string]loginAttempt
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{attempts: make(map[string]loginAttempt)}
}

func (l *loginLimiter) allow(remote string, now time.Time) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	a := l.attempts[host]
	if a.window.IsZero() || now.Sub(a.window) >= loginWindow {
		a = loginAttempt{window: now}
	}
	if a.count >= loginBurst {
		return false
	}
	a.count++
	l.attempts[host] = a
	// Keep the map bounded even behind a public listener receiving spoof-like
	// churn through many real source addresses.
	if len(l.attempts) > 4096 {
		for key, entry := range l.attempts {
			if now.Sub(entry.window) >= loginWindow {
				delete(l.attempts, key)
			}
		}
		for len(l.attempts) > 4096 {
			for key := range l.attempts {
				delete(l.attempts, key)
				break
			}
		}
	}
	return true
}

// WebAuth оборачивает управляющий handler аутентификацией web-API. Монтирует
// открытые POST /login и POST /logout, а на остальные запросы требует валидный
// bearer-токен ЛИБО валидную сессионную cookie. Если ни Token, ни UIPassword не
// заданы, доступ открыт (совместимо с текущим поведением незащищённого
// --web-api на loopback) — предупреждение о небезопасной привязке остаётся на
// вызывающем.
func WebAuth(cfg WebAuthConfig, next http.Handler) http.Handler {
	// HMAC-ключ сессии выводим из пароля (SHA256), чтобы выданные cookie
	// переживали плавный перезапуск сервера (ключ тот же, пока тот же пароль).
	var key []byte
	if cfg.UIPassword != "" {
		sum := sha256.Sum256([]byte("bproxy-session\x00" + cfg.UIPassword))
		key = sum[:]
	}
	open := cfg.Token == "" && cfg.UIPassword == ""
	limiter := newLoginLimiter()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/login":
			if !limiter.allow(r.RemoteAddr, time.Now()) {
				w.Header().Set("Retry-After", strconv.Itoa(int(loginWindow.Seconds())))
				http.Error(w, `{"error":"too many login attempts"}`, http.StatusTooManyRequests)
				return
			}
			handleLogin(w, r, cfg.UIPassword, key)
			return
		case r.Method == http.MethodPost && r.URL.Path == "/logout":
			clearSession(w)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if open || bearerOK(cfg.Token, r) || sessionOK(key, r) {
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
	})
}

// handleLogin проверяет пароль и, при совпадении, ставит сессионную cookie.
// Если вход по паролю выключен (пустой password), отвечает 404 — эндпойнта нет.
func handleLogin(w http.ResponseWriter, r *http.Request, password string, key []byte) {
	if password == "" {
		http.Error(w, `{"error":"password login disabled"}`, http.StatusNotFound)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxLoginBody)
	var req struct {
		Password string `json:"password"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}
	if subtle.ConstantTimeCompare([]byte(req.Password), []byte(password)) != 1 {
		http.Error(w, `{"error":"invalid password"}`, http.StatusUnauthorized)
		return
	}
	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	setSession(w, key, secure)
	w.WriteHeader(http.StatusNoContent)
}

// bearerOK сообщает, что задан непустой токен и он совпал с заголовком (в
// постоянном времени, чтобы не течь по таймингу ответа).
func bearerOK(token string, r *http.Request) bool {
	if token == "" {
		return false
	}
	want := "Bearer " + token
	got := r.Header.Get("Authorization")
	return len(got) == len(want) && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// sessionOK проверяет валидность и срок сессионной cookie.
func sessionOK(key []byte, r *http.Request) bool {
	if key == nil {
		return false
	}
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}
	exp, ok := verifyToken(key, c.Value)
	return ok && time.Now().Before(exp)
}

// setSession ставит подписанную HttpOnly-cookie со сроком sessionTTL.
func setSession(w http.ResponseWriter, key []byte, secure bool) {
	exp := time.Now().Add(sessionTTL)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    signToken(key, exp),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  exp,
		Secure:   secure,
	})
}

// clearSession удаляет сессионную cookie.
func clearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// signToken формирует значение cookie: "<exp>.<hmac(exp)>", где exp — unix-время
// истечения, а подпись — HMAC-SHA256 от него на ключе key.
func signToken(key []byte, exp time.Time) string {
	payload := strconv.FormatInt(exp.Unix(), 10)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payload + "." + sig
}

// verifyToken проверяет подпись cookie и возвращает зашитый срок истечения.
func verifyToken(key []byte, value string) (time.Time, bool) {
	dot := -1
	for i := len(value) - 1; i >= 0; i-- {
		if value[i] == '.' {
			dot = i
			break
		}
	}
	if dot <= 0 || dot == len(value)-1 {
		return time.Time{}, false
	}
	payload, sig := value[:dot], value[dot+1:]
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payload))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(sig), []byte(want)) != 1 {
		return time.Time{}, false
	}
	unix, err := strconv.ParseInt(payload, 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(unix, 0), true
}
