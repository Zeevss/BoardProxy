package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	sessionCookie = "bproxy_panel_session"
	nodeCookie    = "bproxy_panel_node"
	sessionTTL    = 12 * time.Hour
	loginWindow   = time.Minute
	loginLimit    = 10
)

type loginAttempt struct {
	windowStart time.Time
	failures    int
}

var errNodeExists = errors.New("node with this address already exists")

type node struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Host      string    `json:"host"`
	Port      int       `json:"port"`
	TLS       bool      `json:"tls"`
	AccessKey string    `json:"access_key,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type publicNode struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Host      string    `json:"host"`
	Port      int       `json:"port"`
	TLS       bool      `json:"tls"`
	KeyHint   string    `json:"key_hint"`
	CreatedAt time.Time `json:"created_at"`
	Selected  bool      `json:"selected"`
}

type registry struct {
	mu    sync.RWMutex
	path  string
	nodes []node
}

func loadRegistry(path string) (*registry, error) {
	r := &registry{path: path, nodes: []node{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &r.nodes); err != nil {
		return nil, fmt.Errorf("decode node registry: %w", err)
	}
	return r, nil
}

func (r *registry) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(r.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r.nodes, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, r.path)
}

func (r *registry) list() []node {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]node(nil), r.nodes...)
}

func (r *registry) get(id string) (node, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, n := range r.nodes {
		if n.ID == id {
			return n, true
		}
	}
	return node{}, false
}

func (r *registry) add(n node) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.nodes {
		if existing.Host == n.Host && existing.Port == n.Port {
			return errNodeExists
		}
	}
	r.nodes = append(r.nodes, n)
	if err := r.saveLocked(); err != nil {
		r.nodes = r.nodes[:len(r.nodes)-1]
		return err
	}
	return nil
}

func (r *registry) remove(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.nodes {
		if r.nodes[i].ID == id {
			previous := r.nodes
			updated := make([]node, 0, len(previous)-1)
			updated = append(updated, previous[:i]...)
			updated = append(updated, previous[i+1:]...)
			r.nodes = updated
			if err := r.saveLocked(); err != nil {
				r.nodes = previous
				return err
			}
			return nil
		}
	}
	return os.ErrNotExist
}

type server struct {
	registry  *registry
	password  string
	static    string
	client    *http.Client
	transport *http.Transport
	loginMu   sync.Mutex
	logins    map[string]loginAttempt
}

func main() {
	addr := env("PANEL_ADDR", ":8080")
	data := env("PANEL_DATA", "/data/nodes.json")
	static := env("PANEL_STATIC", "/srv")
	r, err := loadRegistry(data)
	if err != nil {
		log.Fatal(err)
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ResponseHeaderTimeout: 8 * time.Second,
		IdleConnTimeout:       60 * time.Second,
	}
	s := &server{
		registry:  r,
		password:  os.Getenv("PANEL_PASSWORD"),
		static:    static,
		client:    &http.Client{Timeout: 5 * time.Second, Transport: transport},
		transport: transport,
		logins:    make(map[string]loginAttempt),
	}
	log.Printf("panel gateway listening on %s; nodes=%d auth=%t", addr, len(r.list()), s.password != "")
	log.Fatal(http.ListenAndServe(addr, s.routes()))
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("POST /api/logout", s.logout)
	mux.HandleFunc("GET /api/session", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("GET /api/nodes", s.listNodes)
	mux.HandleFunc("POST /api/nodes", s.createNode)
	mux.HandleFunc("DELETE /api/nodes/{id}", s.deleteNode)
	mux.HandleFunc("POST /api/nodes/{id}/select", s.selectNode)
	mux.HandleFunc("GET /api/nodes/{id}/status", s.nodeStatus)
	mux.HandleFunc("/api/node/{path...}", s.proxyNode)
	mux.HandleFunc("/", s.serveStatic)
	return s.auth(mux)
}

func (s *server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/login" || s.password == "" || s.sessionValid(r) {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			jsonError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	if s.password == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	client, _, _ := net.SplitHostPort(r.RemoteAddr)
	if client == "" {
		client = r.RemoteAddr
	}
	if !s.loginAllowed(client, time.Now()) {
		jsonError(w, http.StatusTooManyRequests, "too many login attempts")
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body) != nil ||
		subtle.ConstantTimeCompare([]byte(body.Password), []byte(s.password)) != 1 {
		s.recordLoginFailure(client, time.Now())
		jsonError(w, http.StatusUnauthorized, "invalid password")
		return
	}
	s.clearLoginFailures(client)
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: s.sessionValue(time.Now().Add(sessionTTL)), Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: requestSecure(r), MaxAge: int(sessionTTL.Seconds())})
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: requestSecure(r), MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) sessionValue(expires time.Time) string {
	expiry := strconv.FormatInt(expires.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(s.password))
	mac.Write([]byte("boardproxy-panel-session-v1:" + expiry))
	return expiry + "." + hex.EncodeToString(mac.Sum(nil))
}

func (s *server) sessionValid(r *http.Request) bool {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}
	expiryRaw, _, ok := strings.Cut(cookie.Value, ".")
	if !ok {
		return false
	}
	expiry, err := strconv.ParseInt(expiryRaw, 10, 64)
	if err != nil || time.Now().After(time.Unix(expiry, 0)) {
		return false
	}
	want := s.sessionValue(time.Unix(expiry, 0))
	return len(cookie.Value) == len(want) && subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(want)) == 1
}

func (s *server) loginAllowed(client string, now time.Time) bool {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	a := s.logins[client]
	return now.Sub(a.windowStart) >= loginWindow || a.failures < loginLimit
}

func (s *server) recordLoginFailure(client string, now time.Time) {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	if s.logins == nil {
		s.logins = make(map[string]loginAttempt)
	}
	a := s.logins[client]
	if a.windowStart.IsZero() || now.Sub(a.windowStart) >= loginWindow {
		a = loginAttempt{windowStart: now}
	}
	a.failures++
	s.logins[client] = a
}

func (s *server) clearLoginFailures(client string) {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	delete(s.logins, client)
}

func (s *server) listNodes(w http.ResponseWriter, r *http.Request) {
	selected := selectedNode(r)
	nodes := s.registry.list()
	out := make([]publicNode, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, expose(n, selected == n.ID))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) createNode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name      string `json:"name"`
		Host      string `json:"host"`
		Port      int    `json:"port"`
		TLS       bool   `json:"tls"`
		AccessKey string `json:"access_key"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid body")
		return
	}
	body.Name, body.Host, body.AccessKey = strings.TrimSpace(body.Name), strings.TrimSpace(body.Host), strings.TrimSpace(body.AccessKey)
	if body.Name == "" || body.Host == "" || body.Port < 1 || body.Port > 65535 || !strings.HasPrefix(body.AccessKey, "bpa_") {
		jsonError(w, http.StatusBadRequest, "name, host, port and bpa_ access key are required")
		return
	}
	if net.ParseIP(strings.Trim(body.Host, "[]")) == nil && strings.ContainsAny(body.Host, "/:@") {
		jsonError(w, http.StatusBadRequest, "invalid host")
		return
	}
	n := node{ID: randomID(), Name: body.Name, Host: body.Host, Port: body.Port, TLS: body.TLS,
		AccessKey: body.AccessKey, CreatedAt: time.Now().UTC()}
	if err := s.registry.add(n); err != nil {
		if errors.Is(err, errNodeExists) {
			jsonError(w, http.StatusConflict, err.Error())
		} else {
			jsonError(w, http.StatusInternalServerError, "save node registry")
		}
		return
	}
	http.SetCookie(w, &http.Cookie{Name: nodeCookie, Value: n.ID, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: requestSecure(r), MaxAge: 365 * 24 * 60 * 60})
	writeJSON(w, http.StatusCreated, expose(n, true))
}

