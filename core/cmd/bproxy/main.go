// Command bproxy runs the client data plane, the config-driven server, and a
// compact gRPC control client.
package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"
	"time"

	controlapi "bproxy-core/api/control/v1"
	"bproxy-core/internal/app"
	"bproxy-core/internal/clientconfig"
	"bproxy-core/internal/crypto"
	"bproxy-core/internal/logging"
	"bproxy-core/internal/serverconfig"
	"bproxy-core/pkg/bproxy"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/emptypb"
)

var version = "dev"

func main() {
	controlAddress := "unix:///tmp/bproxy-control.sock"
	root := &cobra.Command{
		Use: "bproxy", Short: "SOCKS5/HTTP proxy tunneled over an online whiteboard", Version: version,
	}
	root.PersistentFlags().StringVar(&controlAddress, "control", controlAddress, "gRPC control address of a running server")
	root.AddCommand(connectCmd(), serveCmd(), generateCmd(), reloadCmd(&controlAddress), usersCmd(&controlAddress), boardsCmd(&controlAddress), statsCmd(&controlAddress))
	if err := root.ExecuteContext(context.Background()); err != nil {
		if !errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	}
}

func connectCmd() *cobra.Command {
	var (
		link, listen, configPath string
		bypassList               []string
		debug, localDNS          bool
		systemProxy, enableUDP   bool
		retryInitial             bool
		maxLanes                 = 8
	)
	cmd := &cobra.Command{
		Use: "connect [config.toml]", Short: "run the local proxy", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := configPath
			if len(args) == 1 {
				path = args[0]
			}
			f := cmd.Flags()
			var cfg bproxy.Config
			if path != "" {
				var err error
				cfg, err = clientconfig.Load(path)
				if err != nil {
					return err
				}
				if f.Changed("link") {
					cfg.Keylink = link
				}
				if f.Changed("listen") {
					cfg.Listen = listen
				}
				if f.Changed("local-dns") {
					cfg.LocalDNS = localDNS
				}
				if f.Changed("system-proxy") {
					cfg.SystemProxy = systemProxy
				}
				if f.Changed("udp") {
					cfg.EnableUDP = enableUDP
				}
				if f.Changed("retry-initial") {
					cfg.RetryInitial = retryInitial
				}
				if f.Changed("max-lanes") {
					cfg.MaxLanes = maxLanes
				}
				if f.Changed("bypass") {
					cfg.BypassList = bypassList
				}
			} else {
				cfg = bproxy.Config{Keylink: link, Listen: listen, LocalDNS: localDNS, SystemProxy: systemProxy,
					EnableUDP: enableUDP, RetryInitial: retryInitial, MaxLanes: maxLanes, BypassList: bypassList}
			}
			if debug {
				cfg.LogLevel = "debug"
			}
			if cfg.LogLevel == "" {
				cfg.LogLevel = "info"
			}
			log := logging.New(cfg.LogLevel)
			cfg.Logger = log
			client := bproxy.New(cfg)
			client.OnStatus(func(status bproxy.Status, err error) {
				if err != nil {
					log.Error("status", "state", string(status), "err", err)
					return
				}
				log.Info("status", "state", string(status))
			})
			client.OnMetrics(func(m bproxy.Metrics) {
				log.Debug("metrics", "streams", m.Streams, "tx_bytes", m.TotalTx, "rx_bytes", m.TotalRx,
					"tx_bps", m.RateTx, "rx_bps", m.RateRx, "rtt_ms", m.RTT.Milliseconds())
			})
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			if path != "" {
				go watchBypass(ctx, path, client, log)
			}
			return client.Run(ctx)
		},
	}
	f := cmd.Flags()
	f.StringVar(&link, "link", getenvOr("BPROXY_KEYLINK", ""), "bproxy:// connection string")
	f.StringVar(&listen, "listen", "127.0.0.1:1080", "local SOCKS5/HTTP address")
	f.StringVar(&configPath, "config", "", "path to a TOML client config")
	f.BoolVar(&debug, "debug", false, "verbose logging")
	f.BoolVar(&localDNS, "local-dns", false, "resolve DNS locally")
	f.BoolVar(&systemProxy, "system-proxy", false, "set the OS system proxy while running")
	f.BoolVar(&enableUDP, "udp", false, "enable SOCKS5 UDP ASSOCIATE")
	f.BoolVar(&retryInitial, "retry-initial", false, "retry if the initial board connection fails")
	f.IntVar(&maxLanes, "max-lanes", maxLanes, "maximum physical lanes")
	f.StringSliceVar(&bypassList, "bypass", nil, "host regexps that bypass the tunnel")
	return cmd
}

