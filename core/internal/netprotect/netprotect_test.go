package netprotect

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

type recordingProtector struct {
	called atomic.Int32
	allow  bool
}

type dnsRecordingProtector struct {
	recordingProtector
	dns string
}

func (p *dnsRecordingProtector) DNSAddress() string { return p.dns }

func (p *recordingProtector) Protect(fd int) bool {
	if fd < 0 {
		return false
	}
	p.called.Add(1)
	return p.allow
}

func TestDialerProtectsSocketBeforeConnect(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			_ = conn.Close()
		}
	}()

	p := &recordingProtector{allow: true}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, err := Dialer(p).DialContext(ctx, "tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	if p.called.Load() == 0 {
		t.Fatal("Protector was not called")
	}
}

func TestDialerAbortsWhenProtectorRejects(t *testing.T) {
	p := &recordingProtector{allow: false}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := Dialer(p).DialContext(ctx, "tcp", "127.0.0.1:9"); err == nil {
		t.Fatal("DialContext succeeded after Protector rejected the socket")
	}
	if p.called.Load() == 0 {
		t.Fatal("Protector was not called")
	}
}

func TestDialerUsesProtectedGoResolverWhenProtectorIsPresent(t *testing.T) {
	p := &dnsRecordingProtector{
		recordingProtector: recordingProtector{allow: true},
		dns:                "1.1.1.1",
	}
	d := Dialer(p)
	if d.Resolver == nil || !d.Resolver.PreferGo {
		t.Fatal("protected dialer must use an explicit Go resolver")
	}
	if Dialer(nil).Resolver != nil {
		t.Fatal("desktop dialer should retain the platform default resolver")
	}
}
