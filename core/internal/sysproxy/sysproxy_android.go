//go:build android

package sysproxy

import "errors"

var errUnsupported = errors.New("sysproxy: Android uses VpnService; system proxy is unsupported")

func Set(string) error { return errUnsupported }
func Unset() error     { return nil }
