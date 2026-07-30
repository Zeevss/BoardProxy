//go:build darwin && !bindings

package main

// На macOS fyne.io/systray конфликтует с wails при линковке (оба определяют
// Objective-C класс AppDelegate → duplicate symbol). Поэтому системный трей на
// маке пока отключён: setupTray — заглушка, App.tray остаётся nil и SyncTray
// ничего не делает. Окно закрывается в Dock; клик по иконке в Dock его вернёт.
func setupTray(*App) (start, stop func()) {
	return func() {}, func() {}
}
