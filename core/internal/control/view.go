package control

import "bproxy-core/internal/serverconfig"

type BoardView struct {
	Config serverconfig.Board
	State  string
	Error  string
}
