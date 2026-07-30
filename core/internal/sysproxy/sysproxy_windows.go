//go:build windows

package sysproxy

import (
	"encoding/json"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const regPath = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

const (
	internetOptionSettingsChanged = 39
	internetOptionRefresh         = 37
)

var (
	wininet               = windows.NewLazySystemDLL("wininet.dll")
	procInternetSetOption = wininet.NewProc("InternetSetOptionW")
)

// proxyState — прежнее состояние прокси, которое Unset обязан вернуть.
type proxyState struct {
	Enable    uint32 `json:"enable"`
	Server    string `json:"server"`
	HadServer bool   `json:"had_server"` // существовал ли ProxyServer до нас
}

// Прежнее состояние храним не в памяти процесса, а в файле-бэкапе: только так
// оно переживает аварийное завершение (закрытое окно консоли, kill, краш),
// когда Unset не успевает отработать. Иначе следующий запуск принял бы наш
// собственный прокси за «оригинал» и уже никогда не вернул бы настоящий.

// Set включает системный прокси в реестре Windows (per-user) на addr. Прежнее
// состояние сохраняется в файл-бэкап РОВНО ОДИН РАЗ: если файл уже есть (прошлый
// запуск не убрался за собой), считаем записанное в нём истинно исходным и не
// перезахватываем.
func Set(addr string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, regPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	if !backupExists() {
		if err := saveBackup(captureState(k)); err != nil {
			return err
		}
	}

	if err := k.SetDWordValue("ProxyEnable", 1); err != nil {
		return err
	}
	if err := k.SetStringValue("ProxyServer", addr); err != nil {
		return err
	}
	refresh()
	return nil
}

// Unset восстанавливает состояние прокси из файла-бэкапа и удаляет файл. Без
// бэкапа (Set не вызывался или уже убрано) — ничего не делает.
func Unset() error {
	st, ok := loadBackup()
	if !ok {
		return nil
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, regPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	if err := k.SetDWordValue("ProxyEnable", st.Enable); err != nil {
		return err
	}
	if st.HadServer {
		// Был прежний адрес — возвращаем его.
		if err := k.SetStringValue("ProxyServer", st.Server); err != nil {
			return err
		}
	} else {
		// Прокси-сервера до нас не было — убираем свой, а не оставляем его.
		_ = k.DeleteValue("ProxyServer")
	}
	refresh()
	_ = os.Remove(backupPath())
	return nil
}

// captureState снимает текущее состояние прокси из реестра.
func captureState(k registry.Key) proxyState {
	var st proxyState
	if v, _, err := k.GetIntegerValue("ProxyEnable"); err == nil {
		st.Enable = uint32(v)
	}
	if s, _, err := k.GetStringValue("ProxyServer"); err == nil {
		st.Server = s
		st.HadServer = true
	}
	return st
}

// backupPath — путь к файлу-бэкапу в пользовательском конфиг-каталоге
// (%AppData%\bproxy на Windows), рядом с прочим состоянием клиента.
func backupPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "bproxy", "winproxy-backup.json")
}

func backupExists() bool {
	_, err := os.Stat(backupPath())
	return err == nil
}

func loadBackup() (proxyState, bool) {
	data, err := os.ReadFile(backupPath())
	if err != nil {
		return proxyState{}, false
	}
	var st proxyState
	if err := json.Unmarshal(data, &st); err != nil {
		return proxyState{}, false
	}
	return st, true
}

func saveBackup(st proxyState) error {
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(backupPath()), 0o755); err != nil {
		return err
	}
	return os.WriteFile(backupPath(), data, 0o600)
}

func refresh() {
	_, _, _ = procInternetSetOption.Call(0, internetOptionSettingsChanged, 0, 0)
	_, _, _ = procInternetSetOption.Call(0, internetOptionRefresh, 0, 0)
}
