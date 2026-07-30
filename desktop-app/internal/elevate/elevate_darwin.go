//go:build darwin

package elevate

import (
	"fmt"
	"os/exec"
	"strings"
)

// elevateCommand использует osascript: «do shell script … with administrator
// privileges» показывает системный диалог ввода пароля администратора и
// запускает helper от root. Команда блокируется до завершения helper'а —
// наблюдаем это через cmd.Wait в вызывающем коде.
func elevateCommand(exe string, args []string) (*exec.Cmd, error) {
	// Каждый аргумент оборачиваем в одинарные кавычки для shell, затем экранируем
	// строку под AppleScript (двойные кавычки и обратные слэши).
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, shQuote(exe))
	for _, a := range args {
		parts = append(parts, shQuote(a))
	}
	shellCmd := strings.Join(parts, " ")
	script := fmt.Sprintf("do shell script %s with administrator privileges", asQuote(shellCmd))
	return exec.Command("osascript", "-e", script), nil
}

// shQuote заключает строку в одинарные кавычки для POSIX-shell.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// asQuote заключает строку в двойные кавычки для AppleScript-литерала.
func asQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
