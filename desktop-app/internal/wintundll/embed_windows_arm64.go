//go:build windows && arm64

package wintundll

import _ "embed"

//go:embed wintun_arm64.dll
var dllBytes []byte
