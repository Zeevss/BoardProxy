// Пакет dnsproxy — локальный DNS-форвардер, который helper поднимает на адресе
// TUN-интерфейса. Система направляется на него; он пересылает запросы вверх по
// туннелю (через маршрут по умолчанию, который уже идёт в TUN) и параллельно
// запоминает соответствие IP→домен из ответов. Это даёт именам появиться в
// статистике, где TUN иначе видит только IP-адреса (домен резолвится до того,
// как пакет попадает в туннель).
package dnsproxy

import (
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// Proxy обслуживает UDP DNS на listenAddr и форвардит на upstream, ведя кэш
// IP→домен. Потокобезопасен.
type Proxy struct {
	upstream string
	conn     *net.UDPConn

	mu    sync.RWMutex
	names map[string]string // ip -> host

	closeOnce sync.Once
}

// Start открывает UDP-сокет на listenAddr (напр. "10.89.0.2:53") и начинает
// обслуживать запросы, форвардя их на upstream (напр. "1.1.1.1:53").
func Start(listenAddr, upstream string) (*Proxy, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", listenAddr)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, err
	}
	p := &Proxy{
		upstream: upstream,
		conn:     conn,
		names:    make(map[string]string),
	}
	go p.serve()
	return p, nil
}

// HostForIP возвращает домен для IP, если он встречался в ответах DNS.
func (p *Proxy) HostForIP(ip string) string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.names[ip]
}

// Stop закрывает сокет.
func (p *Proxy) Stop() {
	if p == nil {
		return
	}
	p.closeOnce.Do(func() { _ = p.conn.Close() })
}

func (p *Proxy) serve() {
	buf := make([]byte, 4096)
	for {
		n, client, err := p.conn.ReadFromUDP(buf)
		if err != nil {
			return // сокет закрыт
		}
		query := make([]byte, n)
		copy(query, buf[:n])
		go p.handle(query, client)
	}
}

func (p *Proxy) handle(query []byte, client *net.UDPAddr) {
	resp, err := p.forward(query)
	if err != nil {
		return
	}
	p.record(resp)
	_, _ = p.conn.WriteToUDP(resp, client)
}

// forward пересылает запрос на upstream и возвращает ответ. Сокет намеренно не
// защищён Protector'ом — пакет уходит через маршрут по умолчанию (в TUN), т.е.
// сам DNS тоже идёт через туннель.
func (p *Proxy) forward(query []byte) ([]byte, error) {
	c, err := net.DialTimeout("udp", p.upstream, 5*time.Second)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write(query); err != nil {
		return nil, err
	}
	resp := make([]byte, 4096)
	n, err := c.Read(resp)
	if err != nil {
		return nil, err
	}
	return resp[:n], nil
}

// record разбирает ответ DNS и запоминает IP→домен (по имени из вопроса).
func (p *Proxy) record(resp []byte) {
	var parser dnsmessage.Parser
	if _, err := parser.Start(resp); err != nil {
		return
	}
	qs, err := parser.AllQuestions()
	if err != nil || len(qs) == 0 {
		return
	}
	name := strings.TrimSuffix(qs[0].Name.String(), ".")
	if name == "" {
		return
	}
	answers, err := parser.AllAnswers()
	if err != nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, a := range answers {
		switch r := a.Body.(type) {
		case *dnsmessage.AResource:
			p.names[net.IP(r.A[:]).String()] = name
		case *dnsmessage.AAAAResource:
			p.names[net.IP(r.AAAA[:]).String()] = name
		}
	}
}