func watchBypass(ctx context.Context, path string, client *bproxy.Client, log *slog.Logger) {
	var lastMod time.Time
	if info, err := os.Stat(path); err == nil {
		lastMod = info.ModTime()
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			info, err := os.Stat(path)
			if err != nil || !info.ModTime().After(lastMod) {
				continue
			}
			lastMod = info.ModTime()
			patterns, err := clientconfig.ReadBypass(path)
			if err == nil {
				err = client.UpdateBypassList(patterns)
			}
			if err != nil {
				log.Warn("bypass reload rejected", "err", err)
				continue
			}
			log.Info("bypass list reloaded", "patterns", len(patterns))
		}
	}
}

func serveCmd() *cobra.Command {
	configPath := "config.toml"
	testOnly, dump := false, false
	cmd := &cobra.Command{
		Use: "serve [config.toml|stdin:]", Short: "run the stateless config-driven server", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			source := configPath
			if len(args) == 1 {
				source = args[0]
			}
			cfg, err := serverconfig.Load(source, os.Stdin)
			if err != nil {
				return err
			}
			if dump {
				redacted := cfg
				redacted.Server.PrivateKey = "<redacted>"
				for i := range redacted.Users {
					if redacted.Users[i].PrivateKey != "" {
						redacted.Users[i].PrivateKey = "<redacted>"
					}
				}
				return toml.NewEncoder(os.Stdout).Encode(redacted)
			}
			if testOnly {
				fmt.Fprintln(os.Stdout, "configuration OK")
				return nil
			}
			log, logs := logging.NewWithBuffer(cfg.Observability.LogLevel)
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return app.RunServer(ctx, cfg, source, log, logs)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", configPath, "TOML file path or stdin:")
	cmd.Flags().BoolVar(&testOnly, "test", false, "validate the config without starting")
	cmd.Flags().BoolVar(&dump, "dump", false, "print normalized config with secrets redacted")
	return cmd
}

func generateCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "generate", Short: "generate offline configuration secrets"}
	gen := func(kind string) *cobra.Command {
		return &cobra.Command{Use: kind, Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
			kp, err := crypto.Generate()
			if err != nil {
				return err
			}
			fmt.Printf("base64:%s\n", base64.StdEncoding.EncodeToString(kp.Private()))
			return nil
		}}
	}
	cmd.AddCommand(gen("server-key"), gen("user-key"))
	return cmd
}

func reloadCmd(address *string) *cobra.Command {
	return &cobra.Command{Use: "reload", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return withControl(cmd.Context(), *address, func(c controlapi.ControlServiceClient) error {
			resp, err := c.Reload(cmd.Context(), &controlapi.RevisionRequest{})
			if err == nil {
				fmt.Println("revision", resp.Revision)
			}
			return err
		})
	}}
}

func usersCmd(address *string) *cobra.Command {
	cmd := &cobra.Command{Use: "users", Short: "inspect or mutate runtime users over gRPC"}
	cmd.AddCommand(addUserCmd(address))
	cmd.AddCommand(&cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(c *cobra.Command, _ []string) error {
		return withControl(c.Context(), *address, func(client controlapi.ControlServiceClient) error {
			resp, err := client.ListUsers(c.Context(), &emptypb.Empty{})
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "TAG\tNAME\tENABLED\tSESSIONS\tRX\tTX")
			for _, u := range resp.Users {
				fmt.Fprintf(tw, "%s\t%s\t%t\t%d\t%d\t%d\n", u.Tag, u.Name, u.Enabled, u.ActiveSessions, u.RxBytesSinceStart, u.TxBytesSinceStart)
			}
			return tw.Flush()
		})
	}})
	cmd.AddCommand(&cobra.Command{Use: "remove <tag>", Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, args []string) error {
		return withControl(c.Context(), *address, func(client controlapi.ControlServiceClient) error {
			_, err := client.RemoveUser(c.Context(), &controlapi.ResourceRequest{Tag: args[0]})
			return err
		})
	}})
	cmd.AddCommand(setEnabledCmd("enable", func(ctx context.Context, client controlapi.ControlServiceClient, req *controlapi.SetEnabledRequest) error {
		_, err := client.SetUserEnabled(ctx, req)
		return err
	}, address, true))
	cmd.AddCommand(setEnabledCmd("disable", func(ctx context.Context, client controlapi.ControlServiceClient, req *controlapi.SetEnabledRequest) error {
		_, err := client.SetUserEnabled(ctx, req)
		return err
	}, address, false))
	cmd.AddCommand(&cobra.Command{Use: "keylink <tag>", Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, args []string) error {
		return withControl(c.Context(), *address, func(client controlapi.ControlServiceClient) error {
			resp, err := client.GetKeylink(c.Context(), &controlapi.ResourceRequest{Tag: args[0]})
			if err == nil {
				fmt.Println(resp.Keylink)
			}
			return err
		})
	}})
	return cmd
}

