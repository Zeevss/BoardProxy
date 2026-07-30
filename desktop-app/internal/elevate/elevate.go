// Пакет elevate запускает helper-бинарь с повышением привилегий, показывая
// системный диалог аутентификации (polkit на Linux, диалог администратора на
// macOS, UAC на Windows). Сам обмен данными идёт по loopback-сокету (см.
// helperipc), поэтому наследование stdio через границу elevation не требуется.
package elevate

import "os/exec"

// Launch запускает exe с аргументами args и запросом прав администратора.
// Возвращает уже запущенный процесс: его завершение (в т.ч. отказ пользователя
// в диалоге) наблюдается через cmd.Wait. Конкретная команда — платформенная
// (см. elevate_*.go).
func Launch(exe string, args ...string) (*exec.Cmd, error) {
	cmd, err := elevateCommand(exe, args)
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}
