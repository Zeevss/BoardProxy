package tun

import (
	"fmt"
	"net"
)

// interfaceIndex возвращает системный индекс интерфейса по имени. Нужен для
// привязки сокетов к физическому интерфейсу (IP_BOUND_IF на macOS,
// IP_UNICAST_IF на Windows).
func interfaceIndex(name string) (int, error) {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return 0, fmt.Errorf("tun: interface %q: %w", name, err)
	}
	return iface.Index, nil
}

// prefixToMask переводит длину префикса IPv4 в маску вида 255.255.255.0.
func prefixToMask(prefix int) string {
	m := net.CIDRMask(prefix, 32)
	if len(m) != 4 {
		return "255.255.255.0"
	}
	return fmt.Sprintf("%d.%d.%d.%d", m[0], m[1], m[2], m[3])
}
