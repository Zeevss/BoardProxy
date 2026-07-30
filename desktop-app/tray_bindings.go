//go:build bindings

package main

func setupTray(*App) (start, stop func()) {
	return func() {}, func() {}
}
