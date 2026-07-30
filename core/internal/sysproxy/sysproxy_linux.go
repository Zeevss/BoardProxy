//go:build linux && !android

package sysproxy

import (
	"fmt"
	"net"
	"os/exec"
)

// Set включает системный прокси в GNOME (gsettings), направляя SOCKS/HTTP(S)/FTP
// на addr. На не-GNOME окружениях gsettings обычно отсутствует — Set вернёт
// ошибку, вызывающий её залогирует и продолжит без системного прокси.
func Set(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	cmds := [][]string{
		{"gsettings", "set", "org.gnome.system.proxy", "mode", "manual"},

		// SOCKS5
		{"gsettings", "set", "org.gnome.system.proxy.socks", "host", host},
		{"gsettings", "set", "org.gnome.system.proxy.socks", "port", port},

		// HTTP
		{"gsettings", "set", "org.gnome.system.proxy.http", "host", host},
		{"gsettings", "set", "org.gnome.system.proxy.http", "port", port},
		{"gsettings", "set", "org.gnome.system.proxy.http", "enabled", "true"},

		// HTTPS
		{"gsettings", "set", "org.gnome.system.proxy.https", "host", host},
		{"gsettings", "set", "org.gnome.system.proxy.https", "port", port},

		// FTP
		{"gsettings", "set", "org.gnome.system.proxy.ftp", "host", host},
		{"gsettings", "set", "org.gnome.system.proxy.ftp", "port", port},
	}
	for _, c := range cmds {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			return fmt.Errorf("%v: %w (%s)", c, err, out)
		}
	}
	return nil
}

// Unset возвращает режим прокси GNOME в none.
func Unset() error {
	if out, err := exec.Command("gsettings", "set", "org.gnome.system.proxy", "mode", "none").CombinedOutput(); err != nil {
		return fmt.Errorf("gsettings reset: %w (%s)", err, out)
	}
	return nil
}
