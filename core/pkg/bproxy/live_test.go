package bproxy

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
	"strings"
	"sync"
	"testing"
	"time"

	"bproxy-core/internal/app"
	"bproxy-core/internal/boardtest"
	"bproxy-core/internal/crypto"
	"bproxy-core/internal/keylink"
	"bproxy-core/internal/serverconfig"
)

// TestLiveProvisionAndConnect checks the complete config-driven path: a runtime
// starts with a pre-generated user identity, reconstructs its keylink and uses
// it to move traffic through the board.
func TestLiveProvisionAndConnect(t *testing.T) {
	hash := boardtest.LiveHash(t)
	wantBody := strings.Repeat("BOARDPROXY-V4-OFFSET-", 1<<16)

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, wantBody)
	}))
	defer target.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	link := startLiveServer(t, ctx, hash, "smoke")

	// Подключаемся выданным keylink.
	listen := freeAddr(t)
	c := New(Config{Keylink: link, Listen: listen, LogLevel: "warn"})
	go func() { _ = c.Run(ctx) }()
	defer c.Stop()

	if err := waitDial(listen, 30*time.Second); err != nil {
		t.Fatalf("client proxy never came up: %v", err)
	}
	body, err := socks5HTTPGet(listen, target.Listener.Addr().String())
	if err != nil {
		t.Fatalf("request through board proxy: %v", err)
	}
	if body != wantBody {
		t.Fatalf("unexpected body length: got=%d want=%d", len(body), len(wantBody))
	}
	t.Log("config-provisioned client tunneled through the board")
}

// TestLiveClientMetrics поднимает сервер на живой доске, гоняет запрос через
// pkg-клиента и проверяет, что колбэки статуса и метрик отработали с реальным
// трафиком (Connected + ненулевые байты). Пропускается без BPROXY_LIVE и DSN.
func TestLiveClientMetrics(t *testing.T) {
	hash := boardtest.LiveHash(t)

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "BOARDPROXY-OK")
	}))
	defer target.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	link := startLiveServer(t, ctx, hash, "pkg-metrics")

	// Клиент через pkg.
	listen := freeAddr(t)
	c := New(Config{Keylink: link, Listen: listen, Board: hash, LogLevel: "warn"})

	var mu sync.Mutex
	var lastMetrics Metrics
	sawConnected := false
	c.OnStatus(func(s Status, _ error) {
		mu.Lock()
		if s == StatusConnected {
			sawConnected = true
		}
		mu.Unlock()
	})
	c.OnMetrics(func(m Metrics) {
		mu.Lock()
		lastMetrics = m
		mu.Unlock()
	})
	go func() { _ = c.Run(ctx) }()
	defer c.Stop()

	if err := waitDial(listen, 30*time.Second); err != nil {
		t.Fatalf("client proxy never came up: %v", err)
	}
	body, err := socks5HTTPGet(listen, target.Listener.Addr().String())
	if err != nil {
		t.Fatalf("request through board proxy: %v", err)
	}
	if body != "BOARDPROXY-OK" {
		t.Fatalf("unexpected body: %q", body)
	}

	// Дожидаемся тика метрик, отражающего прошедший трафик.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		m := lastMetrics
		connected := sawConnected
		mu.Unlock()
		if connected && m.TotalTx > 0 && m.TotalRx > 0 {
			t.Logf("метрики: tx=%d rx=%d rtt=%v streams=%d", m.TotalTx, m.TotalRx, m.RTT, m.Streams)
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	t.Fatalf("метрики не отразили трафик: connected=%v last=%+v", sawConnected, lastMetrics)
}

// TestLiveUDPRoundTrip verifies SOCKS5 UDP ASSOCIATE, mux datagram framing and
// the real server egress UDP socket over an actual board.
func TestLiveUDPRoundTrip(t *testing.T) {
	hash := boardtest.LiveHash(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	echo, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		buf := make([]byte, 65535)
		for {
			n, peer, err := echo.ReadFromUDP(buf)
			if err != nil {
				return
			}
			_, _ = echo.WriteToUDP(buf[:n], peer)
		}
	}()

	link := startLiveServer(t, ctx, hash, "udp-live")

	listen := freeAddr(t)
	client := New(Config{Keylink: link, Listen: listen, EnableUDP: true})
	go func() { _ = client.Run(ctx) }()
	defer client.Stop()
	if err := waitDial(listen, 30*time.Second); err != nil {
		t.Fatal(err)
	}

	payload := []byte("BOARDPROXY-UDP-OK")
	got, err := socks5UDPEcho(listen, echo.LocalAddr().String(), payload)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("UDP payload = %q, want %q", got, payload)
	}
}

