//go:build windows

package tun

import (
	"math/bits"

	"golang.org/x/sys/windows"
)

// Опции сокета для выбора исходящего интерфейса (нет в x/sys/windows как
// константы во всех версиях — задаём явно).
const (
	ipUnicastIF   = 31 // IPPROTO_IP, значение — индекс в network byte order
	ipv6UnicastIF = 31 // IPPROTO_IPV6, значение — индекс в host byte order
)

// windowsProtector направляет control-plane сокеты через физический интерфейс
// опцией IP_UNICAST_IF, чтобы трафик к доске шёл мимо TUN.
type windowsProtector struct {
	index int
	dns   string
}

func (p *windowsProtector) Protect(fd int) bool {
	if p.index == 0 {
		return false
	}
	h := windows.Handle(fd)
	// Для IPv4 индекс передаётся в network byte order; Windows всегда
	// little-endian, поэтому переставляем байты.
	v4val := int(bits.ReverseBytes32(uint32(p.index)))
	v4 := windows.SetsockoptInt(h, windows.IPPROTO_IP, ipUnicastIF, v4val)
	v6 := windows.SetsockoptInt(h, windows.IPPROTO_IPV6, ipv6UnicastIF, p.index)
	return v4 == nil || v6 == nil
}

func (p *windowsProtector) DNSAddress() string { return p.dns }
