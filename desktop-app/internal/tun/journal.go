package tun

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// journal — «страховка» на случай аварийного завершения helper'а (kill -9, паника,
// выключение питания). Перед изменением системных настроек мы записываем на диск
// то, что нужно вернуть; после успешного отката файл удаляется. При следующем
// старте оставшийся файл означает, что прошлый раз откат не отработал, — тогда
// настройки восстанавливаются до подъёма нового туннеля. Без этого система может
// навсегда остаться с нашим DNS/маршрутами.
type journal struct {
	// DNSService — сетевой сервис macOS, которому меняли резолверы.
	DNSService string `json:"dnsService,omitempty"`
	// DNSBackup — прежние резолверы (для networksetup: список через пробел либо
	// "empty"); на Linux — прежнее содержимое resolv.conf.
	DNSBackup string `json:"dnsBackup,omitempty"`
	// ResolvLink — если /etc/resolv.conf был симлинком, куда он указывал.
	ResolvLink string `json:"resolvLink,omitempty"`
	// TunName — имя интерфейса, чьи маршруты нужно снять.
	TunName string `json:"tunName,omitempty"`
	// SplitRoutes — были ли добавлены split-маршруты (0/1 и 128/1) на macOS.
	SplitRoutes bool `json:"splitRoutes,omitempty"`
	// DefaultRoute — был ли добавлен default через TUN (Linux/Windows).
	DefaultRoute bool `json:"defaultRoute,omitempty"`
}

// journalPath — файл состояния. Кладём рядом с системными временными файлами,
// доступными только root (helper работает от администратора).
func journalPath() string {
	dir := "/var/run"
	if _, err := os.Stat(dir); err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "boardproxy-tun-state.json")
}

// save записывает состояние отката на диск.
func (j *journal) save() {
	data, err := json.Marshal(j)
	if err != nil {
		return
	}
	_ = os.WriteFile(journalPath(), data, 0o600)
}

// clear удаляет файл состояния (откат прошёл штатно).
func clearJournal() { _ = os.Remove(journalPath()) }

// loadJournal читает оставшееся с прошлого запуска состояние. Возвращает nil,
// если файла нет.
func loadJournal() *journal {
	data, err := os.ReadFile(journalPath())
	if err != nil {
		return nil
	}
	var j journal
	if json.Unmarshal(data, &j) != nil {
		return nil
	}
	return &j
}