func (s *server) deleteNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.registry.remove(id); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			jsonError(w, http.StatusNotFound, "node not found")
		} else {
			jsonError(w, http.StatusInternalServerError, "save node registry")
		}
		return
	}
	if selectedNode(r) == id {
		http.SetCookie(w, &http.Cookie{Name: nodeCookie, Path: "/", HttpOnly: true,
			SameSite: http.SameSiteLaxMode, Secure: requestSecure(r), MaxAge: -1})
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) selectNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.registry.get(id); !ok {
		jsonError(w, http.StatusNotFound, "node not found")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: nodeCookie, Value: id, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: requestSecure(r), MaxAge: 365 * 24 * 60 * 60})
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) nodeStatus(w http.ResponseWriter, r *http.Request) {
	n, ok := s.registry.get(r.PathValue("id"))
	if !ok {
		jsonError(w, http.StatusNotFound, "node not found")
		return
	}
	start := time.Now()
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, n.baseURL()+"/stats", nil)
	req.Header.Set("Authorization", "Bearer "+n.AccessKey)
	resp, err := s.client.Do(req)
	out := map[string]any{"online": false, "checked_at": time.Now().UTC(), "latency_ms": time.Since(start).Milliseconds()}
	if err != nil {
		out["error"] = err.Error()
	} else {
		resp.Body.Close()
		out["online"] = resp.StatusCode == http.StatusOK
		if resp.StatusCode == http.StatusUnauthorized {
			out["error"] = "access key rejected"
		} else if resp.StatusCode != http.StatusOK {
			out["error"] = "node returned HTTP " + strconv.Itoa(resp.StatusCode)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) proxyNode(w http.ResponseWriter, r *http.Request) {
	n, ok := s.registry.get(selectedNode(r))
	if !ok {
		jsonError(w, http.StatusConflict, "select a node first")
		return
	}
	target, _ := url.Parse(n.baseURL())
	proxy := httputil.NewSingleHostReverseProxy(target)
	if s.transport != nil {
		proxy.Transport = s.transport
	}
	original := proxy.Director
	proxy.Director = func(req *http.Request) {
		original(req)
		req.URL.Path = "/" + r.PathValue("path")
		req.URL.RawPath = ""
		req.Header.Set("Authorization", "Bearer "+n.AccessKey)
		req.Header.Del("Cookie")
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		jsonError(w, http.StatusBadGateway, "node unavailable: "+err.Error())
	}
	proxy.ServeHTTP(w, r)
}

func (s *server) serveStatic(w http.ResponseWriter, r *http.Request) {
	clean := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		http.NotFound(w, r)
		return
	}
	if clean == "." {
		clean = "index.html"
	}
	path := filepath.Join(s.static, clean)
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		http.ServeFile(w, r, path)
		return
	}
	if _, err := os.Stat(filepath.Join(s.static, "index.html")); err == nil {
		http.ServeFile(w, r, filepath.Join(s.static, "index.html"))
		return
	}
	if !errors.Is(errNoStatic(s.static), fs.ErrNotExist) {
		jsonError(w, http.StatusInternalServerError, "static files unavailable")
		return
	}
	http.NotFound(w, r)
}

func (n node) baseURL() string {
	scheme := "http"
	if n.TLS {
		scheme = "https"
	}
	return scheme + "://" + net.JoinHostPort(strings.Trim(n.Host, "[]"), strconv.Itoa(n.Port))
}

func expose(n node, selected bool) publicNode {
	hint := n.AccessKey
	if len(hint) > 12 {
		hint = hint[:12] + "…"
	}
	return publicNode{ID: n.ID, Name: n.Name, Host: n.Host, Port: n.Port, TLS: n.TLS,
		KeyHint: hint, CreatedAt: n.CreatedAt, Selected: selected}
}

func selectedNode(r *http.Request) string {
	cookie, _ := r.Cookie(nodeCookie)
	if cookie == nil {
		return ""
	}
	return cookie.Value
}

func randomID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func requestSecure(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func jsonError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func errNoStatic(path string) error {
	_, err := os.Stat(path)
	return err
}
