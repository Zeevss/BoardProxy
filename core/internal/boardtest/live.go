// Package boardtest holds shared helpers for tests that talk to a real board.
package boardtest

import (
	"os"
	"testing"
)

// APIBase is the REST entry point used by live tests.
const APIBase = "https://boards.yandex.ru/api"

// LiveHash returns the board hash for live tests, or skips the test. Live tests
// run only when BPROXY_LIVE=1 and BPROXY_BOARD is set to a board hash; there is
// no hardcoded board.
func LiveHash(t *testing.T) string {
	t.Helper()
	if os.Getenv("BPROXY_LIVE") != "1" {
		t.Skip("set BPROXY_LIVE=1 to run live board tests")
	}
	hash := os.Getenv("BPROXY_BOARD")
	if hash == "" {
		t.Skip("set BPROXY_BOARD to a board hash to run live board tests")
	}
	return hash
}
