package bproxy

import "bproxy-core/internal/sysproxy"

// SetSystemProxy прописывает адрес локального прокси (host:port) в системные
// настройки ОС. Вынесено в публичный API, чтобы GUI мог включать системный
// прокси и в режиме TUN, где сам клиент работает в отдельном процессе.
func SetSystemProxy(addr string) error { return sysproxy.Set(addr) }

// UnsetSystemProxy восстанавливает прежние системные настройки прокси.
func UnsetSystemProxy() error { return sysproxy.Unset() }