func boardsCmd(address *string) *cobra.Command {
	cmd := &cobra.Command{Use: "boards", Short: "inspect or mutate runtime boards over gRPC"}
	cmd.AddCommand(addBoardCmd(address))
	cmd.AddCommand(&cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(c *cobra.Command, _ []string) error {
		return withControl(c.Context(), *address, func(client controlapi.ControlServiceClient) error {
			resp, err := client.ListBoards(c.Context(), &emptypb.Empty{})
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "TAG\tNAME\tHASH\tENABLED\tSTATE\tERROR")
			for _, b := range resp.Boards {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%t\t%s\t%s\n", b.Config.Tag, b.Config.Name, b.Config.Hash,
					b.Config.GetEnabled(), b.State, b.Error)
			}
			return tw.Flush()
		})
	}})
	cmd.AddCommand(&cobra.Command{Use: "remove <tag>", Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, args []string) error {
		return withControl(c.Context(), *address, func(client controlapi.ControlServiceClient) error {
			_, err := client.RemoveBoard(c.Context(), &controlapi.ResourceRequest{Tag: args[0]})
			return err
		})
	}})
	cmd.AddCommand(setEnabledCmd("enable", func(ctx context.Context, client controlapi.ControlServiceClient, req *controlapi.SetEnabledRequest) error {
		_, err := client.SetBoardEnabled(ctx, req)
		return err
	}, address, true))
	cmd.AddCommand(setEnabledCmd("disable", func(ctx context.Context, client controlapi.ControlServiceClient, req *controlapi.SetEnabledRequest) error {
		_, err := client.SetBoardEnabled(ctx, req)
		return err
	}, address, false))
	return cmd
}

