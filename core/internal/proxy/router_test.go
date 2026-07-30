package proxy

import (
	"errors"
	"net"
	"testing"

	"bproxy-core/internal/mux"
)

// captureDialer запоминает target, с которым звали OpenStream, и всегда
// возвращает ошибку — нам достаточно проверить, ЧТО ушло в туннель, а не
// поднимать настоящую mux-сессию.
type captureDialer struct{ target string }

func (c *captureDialer) OpenStream(target string) (*mux.Stream, error) {
	c.target = target
	return nil, errors.New("capture")
}

func TestRouterPassesHostnameThroughByDefault(t *testing.T) {
	cd := &captureDialer{}
	r := &router{d: cd, log: discardLog()}
	_, _ = r.dial("example.com:443")
	if cd.target != "example.com:443" {
		t.Fatalf("без local-dns имя должно уйти как есть, target = %q", cd.target)
	}
}

func TestRouterLocalDNSResolvesToIP(t *testing.T) {
	cd := &captureDialer{}
	r := &router{d: cd, opts: Options{LocalDNS: true}, log: discardLog()}
	_, _ = r.dial("localhost:80")
	host, port, err := net.SplitHostPort(cd.target)
	if err != nil {
		t.Fatalf("target %q не host:port: %v", cd.target, err)
	}
	if net.ParseIP(host) == nil {
		t.Fatalf("local-dns должен был подставить IP, а host = %q", host)
	}
	if port != "80" {
		t.Fatalf("порт должен сохраниться, port = %q", port)
	}
}

func TestRouterLocalDNSLeavesLiteralIP(t *testing.T) {
	cd := &captureDialer{}
	r := &router{d: cd, opts: Options{LocalDNS: true}, log: discardLog()}
	_, _ = r.dial("93.184.216.34:443")
	if cd.target != "93.184.216.34:443" {
		t.Fatalf("готовый IP не должен резолвиться повторно, target = %q", cd.target)
	}
}

func TestRouterBypassDialsDirectly(t *testing.T) {
	addr := echoTCP(t)
	cd := &captureDialer{}
	r := &router{d: cd, opts: Options{Bypass: func(string) bool { return true }}, log: discardLog()}
	conn, err := r.dial(addr)
	if err != nil {
		t.Fatalf("bypass dial: %v", err)
	}
	defer conn.Close()
	if _, ok := conn.(*net.TCPConn); !ok {
		t.Fatalf("bypass должен вернуть прямой *net.TCPConn, а вернул %T", conn)
	}
	if cd.target != "" {
		t.Fatalf("при bypass туннель не должен использоваться, а OpenStream звали с %q", cd.target)
	}
}
