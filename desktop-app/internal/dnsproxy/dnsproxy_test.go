package dnsproxy

import (
	"net"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// fakeUpstream поднимает UDP-резолвер, который на любой A-запрос отвечает
// заданным адресом. Возвращает его адрес и функцию остановки.
func fakeUpstream(t *testing.T, answer [4]byte) (string, func()) {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("upstream listen: %v", err)
	}
	go func() {
		buf := make([]byte, 512)
		for {
			n, addr, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			var p dnsmessage.Parser
			header, err := p.Start(buf[:n])
			if err != nil {
				continue
			}
			q, err := p.Question()
			if err != nil {
				continue
			}
			b := dnsmessage.NewBuilder(nil, dnsmessage.Header{
				ID: header.ID, Response: true, RCode: dnsmessage.RCodeSuccess,
			})
			b.EnableCompression()
			_ = b.StartQuestions()
			_ = b.Question(q)
			_ = b.StartAnswers()
			_ = b.AResource(
				dnsmessage.ResourceHeader{Name: q.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 60},
				dnsmessage.AResource{A: answer},
			)
			msg, err := b.Finish()
			if err != nil {
				continue
			}
			_, _ = conn.WriteToUDP(msg, addr)
		}
	}()
	return conn.LocalAddr().String(), func() { _ = conn.Close() }
}

// query шлёт A-запрос на адрес резолвера и ждёт ответ.
func query(t *testing.T, resolver, name string) {
	t.Helper()
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: 1234, RecursionDesired: true})
	b.EnableCompression()
	_ = b.StartQuestions()
	_ = b.Question(dnsmessage.Question{
		Name:  dnsmessage.MustNewName(name + "."),
		Type:  dnsmessage.TypeA,
		Class: dnsmessage.ClassINET,
	})
	msg, err := b.Finish()
	if err != nil {
		t.Fatalf("build query: %v", err)
	}
	conn, err := net.Dial("udp", resolver)
	if err != nil {
		t.Fatalf("dial resolver: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write(msg); err != nil {
		t.Fatalf("write query: %v", err)
	}
	resp := make([]byte, 512)
	if _, err := conn.Read(resp); err != nil {
		t.Fatalf("read response: %v", err)
	}
}

// Форвардер должен отвечать клиенту и запоминать соответствие IP→домен: именно
// из этого кэша берутся имена для статистики в TUN-режиме.
func TestForwardsAndRecordsNames(t *testing.T) {
	upstream, stopUpstream := fakeUpstream(t, [4]byte{93, 184, 216, 34})
	defer stopUpstream()

	p, err := Start("127.0.0.1:0", upstream)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer p.Stop()

	query(t, p.conn.LocalAddr().String(), "example.com")

	// Запись в кэш происходит в обработчике; дадим ему завершиться.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if p.HostForIP("93.184.216.34") == "example.com" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("host for 93.184.216.34 = %q, want example.com", p.HostForIP("93.184.216.34"))
}

// Неизвестный IP не должен давать имя (иначе в статистике появятся выдумки).
func TestUnknownIPHasNoHost(t *testing.T) {
	upstream, stopUpstream := fakeUpstream(t, [4]byte{1, 2, 3, 4})
	defer stopUpstream()

	p, err := Start("127.0.0.1:0", upstream)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer p.Stop()

	if got := p.HostForIP("8.8.8.8"); got != "" {
		t.Fatalf("HostForIP(8.8.8.8) = %q, want empty", got)
	}
}

// Stop должен быть идемпотентным и безопасным на nil-приёмнике: helper зовёт его
// в teardown даже когда форвардер не поднялся.
func TestStopIsSafe(t *testing.T) {
	var nilProxy *Proxy
	nilProxy.Stop()

	upstream, stopUpstream := fakeUpstream(t, [4]byte{1, 2, 3, 4})
	defer stopUpstream()
	p, err := Start("127.0.0.1:0", upstream)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	p.Stop()
	p.Stop()
}