func startLiveServer(t *testing.T, ctx context.Context, hash, tag string) string {
	t.Helper()
	serverKP, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	clientKP, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	enc := func(raw []byte) string { return "base64:" + base64.StdEncoding.EncodeToString(raw) }
	cfg := serverconfig.Defaults()
	cfg.Server.PrivateKey = enc(serverKP.Private())
	cfg.Server.AllowPrivateEgress = true
	cfg.Management.GRPCListen = "unix://" + t.TempDir() + "/control.sock"
	cfg.Boards = []serverconfig.Board{{Tag: "live", Name: "Live", Hash: hash, MaxLanes: 8}}
	cfg.Users = []serverconfig.User{{Tag: tag, Name: tag, PrivateKey: enc(clientKP.Private()), Boards: []string{"live"}, MaxSessions: 4, MaxLanes: 8}}
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	go func() { _ = app.RunServer(ctx, cfg, "stdin:", quiet, nil) }()
	time.Sleep(4 * time.Second)
	link, err := keylink.Build(clientKP.Private(), serverKP.Public(), []string{hash}, tag)
	if err != nil {
		t.Fatal(err)
	}
	return link
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

// socks5HTTPGet выполняет SOCKS5 CONNECT к target через прокси и делает сырой
// HTTP/1.0 GET, возвращая тело ответа.
func socks5HTTPGet(proxyAddr, target string) (string, error) {
	c, err := net.DialTimeout("tcp", proxyAddr, 5*time.Second)
	if err != nil {
		return "", err
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(60 * time.Second))

	if _, err := c.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		return "", err
	}
	if _, err := io.ReadFull(c, make([]byte, 2)); err != nil {
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
	fmt.Fprintf(c, "GET / HTTP/1.0\r\nHost: %s\r\n\r\n", host)
	raw, err := io.ReadAll(c)
	if err != nil {
		return "", err
	}
	s := string(raw)
	const sep = "\r\n\r\n"
	for i := 0; i+len(sep) <= len(s); i++ {
		if s[i:i+len(sep)] == sep {
			return s[i+len(sep):], nil
		}
	}
	return s, nil
}

func socks5UDPEcho(proxyAddr, target string, payload []byte) ([]byte, error) {
	control, err := net.DialTimeout("tcp", proxyAddr, 5*time.Second)
	if err != nil {
		return nil, err
	}
	defer control.Close()
	_ = control.SetDeadline(time.Now().Add(60 * time.Second))
	if _, err := control.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(control, make([]byte, 2)); err != nil {
		return nil, err
	}
	if _, err := control.Write([]byte{0x05, 0x03, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		return nil, err
	}
	reply := make([]byte, 10)
	if _, err := io.ReadFull(control, reply); err != nil {
		return nil, err
	}
	if reply[1] != 0 || reply[3] != 1 {
		return nil, fmt.Errorf("socks UDP ASSOCIATE reply=%v", reply)
	}
	relay := &net.UDPAddr{
		IP:   net.IPv4(reply[4], reply[5], reply[6], reply[7]),
		Port: int(binary.BigEndian.Uint16(reply[8:10])),
	}
	udp, err := net.DialUDP("udp", nil, relay)
	if err != nil {
		return nil, err
	}
	defer udp.Close()
	_ = udp.SetDeadline(time.Now().Add(60 * time.Second))
	host, portText, err := net.SplitHostPort(target)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(host).To4()
	if ip == nil {
		return nil, fmt.Errorf("live UDP helper requires IPv4 target")
	}
	packet := []byte{0, 0, 0, 1}
	packet = append(packet, ip...)
	packet = binary.BigEndian.AppendUint16(packet, uint16(port))
	packet = append(packet, payload...)
	if _, err := udp.Write(packet); err != nil {
		return nil, err
	}
	buf := make([]byte, 65535)
	n, err := udp.Read(buf)
	if err != nil {
		return nil, err
	}
	if n < 10 || buf[0] != 0 || buf[1] != 0 || buf[2] != 0 {
		return nil, fmt.Errorf("malformed SOCKS UDP response")
	}
	return append([]byte(nil), buf[10:n]...), nil
}
