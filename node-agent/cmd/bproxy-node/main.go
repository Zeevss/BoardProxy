package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"bproxy-node-agent/internal/agent"
	"bproxy-node-agent/internal/nodeconfig"
)

var version = "dev"

func main() {
	config, err := nodeconfig.Parse(os.Args[1:])
	if err == nil {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		err = agent.Run(ctx, config, version, os.Stdout, os.Stderr, slog.Default())
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
