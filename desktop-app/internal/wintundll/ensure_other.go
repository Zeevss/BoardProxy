//go:build !windows

package wintundll

// Ensure — на не-Windows платформах wintun не используется (no-op).
func Ensure() error { return nil }
