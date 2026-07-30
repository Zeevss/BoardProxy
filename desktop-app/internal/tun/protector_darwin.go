//go:build darwin

package tun

import "golang.org/x/sys/unix"

// darwinProtector привязывает control-plane сокеты к физическому интерфейсу
// через IP_BOUND_IF (IPv4) / IPV6_BOUND_IF (IPv6), чтобы трафик к доске шёл мимо
// TUN и не создавал петлю.
type darwinProtector struct {
	index int    // индекс физического интерфейса
	dns   string // upstream-резолвер, достижимый через этот интерфейс
}

func (p *darwinProtector) Protect(fd int) bool {
	if p.index == 0 {
		return false
	}
	// Тип сокета (v4/v6) на этом этапе неизвестен; пробуем обе опции, успех хотя
	// бы одной означает, что нужный интерфейс задан.
	v4 := unix.SetsockoptInt(fd, unix.IPPROTO_IP, unix.IP_BOUND_IF, p.index)
	v6 := unix.SetsockoptInt(fd, unix.IPPROTO_IPV6, unix.IPV6_BOUND_IF, p.index)
	return v4 == nil || v6 == nil
}

func (p *darwinProtector) DNSAddress() string { return p.dns }
