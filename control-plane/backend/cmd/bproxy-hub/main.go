package main

import (
	"fmt"
	"log/slog"
	"os"

	"bproxy-control-plane/internal/cli"
)

var version = "dev"

func main() {
	app := cli.App{Version: version, Stdin: os.Stdin, Stdout: os.Stdout, Logger: slog.Default()}
	if err := app.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
