// Package netprotect creates outbound transports whose sockets can be excluded
// from an OS-level VPN before connect. Android implements Protector with
// VpnService.protect(fd); desktop callers normally pass nil.
package netprotect

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"
)

// Protector is called after a socket is created but before it connects.
type Protector interface {
	Protect(fd int) bool
}

type dnsProvider interface {
	DNSAddress() string
}

// Dialer returns a net.Dialer that protects every socket when p is non-nil.
func Dialer(p Protector) *net.Dialer {
	d := protectedDialer(p)
	if p != nil {
		// net.Dialer.ControlContext protects the destination socket, but name
		// resolution happens on separate UDP/TCP sockets. Give the Go resolver
		// its own protected dialer so Android full-tunnel DNS cannot loop back
		// into TUN before tun2socks has started (and remains safe on reconnect).
		resolverDialer := protectedDialer(p)
		d.Resolver = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				if provider, ok := p.(dnsProvider); ok {
					if dns := net.ParseIP(provider.DNSAddress()); dns != nil {
						address = net.JoinHostPort(dns.String(), "53")
					}
				}
				return resolverDialer.DialContext(ctx, network, address)
			},
		}
	}
	return d
}

func protectedDialer(p Protector) *net.Dialer {
	d := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	if p == nil {
		return d
	}
	d.ControlContext = func(_ context.Context, network, address string, raw syscall.RawConn) error {
		var protectErr error
		if err := raw.Control(func(fd uintptr) {
			if !p.Protect(int(fd)) {
				protectErr = fmt.Errorf("protect socket %s %s: rejected", network, address)
			}
		}); err != nil {
			return fmt.Errorf("protect socket %s %s: %w", network, address, err)
		}
		return protectErr
	}
	return d
}

// HTTPClient returns an HTTP client backed by the protected dialer. Callers may
// attach a cookie jar; the transport is safe to reuse for REST and WebSocket.
func HTTPClient(p Protector) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = Dialer(p).DialContext
	return &http.Client{Transport: transport}
}
