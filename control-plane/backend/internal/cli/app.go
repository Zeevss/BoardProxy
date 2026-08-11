package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"bproxy-control-plane/internal/adapters/filesystem"
	"bproxy-control-plane/internal/adapters/grpcapi"
	"bproxy-control-plane/internal/application"
	"bproxy-control-plane/internal/bootstrap"
)

type App struct {
	Version string
	Stdin   io.Reader
	Stdout  io.Writer
	Logger  *slog.Logger
}

func (a App) Run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: bproxy-hub <serve|token|config|catalog|version>")
	}
	switch args[0] {
	case "serve":
		return a.serve(args[1:])
	case "token":
		return a.createToken(args[1:])
	case "config":
		return a.publishConfig(args[1:])
	case "catalog":
		return a.catalog(args[1:])
	case "version":
		_, err := fmt.Fprintln(a.Stdout, a.Version)
		return err
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func (a App) serve(args []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	data := flags.String("data", "/var/lib/bproxy-hub", "persistent hub data directory")
	listen := flags.String("listen", ":8443", "mTLS gRPC listen address")
	serverNames := flags.String("server-names", "localhost", "comma-separated TLS DNS names or IPs")
	if err := flags.Parse(args); err != nil {
		return err
	}
	container, err := bootstrap.Build(*data, split(*serverNames))
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go reconcileCatalogs(ctx, container.Catalogs, a.Logger)
	return grpcapi.Serve(
		ctx, *listen,
		container.Repository, container.Repository, container.Repository, container.Repository,
		container.Events, container.Authority, a.Logger,
	)
}

func (a App) createToken(args []string) error {
	flags := flag.NewFlagSet("token", flag.ContinueOnError)
	data := flags.String("data", "/var/lib/bproxy-hub", "persistent hub data directory")
	nodeID := flags.String("node", "", "node id")
	hubURL := flags.String("hub-url", "", "public hub gRPC URL")
	serverNames := flags.String("server-names", "localhost", "comma-separated TLS DNS names or IPs")
	ttl := flags.Duration("ttl", 15*time.Minute, "one-time enrollment token lifetime")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *nodeID == "" || *hubURL == "" {
		return errors.New("--node and --hub-url are required")
	}
	container, err := bootstrap.Build(*data, split(*serverNames))
	if err != nil {
		return err
	}
	secret, err := container.Enrollment.IssueBootstrap(context.Background(), *nodeID, *hubURL, *ttl)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(a.Stdout, "BPROXY_NODE_SECRET="+secret)
	return err
}

func (a App) publishConfig(args []string) error {
	flags := flag.NewFlagSet("config", flag.ContinueOnError)
	data := flags.String("data", "/var/lib/bproxy-hub", "persistent hub data directory")
	nodeID := flags.String("node", "", "node id")
	path := flags.String("file", "", "core config.toml path or - for stdin")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *nodeID == "" || *path == "" {
		return errors.New("--node and --file are required")
	}
	config, err := readInput(*path, a.Stdin)
	if err != nil {
		return err
	}
	repository, err := filesystem.Open(*data)
	if err != nil {
		return err
	}
	desired, err := application.NewDesiredStates(repository, nil).PublishNext(
		context.Background(), *nodeID, 0, config, "cli.config",
	)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(a.Stdout, "node %s desired revision %d sha256 %s\n", *nodeID, desired.Revision, desired.ConfigSHA256)
	if err == nil {
		_, err = fmt.Fprintln(a.Stdout, "warning: raw config bypasses the normalized catalog; use 'catalog seed' and resource mutations for managed nodes")
	}
	return err
}

func readInput(path string, stdin io.Reader) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(stdin)
	}
	return os.ReadFile(path)
}

func split(raw string) []string {
	var values []string
	for _, value := range strings.Split(raw, ",") {
		if value = strings.TrimSpace(value); value != "" {
			values = append(values, value)
		}
	}
	return values
}
