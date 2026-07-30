//go:build windows

// Пакет wintundll поставляет wintun.dll внутри самого бинаря. Пакет
// golang.zx2c4.com/wintun грузит DLL через LoadLibraryEx с флагами
// APPLICATION_DIR|SYSTEM32, поэтому wintun.dll обязана оказаться в каталоге exe
// (или в System32). Мы встраиваем DLL в бинарь и распаковываем её рядом с exe
// (helper работает от администратора, запись в каталог приложения доступна).
package wintundll

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Ensure распаковывает встроенную wintun.dll рядом с исполняемым файлом, если её
// там ещё нет (или размер не совпадает). Вызывать до создания TUN-устройства.
func Ensure() error {
	if len(dllBytes) == 0 {
		return errors.New("wintun: встроенная DLL недоступна для этой архитектуры")
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("wintun: определение пути exe: %w", err)
	}
	target := filepath.Join(filepath.Dir(exe), "wintun.dll")
	if fi, err := os.Stat(target); err == nil && fi.Size() == int64(len(dllBytes)) {
		return nil // уже на месте
	}
	// Пишем во временный файл в том же каталоге и атомарно переименовываем.
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, dllBytes, 0o644); err != nil {
		return fmt.Errorf("wintun: запись DLL рядом с приложением: %w", err)
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("wintun: установка DLL: %w", err)
	}
	return nil
}
