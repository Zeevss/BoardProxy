//go:build linux

package tun

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// verifyPrivileges заранее проверяет, что у процесса есть право привязывать
// сокеты к интерфейсу (CAP_NET_RAW / root) — та же операция, что и в Protect.
// Без неё control-plane молча не подключался бы, а клиент вечно висел в
// "connecting". Даёт понятную ошибку сразу при старте TUN.
func (p *linuxProtector) verifyPrivileges() error {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, 0)
	if err != nil {
		return fmt.Errorf("tun: socket self-test: %w", err)
	}
	defer unix.Close(fd)
	if err := unix.SetsockoptString(fd, unix.SOL_SOCKET, unix.SO_BINDTODEVICE, p.dev); err != nil {
		return fmt.Errorf("режим TUN требует root или CAP_NET_ADMIN+CAP_NET_RAW: %w", err)
	}
	return nil
}

// linuxProtector привязывает control-plane сокеты клиента (WSS/REST/DNS к доске)
// к физическому интерфейсу через SO_BINDTODEVICE, чтобы они шли мимо TUN. Без
// этого трафик к доске сам заходил бы в туннель, зависящий от этого же трафика —
// бесконечная петля. Требует root/CAP_NET_ADMIN.
type linuxProtector struct {
	dev string // физический интерфейс
	dns string // upstream-резолвер, достижимый через dev
}

// Protect вызывается netprotect до connect для каждого сокета. Возврат false
// прерывает соединение (защита обязательна — иначе петля).
func (p *linuxProtector) Protect(fd int) bool {
	if p.dev == "" {
		return false
	}
	if err := unix.SetsockoptString(fd, unix.SOL_SOCKET, unix.SO_BINDTODEVICE, p.dev); err != nil {
		return false
	}
	return true
}

// DNSAddress отдаёт netprotect реальный upstream-резолвер: системный
// /etc/resolv.conf на время туннеля указывает на резолвер за TUN, поэтому
// собственный DNS-резолв клиента нужно направить в обход, на физический
// интерфейс.
func (p *linuxProtector) DNSAddress() string { return p.dns }
