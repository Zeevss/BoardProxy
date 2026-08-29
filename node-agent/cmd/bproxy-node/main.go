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
	"bproxy-node-agent/internal/identity"
	"bproxy-node-agent/internal/nodeconfig"
)

var version = "dev"

func main() {
	config, err := nodeconfig.Parse(os.Args[1:])
	switch {
	case err != nil:
	case config.ResetIdentity:
		// Разовая команда: секрет для неё не нужен, поэтому идёт до Run.
		err = resetIdentity(config.DataDirectory)
	default:
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		err = agent.Run(ctx, config, version, os.Stdout, os.Stderr, slog.Default())
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// resetIdentity стирает сохранённый бандл. Конфигурация ядра и недоставленная
// телеметрия в том же каталоге остаются — ради них команда и существует вместо
// удаления тома целиком.
func resetIdentity(dataDirectory string) error {
	removed, err := identity.Reset(dataDirectory)
	if err != nil {
		return err
	}
	if removed {
		fmt.Println("identity: сохранённая идентичность удалена; при следующем запуске нода зарегистрируется заново")
	} else {
		fmt.Println("identity: сохранённой идентичности не было — сбрасывать нечего")
	}
	return nil
}
