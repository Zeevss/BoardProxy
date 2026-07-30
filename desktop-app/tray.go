//go:build !bindings

package main

import (
	"sync"

	"fyne.io/systray"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// maxTrayProfiles — сколько слотов профилей заранее создаётся в подменю (systray
// не умеет добавлять пункты после инициализации, поэтому слоты фиксированы и
// показываются/прячутся по мере надобности).
const maxTrayProfiles = 20

// trayController строит и обновляет меню трея. Клики транслируются во фронтенд
// событиями (tray:toggle, tray:selectProfile), состояние приходит из фронтенда
// через App.SyncTray → update.
type trayController struct {
	app *App

	mu           sync.Mutex
	toggleItem   *systray.MenuItem
	profileItems []*systray.MenuItem
	profileIDs   []string // id профиля в каждом слоте (параллельно profileItems)
}

func (t *trayController) emit(name string, data any) {
	if ctx := t.app.runtimeContext(); ctx != nil {
		wruntime.EventsEmit(ctx, name, data)
	}
}

// update применяет снимок состояния: подпись кнопки подключения и список профилей.
func (t *trayController) update(state TrayState) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.toggleItem != nil {
		if isActiveTrayStatus(state.Status) {
			t.toggleItem.SetTitle("Отключить")
		} else {
			t.toggleItem.SetTitle("Подключить")
		}
	}
	for i, item := range t.profileItems {
		if i < len(state.Profiles) {
			p := state.Profiles[i]
			t.profileIDs[i] = p.ID
			item.SetTitle(p.Name)
			if p.ID == state.ActiveID {
				item.Check()
			} else {
				item.Uncheck()
			}
			item.Show()
		} else {
			t.profileIDs[i] = ""
			item.Hide()
		}
	}
}

func isActiveTrayStatus(s string) bool {
	return s == "connected" || s == "connecting" || s == "reconnecting" || s == "stopping"
}

func setupTray(app *App) (start, stop func()) {
	tc := &trayController{app: app}
	rawStart, rawStop := systray.RunWithExternalLoop(func() {
		systray.SetIcon(trayIcon)
		systray.SetTitle("BoardProxy")
		systray.SetTooltip("BoardProxy")

		show := systray.AddMenuItem("Открыть BoardProxy", "Показать главное окно")
		systray.AddSeparator()
		toggle := systray.AddMenuItem("Подключить", "Подключить или отключить туннель")
		profiles := systray.AddMenuItem("Профиль", "Выбрать активный профиль")
		systray.AddSeparator()
		quit := systray.AddMenuItem("Выйти", "Остановить BoardProxy")

		tc.mu.Lock()
		tc.toggleItem = toggle
		tc.profileItems = make([]*systray.MenuItem, maxTrayProfiles)
		tc.profileIDs = make([]string, maxTrayProfiles)
		for i := 0; i < maxTrayProfiles; i++ {
			item := profiles.AddSubMenuItemCheckbox("", "", false)
			item.Hide()
			tc.profileItems[i] = item
			idx := i
			go func() {
				for range item.ClickedCh {
					tc.mu.Lock()
					id := tc.profileIDs[idx]
					tc.mu.Unlock()
					if id != "" {
						tc.emit("tray:selectProfile", map[string]string{"id": id})
					}
				}
			}()
		}
		tc.mu.Unlock()

		app.mu.Lock()
		app.tray = tc
		app.mu.Unlock()

		go func() {
			for {
				select {
				case <-show.ClickedCh:
					app.showWindow()
				case <-toggle.ClickedCh:
					tc.emit("tray:toggle", nil)
				case <-quit.ClickedCh:
					if ctx := app.runtimeContext(); ctx != nil {
						wruntime.Quit(ctx)
					}
					return
				}
			}
		}()
	}, func() {})

	return func() {
			defer func() { _ = recover() }()
			rawStart()
		}, func() {
			defer func() { _ = recover() }()
			rawStop()
		}
}
