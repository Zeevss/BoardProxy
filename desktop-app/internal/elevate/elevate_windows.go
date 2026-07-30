//go:build windows

package elevate

import (
	"fmt"
	"os/exec"
	"strings"
)

// elevateCommand через PowerShell Start-Process -Verb RunAs показывает диалог
// UAC и запускает helper с правами администратора. Start-Process не наследует
// stdio за границу elevation, поэтому обмен идёт по loopback-сокету (helper
// подключается обратно по EventAddr). Отказ в UAC роняет PowerShell с ошибкой —
// вызывающий распознаёт это как отсутствие подключения helper'а в таймаут.
func elevateCommand(exe string, args []string) (*exec.Cmd, error) {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = psQuote(a)
	}
	inner := fmt.Sprintf(
		"Start-Process -FilePath %s -ArgumentList %s -Verb RunAs -WindowStyle Hidden",
		psQuote(exe), strings.Join(quoted, ","),
	)
	return exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", inner), nil
}

// psQuote заключает строку в одинарные кавычки PowerShell (внутренняя ' удваивается).
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
