package egress

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"bproxy-core/internal/board/memory"
	"bproxy-core/internal/codec"
	"bproxy-core/internal/link"
	"bproxy-core/internal/mux"
)

func TestDatagramEgressRoundTripAndClose(t *testing.T) {
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

	board := memory.NewBoard()
	clientBoard := board.NewSession("client")
	serverBoard := board.NewSession("server")
	if _, err := clientBoard.Subscribe(context.Background(), "page"); err != nil {
		t.Fatal(err)
	}
	if _, err := serverBoard.Subscribe(context.Background(), "page"); err != nil {
		t.Fatal(err)
	}
	clientMux := mux.New(link.New(clientBoard, codec.Base64Codec{}, link.Options{}), mux.Options{Client: true})
	serverMux := mux.New(link.New(serverBoard, codec.Base64Codec{}, link.Options{}), mux.Options{})
	defer clientMux.Close()
	defer serverMux.Close()

	clientDatagram, err := clientMux.OpenDatagram()
	if err != nil {
		t.Fatal(err)
	}
	serverDatagram, err := serverMux.AcceptDatagram(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		serveDatagram(ctx, serverDatagram, slog.New(slog.NewTextHandler(io.Discard, nil)), Options{allowLoopbackForTest: true})
		close(done)
	}()

	payload := []byte("real UDP egress")
	if err := clientDatagram.Send(echo.LocalAddr().String(), payload); err != nil {
		t.Fatal(err)
	}
	recvCtx, stopRecv := context.WithTimeout(context.Background(), 5*time.Second)
	packet, err := clientDatagram.Receive(recvCtx)
	stopRecv()
	if err != nil {
		t.Fatal(err)
	}
	if string(packet.Payload) != string(payload) {
		t.Fatalf("payload = %q, want %q", packet.Payload, payload)
	}

	_ = clientDatagram.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("egress UDP socket was not released after association close")
	}
}

func TestEgressIPPolicy(t *testing.T) {
	tests := []struct {
		ip      string
		opts    Options
		allowed bool
	}{
		{"127.0.0.1", Options{AllowPrivate: true}, false},
		{"169.254.169.254", Options{AllowPrivate: true}, false},
		{"10.0.0.1", Options{}, false},
		{"10.0.0.1", Options{AllowPrivate: true}, true},
		{"1.1.1.1", Options{}, true},
		{"::1", Options{AllowPrivate: true}, false},
	}
	for _, tt := range tests {
		if got := egressIPAllowed(net.ParseIP(tt.ip), tt.opts); got != tt.allowed {
			t.Errorf("egressIPAllowed(%s) = %v, want %v", tt.ip, got, tt.allowed)
		}
	}
}
