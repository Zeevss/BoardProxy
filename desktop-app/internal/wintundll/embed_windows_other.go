//go:build windows && !amd64 && !arm64

package wintundll

// Для прочих архитектур Windows (напр. 386) встроенной DLL нет.
var dllBytes []byte
