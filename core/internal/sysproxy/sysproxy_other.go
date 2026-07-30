//go:build !windows && !linux && !darwin

package sysproxy

import "errors"

// errUnsupported — на этой платформе автоматическая установка системного прокси
// не реализована; вызывающий залогирует ошибку и продолжит без неё.
var errUnsupported = errors.New("sysproxy: --system-proxy не поддерживается на этой платформе")

func Set(addr string) error { return errUnsupported }

func Unset() error { return nil }
