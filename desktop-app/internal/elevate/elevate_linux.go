//go:build linux

package elevate

import (
	"fmt"
	"os/exec"
)

// elevateCommand использует pkexec (polkit): показывает графический диалог
// аутентификации и запускает helper от root, сохраняя переданные аргументы и
// файловые дескрипторы (loopback-сокет helper открывает сам по EventAddr).
func elevateCommand(exe string, args []string) (*exec.Cmd, error) {
	if _, err := exec.LookPath("pkexec"); err != nil {
		return nil, fmt.Errorf("elevate: pkexec не найден (установите polkit): %w", err)
	}
	return exec.Command("pkexec", append([]string{exe}, args...)...), nil
}
