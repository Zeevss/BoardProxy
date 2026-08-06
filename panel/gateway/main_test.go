package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPanelSessionHasServerSideExpiry(t *testing.T) {
	s := &server{password: "secret"}
	valid := httptest.NewRequest(http.MethodGet, "/api/session", nil)
	valid.AddCookie(&http.Cookie{Name: sessionCookie, Value: s.sessionValue(time.Now().Add(time.Minute))})
	if !s.sessionValid(valid) {
		t.Fatal("fresh session rejected")
	}
	expired := httptest.NewRequest(http.MethodGet, "/api/session", nil)
	expired.AddCookie(&http.Cookie{Name: sessionCookie, Value: s.sessionValue(time.Now().Add(-time.Minute))})
	if s.sessionValid(expired) {
		t.Fatal("expired session accepted")
	}
}

func TestRegistryMutationRollsBackWhenPersistenceFails(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &registry{path: filepath.Join(blocker, "nodes.json"), nodes: []node{}}
	if err := r.add(node{ID: "n1", Host: "host", Port: 8080}); err == nil || len(r.nodes) != 0 {
		t.Fatalf("add error=%v nodes=%d", err, len(r.nodes))
	}
	r.nodes = []node{{ID: "n1", Host: "host", Port: 8080}}
	if err := r.remove("n1"); err == nil || len(r.nodes) != 1 {
		t.Fatalf("remove error=%v nodes=%d", err, len(r.nodes))
	}
}

func TestSelectedNodeProxyInjectsKeyAndHidesItFromList(t *testing.T) {
	const accessKey = "bpa_super-secret"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/stats" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+accessKey {
			t.Fatalf("authorization = %q", got)
		}
		writeJSON(w, http.StatusOK, map[string]int{"clients": 1})
	}))
	defer upstream.Close()
	parsed, _ := url.Parse(upstream.URL)
	host, port, _ := strings.Cut(parsed.Host, ":")
	portNumber := 0
	_, _ = fmt.Sscan(port, &portNumber)

	r, err := loadRegistry(filepath.Join(t.TempDir(), "nodes.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.add(node{ID: "n1", Name: "one", Host: host, Port: portNumber, AccessKey: accessKey, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	s := &server{registry: r, client: &http.Client{Timeout: time.Second}}

	listReq := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	listW := httptest.NewRecorder()
	s.routes().ServeHTTP(listW, listReq)
	if strings.Contains(listW.Body.String(), accessKey) {
		t.Fatal("node list leaked access key")
	}

	proxyReq := httptest.NewRequest(http.MethodGet, "/api/node/stats", nil)
	proxyReq.AddCookie(&http.Cookie{Name: nodeCookie, Value: "n1"})
	proxyW := httptest.NewRecorder()
	s.routes().ServeHTTP(proxyW, proxyReq)
	if proxyW.Code != http.StatusOK {
		t.Fatalf("proxy status = %d body=%s", proxyW.Code, proxyW.Body.String())
	}
	var body map[string]int
	if err := json.Unmarshal(proxyW.Body.Bytes(), &body); err != nil || body["clients"] != 1 {
		t.Fatalf("proxy body = %s err=%v", proxyW.Body.String(), err)
	}
}

func TestNodeUnauthorizedDoesNotInvalidatePanelSession(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		jsonError(w, http.StatusUnauthorized, "unauthorized")
	}))
	defer upstream.Close()
	parsed, _ := url.Parse(upstream.URL)
	host, port, _ := strings.Cut(parsed.Host, ":")
	portNumber := 0
	_, _ = fmt.Sscan(port, &portNumber)

	r, err := loadRegistry(filepath.Join(t.TempDir(), "nodes.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.add(node{ID: "n1", Name: "one", Host: host, Port: portNumber, AccessKey: "bpa_wrong"}); err != nil {
		t.Fatal(err)
	}
	s := &server{registry: r, client: &http.Client{Timeout: time.Second}}
	req := httptest.NewRequest(http.MethodGet, "/api/node/stats", nil)
	req.AddCookie(&http.Cookie{Name: nodeCookie, Value: "n1"})
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, req)
	if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), "node access key rejected") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
