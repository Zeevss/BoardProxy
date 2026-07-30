//go:build !bindings

package main

import (
	"sync"

	"github.com/energye/systray"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Используется форк energye/systray, а не fyne.io/systray: на macOS последний
// объявляет Objective-C класс AppDelegate, который уже есть в wails, и линковка
// падает с "duplicate symbol _OBJC_METACLASS_$_AppDelegate". Форк называет свой
// класс SystrayAppDelegate и вдобавок умеет раздельные обработчики левого и
// правого клика по иконке.

// maxTrayProfiles — сколько слотов профилей заранее создаётся в подменю (пункты
// нельзя добавлять после инициализации, поэтому слоты фиксированы и
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
		systray.SetTooltip("BoardProxy")

		// Левый клик по иконке — открыть окно; правый — системное меню.
		systray.SetOnClick(func(systray.IMenu) { app.showWindow() })
		systray.SetOnRClick(func(menu systray.IMenu) {
			if menu != nil {
				_ = menu.ShowMenu()
			}
		})

		show := systray.AddMenuItem("Открыть BoardProxy", "Показать главное окно")
		show.Click(func() { app.showWindow() })

		systray.AddSeparator()

		toggle := systray.AddMenuItem("Подключить", "Подключить или отключить туннель")
		toggle.Click(func() { tc.emit("tray:toggle", nil) })

		profiles := systray.AddMenuItem("Профиль", "Выбрать активный профиль")

		systray.AddSeparator()

		quit := systray.AddMenuItem("Выйти", "Остановить BoardProxy")
		quit.Click(func() {
			if ctx := app.runtimeContext(); ctx != nil {
				wruntime.Quit(ctx)
			}
		})

		tc.mu.Lock()
		tc.toggleItem = toggle
		tc.profileItems = make([]*systray.MenuItem, maxTrayProfiles)
		tc.profileIDs = make([]string, maxTrayProfiles)
		for i := 0; i < maxTrayProfiles; i++ {
			item := profiles.AddSubMenuItemCheckbox("", "", false)
			item.Hide()
			tc.profileItems[i] = item
			idx := i
			item.Click(func() {
				tc.mu.Lock()
				id := tc.profileIDs[idx]
				tc.mu.Unlock()
				if id != "" {
					tc.emit("tray:selectProfile", map[string]string{"id": id})
				}
			})
		}
		tc.mu.Unlock()

		app.mu.Lock()
		app.tray = tc
		app.mu.Unlock()
	}, func() {})

	return func() {
			defer func() { _ = recover() }()
			rawStart()
		}, func() {
			defer func() { _ = recover() }()
			rawStop()
		}
}
