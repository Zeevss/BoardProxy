//go:build windows && amd64

package wintundll

import _ "embed"

//go:embed wintun_amd64.dll
var dllBytes []byte
