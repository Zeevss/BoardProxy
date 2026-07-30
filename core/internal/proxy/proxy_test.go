package proxy

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"testing"
	"time"

	"bproxy-core/internal/board/memory"
	"bproxy-core/internal/codec"
	"bproxy-core/internal/link"
	"bproxy-core/internal/mux"
	"bproxy-core/internal/relay"
)

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// echoTCP starts a local TCP echo server and returns its address.
func echoTCP(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) { defer c.Close(); _, _ = io.Copy(c, c) }(c)
		}
	}()
	return ln.Addr().String()
}

func echoUDP(t *testing.T) string {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	go func() {
		buf := make([]byte, 65535)
		for {
			n, peer, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			_, _ = conn.WriteToUDP(buf[:n], peer)
		}
	}()
	return conn.LocalAddr().String()
}

// proxyOverBoard wires a client mux (proxy dialer) to a server mux that dials
// real targets, both over an in-memory board, and starts the proxy on a fresh
// listener. It returns the proxy's address.
func proxyOverBoard(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	b := memory.NewBoard()
	sa := b.NewSession("client")
	sb := b.NewSession("server")
	if _, err := sa.Subscribe(ctx, "page"); err != nil {
		t.Fatal(err)
	}
	if _, err := sb.Subscribe(ctx, "page"); err != nil {
		t.Fatal(err)
	}
	clientMux := mux.New(link.New(sa, codec.Base64Codec{}, link.Options{}), mux.Options{Client: true})
	serverMux := mux.New(link.New(sb, codec.Base64Codec{}, link.Options{}), mux.Options{})
	t.Cleanup(func() { clientMux.Close(); serverMux.Close() })

	// Server egress: accept streams, dial target, relay.
	go func() {
		for {
			st, err := serverMux.AcceptStream(ctx)
			if err != nil {
				return
			}
			go func(st *mux.Stream) {
				conn, err := net.Dial("tcp", st.Target())
				if err != nil {
					_ = st.Reset()
					return
				}
				defer conn.Close()
				relay.Pipe(st, conn.(*net.TCPConn))
				_ = st.Close()
			}(st)
		}
	}()
	go func() {
		for {
			d, err := serverMux.AcceptDatagram(ctx)
			if err != nil {
				return
			}
			go func(d *mux.Datagram) {
				defer d.Close()
				for {
					packet, err := d.Receive(ctx)
					if err != nil {
						return
					}
					conn, err := net.Dial("udp", packet.Target)
					if err != nil {
						continue
					}
					_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
					_, _ = conn.Write(packet.Payload)
					buf := make([]byte, 65535)
					n, err := conn.Read(buf)
					_ = conn.Close()
					if err == nil {
						_ = d.Send(packet.Target, buf[:n])
					}
				}
			}(d)
		}
	}()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	pctx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	go serve(pctx, ln, &router{d: clientMux, opts: Options{EnableUDP: true}, log: discardLog()}, discardLog())
	return ln.Addr().String()
}