func addUserCmd(address *string) *cobra.Command {
	var (
		name, privateKey, publicKey string
		boards                      []string
		maxSessions, maxLanes       int
		disabled                    bool
		revision                    uint64
	)
	cmd := &cobra.Command{
		Use: "add <tag>", Short: "add a user to the live runtime", Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if name == "" {
				name = args[0]
			}
			if (privateKey == "") == (publicKey == "") {
				return errors.New("set exactly one of --private-key or --public-key")
			}
			if len(boards) == 0 {
				return errors.New("at least one --boards value is required")
			}
			enabled := !disabled
			return withControl(c.Context(), *address, func(client controlapi.ControlServiceClient) error {
				result, err := client.AddUser(c.Context(), &controlapi.AddUserRequest{
					ExpectedRevision: revision,
					User: &controlapi.UserSpec{
						Tag: args[0], Name: name, PrivateKey: privateKey, PublicKey: publicKey,
						Enabled: &enabled, Boards: boards, MaxSessions: int32(maxSessions), MaxLanes: int32(maxLanes),
					},
				})
				if err != nil {
					return err
				}
				printRuntimeOnlyWarning(c.Context(), client, "user", args[0], result.Revision)
				return nil
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&name, "name", "", "display name (defaults to tag)")
	f.StringVar(&privateKey, "private-key", "", "base64:<32-byte-user-private-key>")
	f.StringVar(&publicKey, "public-key", "", "migration-only base64:<32-byte-user-public-key>")
	f.StringSliceVar(&boards, "boards", nil, "allowed board tags")
	f.IntVar(&maxSessions, "max-sessions", 0, "maximum simultaneous sessions; 0 is unlimited")
	f.IntVar(&maxLanes, "max-lanes", 1, "maximum lanes per session (1..32)")
	f.BoolVar(&disabled, "disabled", false, "create the user disabled")
	f.Uint64Var(&revision, "revision", 0, "expected runtime revision; 0 uses the current revision")
	return cmd
}

func addBoardCmd(address *string) *cobra.Command {
	var (
		name, hash, hubSlide, apiBase, guestName string
		maxLanes                                 int
		disabled                                 bool
		revision                                 uint64
	)
	cmd := &cobra.Command{
		Use: "add <tag>", Short: "add a board to the live runtime", Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if name == "" {
				name = args[0]
			}
			if hash == "" {
				return errors.New("--hash is required")
			}
			enabled := !disabled
			return withControl(c.Context(), *address, func(client controlapi.ControlServiceClient) error {
				result, err := client.AddBoard(c.Context(), &controlapi.AddBoardRequest{
					ExpectedRevision: revision,
					Board: &controlapi.BoardSpec{
						Tag: args[0], Name: name, Hash: hash, HubSlide: hubSlide, ApiBase: apiBase,
						GuestName: guestName, Enabled: &enabled, MaxLanes: int32(maxLanes),
					},
				})
				if err != nil {
					return err
				}
				printRuntimeOnlyWarning(c.Context(), client, "board", args[0], result.Revision)
				return nil
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&name, "name", "", "display name (defaults to tag)")
	f.StringVar(&hash, "hash", "", "whiteboard hash")
	f.StringVar(&hubSlide, "hub-slide", "", "fixed hub slide; empty uses deterministic discovery")
	f.StringVar(&apiBase, "api-base", "", "board REST API base URL")
	f.StringVar(&guestName, "guest-name", "", "board guest display-name prefix")
	f.IntVar(&maxLanes, "max-lanes", 1, "maximum lanes per session (1..32)")
	f.BoolVar(&disabled, "disabled", false, "create the board disabled")
	f.Uint64Var(&revision, "revision", 0, "expected runtime revision; 0 uses the current revision")
	return cmd
}

func printRuntimeOnlyWarning(ctx context.Context, client controlapi.ControlServiceClient, kind, tag string, revision uint64) {
	source := "the durable desired-state config"
	if runtime, err := client.GetRuntime(ctx, &emptypb.Empty{}); err == nil {
		if runtime.ConfigSource == "stdin:" || runtime.ConfigSource == "-" {
			source = "the hub/source that generated stdin"
		} else if runtime.ConfigSource != "" {
			source = runtime.ConfigSource
		}
	}
	fmt.Printf("added %s %q at runtime revision %d\n", kind, tag, revision)
	fmt.Fprintf(os.Stderr, "warning: runtime-only change; add this %s to %s or reload/restart will remove it\n", kind, source)
}

func setEnabledCmd(name string, call func(context.Context, controlapi.ControlServiceClient, *controlapi.SetEnabledRequest) error,
	address *string, enabled bool,
) *cobra.Command {
	return &cobra.Command{Use: name + " <tag>", Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, args []string) error {
		return withControl(c.Context(), *address, func(client controlapi.ControlServiceClient) error {
			return call(c.Context(), client, &controlapi.SetEnabledRequest{Tag: args[0], Enabled: enabled})
		})
	}}
}

func statsCmd(address *string) *cobra.Command {
	return &cobra.Command{Use: "stats", Args: cobra.NoArgs, RunE: func(c *cobra.Command, _ []string) error {
		return withControl(c.Context(), *address, func(client controlapi.ControlServiceClient) error {
			resp, err := client.GetStats(c.Context(), &emptypb.Empty{})
			if err != nil {
				return err
			}
			raw, err := protojson.MarshalOptions{Indent: "  "}.Marshal(resp)
			if err == nil {
				fmt.Println(string(raw))
			}
			return err
		})
	}}
}

func withControl(ctx context.Context, address string, fn func(controlapi.ControlServiceClient) error) error {
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()
	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return fn(controlapi.NewControlServiceClient(&contextClientConn{ClientConnInterface: conn, ctx: callCtx}))
}

// contextClientConn applies the command timeout even when a Cobra callback
// accidentally passes its longer-lived parent context.
type contextClientConn struct {
	grpc.ClientConnInterface
	ctx context.Context
}

func (c *contextClientConn) Invoke(_ context.Context, method string, args, reply any, opts ...grpc.CallOption) error {
	return c.ClientConnInterface.Invoke(c.ctx, method, args, reply, opts...)
}
func (c *contextClientConn) NewStream(_ context.Context, desc *grpc.StreamDesc, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	return c.ClientConnInterface.NewStream(c.ctx, desc, method, opts...)
}

func getenvOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
