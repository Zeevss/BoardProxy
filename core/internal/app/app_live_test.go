package app

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"bproxy-core/internal/boardtest"
	"bproxy-core/internal/crypto"
	"bproxy-core/internal/keylink"
	"bproxy-core/internal/serverconfig"
	"bproxy-core/pkg/bproxy"
)

// TestLiveEndToEnd runs the whole pipeline over a real board: a server (hub +
// egress) and a client (SOCKS5/HTTP proxy) join the board, and an HTTP request
// is fetched through the client's SOCKS5 proxy to a local target — proving the
// full path proxy → mux → link → board → egress → target and back.
//
// Skipped unless BPROXY_LIVE=1.
func TestLiveEndToEnd(t *testing.T) {
	hash := boardtest.LiveHash(t)

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "BOARDPROXY-OK")
	}))
	defer target.Close()
	targetAddr := target.Listener.Addr().String()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	serverKP, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	clientKP, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	enc := func(raw []byte) string { return "base64:" + base64.StdEncoding.EncodeToString(raw) }
	serverCfg := serverconfig.Defaults()
	serverCfg.Server.PrivateKey = enc(serverKP.Private())
	serverCfg.Server.AllowPrivateEgress = true
	serverCfg.Management.GRPCListen = "unix://" + t.TempDir() + "/control.sock"
	serverCfg.Boards = []serverconfig.Board{{Tag: "live", Name: "Live", Hash: hash, MaxLanes: 8}}
	serverCfg.Users = []serverconfig.User{{Tag: "e2e", Name: "e2e-client", PrivateKey: enc(clientKP.Private()), Boards: []string{"live"}, MaxSessions: 4, MaxLanes: 8}}

	cfg := bproxy.Config{LogLevel: "warn", Listen: freeAddr(t), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	cfg.Keylink, err = keylink.Build(clientKP.Private(), serverKP.Public(), []string{hash}, "e2e-client")
	if err != nil {
		t.Fatal(err)
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	srvErr := make(chan error, 1)
	go func() { srvErr <- RunServer(ctx, serverCfg, "stdin:", log, nil) }()

	// Give the server time to join the board and subscribe the hub before the
	// client starts rendezvous.
	time.Sleep(4 * time.Second)

	cliErr := make(chan error, 1)
	go func() { cliErr <- bproxy.New(cfg).Run(ctx) }()

	if err := waitDial(cfg.Listen, 30*time.Second); err != nil {
		t.Fatalf("client proxy never came up: %v (srv=%v cli=%v)", err, drain(srvErr), drain(cliErr))
	}

	body, err := socks5HTTPGet(cfg.Listen, targetAddr, "/")
	if err != nil {
		t.Fatalf("request through board proxy failed: %v", err)
	}
	if body != "BOARDPROXY-OK" {
		t.Fatalf("unexpected body over the board: %q", body)
	}
	t.Log("fetched local target through SOCKS5 over the live board — full pipeline OK")
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

func waitDial(addr string, d time.Duration) error {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			c.Close()
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("addr %s not reachable", addr)
}

func drain(ch chan error) error {
	select {
	case e := <-ch:
		return e
	default:
		return nil
	}
}

// socks5HTTPGet performs a SOCKS5 CONNECT to target through the proxy and issues
// a raw HTTP/1.0 GET, returning the response body.
func socks5HTTPGet(proxyAddr, target, path string) (string, error) {
	c, err := net.DialTimeout("tcp", proxyAddr, 5*time.Second)
	if err != nil {
		return "", err
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(60 * time.Second))

	if _, err := c.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		return "", err
	}
	greet := make([]byte, 2)
	if _, err := io.ReadFull(c, greet); err != nil {
		return "", err
	}

	host, portStr, _ := net.SplitHostPort(target)
	port, _ := strconv.Atoi(portStr)
	req := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
	req = append(req, host...)
	req = binary.BigEndian.AppendUint16(req, uint16(port))
	if _, err := c.Write(req); err != nil {
		return "", err
	}
	rep := make([]byte, 10)
	if _, err := io.ReadFull(c, rep); err != nil {
		return "", err
	}
	if rep[1] != 0x00 {
		return "", fmt.Errorf("socks connect rep=%d", rep[1])
	}

	fmt.Fprintf(c, "GET %s HTTP/1.0\r\nHost: %s\r\n\r\n", path, host)
	raw, err := io.ReadAll(c)
	if err != nil {
		return "", err
	}
	s := string(raw)
	if i := indexBody(s); i >= 0 {
		return s[i:], nil
	}
	return s, nil
}

// indexBody returns the offset just past the HTTP header terminator.
func indexBody(s string) int {
	const sep = "\r\n\r\n"
	for i := 0; i+len(sep) <= len(s); i++ {
		if s[i:i+len(sep)] == sep {
			return i + len(sep)
		}
	}
	return -1
}