func TestSOCKS5UDPRoundtrip(t *testing.T) {
	target := echoUDP(t)
	proxyAddr := proxyOverBoard(t)

	control, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	_ = control.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := control.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		t.Fatal(err)
	}
	greet := make([]byte, 2)
	if _, err := io.ReadFull(control, greet); err != nil {
		t.Fatal(err)
	}
	// UDP ASSOCIATE, client endpoint unknown (0.0.0.0:0).
	if _, err := control.Write([]byte{0x05, 0x03, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 10)
	if _, err := io.ReadFull(control, reply); err != nil {
		t.Fatal(err)
	}
	if reply[1] != socksRepSuccess || reply[3] != socksAtypIPv4 {
		t.Fatalf("UDP ASSOCIATE reply = %v", reply)
	}
	relayAddr := &net.UDPAddr{
		IP:   net.IPv4(reply[4], reply[5], reply[6], reply[7]),
		Port: int(binary.BigEndian.Uint16(reply[8:10])),
	}
	udp, err := net.DialUDP("udp", nil, relayAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	_ = udp.SetDeadline(time.Now().Add(10 * time.Second))
	payload := []byte("udp over board datagram")
	packet, err := encodeSOCKS5UDP(target, payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := udp.Write(packet); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 65535)
	n, err := udp.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	gotTarget, gotPayload, err := decodeSOCKS5UDP(buf[:n])
	if err != nil {
		t.Fatal(err)
	}
	if gotTarget != target || string(gotPayload) != string(payload) {
		t.Fatalf("UDP reply target=%q payload=%q", gotTarget, gotPayload)
	}
}

func TestSOCKS5Roundtrip(t *testing.T) {
	target := echoTCP(t)
	proxyAddr := proxyOverBoard(t)

	c, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(10 * time.Second))

	// Greeting: version 5, one method (no-auth).
	if _, err := c.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		t.Fatal(err)
	}
	greet := make([]byte, 2)
	if _, err := io.ReadFull(c, greet); err != nil {
		t.Fatal(err)
	}
	if greet[0] != 0x05 || greet[1] != 0x00 {
		t.Fatalf("bad greeting reply: %v", greet)
	}

	// CONNECT request to the echo target (domain atyp).
	host, portStr, _ := net.SplitHostPort(target)
	port, _ := strconv.Atoi(portStr)
	req := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
	req = append(req, host...)
	req = append(req, byte(port>>8), byte(port))
	if _, err := c.Write(req); err != nil {
		t.Fatal(err)
	}
	rep := make([]byte, 10)
	if _, err := io.ReadFull(c, rep); err != nil {
		t.Fatal(err)
	}
	if rep[1] != 0x00 {
		t.Fatalf("connect failed, rep=%v", rep)
	}

	// Tunnel established: echo check.
	msg := "socks5 over a whiteboard"
	if _, err := io.WriteString(c, msg); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(msg))
	if _, err := io.ReadFull(c, got); err != nil {
		t.Fatal(err)
	}
	if string(got) != msg {
		t.Fatalf("echo mismatch: got %q want %q", got, msg)
	}
}

// noopDialer never has OpenStream called in TestServeDrainsStuckConnection:
// the accepted connection never sends its first byte, so handleConn blocks
// before reaching the dialer.
type noopDialer struct{}

func (noopDialer) OpenStream(string) (*mux.Stream, error) { return nil, io.EOF }

type lifecycleNoopDialer struct {
	done chan struct{}
}

func (d *lifecycleNoopDialer) OpenStream(string) (*mux.Stream, error) { return nil, io.EOF }
func (d *lifecycleNoopDialer) Done() <-chan struct{}                  { return d.done }

// TestServeStopsWhenTunnelSessionEnds фиксирует связь между GOAWAY/mux.Done и
// локальным listener'ом. Без неё proxy продолжал слушать порт, а Client.Run не
// получал управление для повторного rendezvous.
func TestServeStopsWhenTunnelSessionEnds(t *testing.T) {
	d := &lifecycleNoopDialer{done: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		done <- Serve(context.Background(), "127.0.0.1:0", d, discardLog(), Options{})
	}()
	time.Sleep(50 * time.Millisecond)
	close(d.done)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Serve after tunnel close = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve kept listening after tunnel session ended")
	}
}

// TestServeDrainsStuckConnection regression-tests shutdown of an accepted
// connection that never sends its first byte. Cancellation closes accepted
// sockets, so Peek unblocks promptly instead of leaking a goroutine after the
// fallback drain timeout.
func TestServeDrainsStuckConnection(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	start := time.Now()
	go func() {
		_ = serve(ctx, ln, &router{d: noopDialer{}, log: discardLog()}, discardLog())
		close(done)
	}()
	// Give the accept loop a moment to accept the stuck connection before
	// triggering shutdown.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(shutdownDrainTimeout + 4*time.Second):
		t.Fatal("serve did not return within the drain timeout")
	}
	elapsed := time.Since(start)
	if elapsed >= shutdownDrainTimeout {
		t.Fatalf("serve took %v, accepted socket was not closed promptly", elapsed)
	}
}

func TestHTTPConnectRoundtrip(t *testing.T) {
	target := echoTCP(t)
	proxyAddr := proxyOverBoard(t)

	c, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(10 * time.Second))

	fmt.Fprintf(c, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
	br := bufio.NewReader(c)
	line, err := br.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if len(line) < 12 || line[9:12] != "200" {
		t.Fatalf("bad CONNECT status: %q", line)
	}
	// Drain the rest of the response headers (blank line).
	for {
		l, err := br.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if l == "\r\n" {
			break
		}
	}

	msg := "http connect over a whiteboard"
	if _, err := io.WriteString(c, msg); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(msg))
	if _, err := io.ReadFull(br, got); err != nil {
		t.Fatal(err)
	}
	if string(got) != msg {
		t.Fatalf("echo mismatch: got %q want %q", got, msg)
	}
}
