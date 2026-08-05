// Command bproxy — единый бинарник BoardProxy: клиент (`connect`), сервер
// (`serve`) и управление запущенным сервером через его локальный сокет
// (`clients`, `boards`, `restart`).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"text/tabwriter"
	"time"

	"bproxy-core/internal/app"
	"bproxy-core/internal/clientconfig"
	"bproxy-core/internal/config"
	"bproxy-core/internal/logging"
	"bproxy-core/internal/mgmt"
	"bproxy-core/pkg/bproxy"

	"github.com/spf13/cobra"
)

// version подставляется при сборке через -ldflags "-X main.version=…".
var version = "dev"

func main() {
	var socket string
	root := &cobra.Command{
		Use:     "bproxy",
		Short:   "SOCKS5/HTTP proxy tunneled over an online whiteboard",
		Version: version,
	}
	root.PersistentFlags().StringVar(&socket, "socket",
		getenvOr("BPROXY_SOCKET", "/tmp/bproxy.sock"), "management socket path of the running server")

	root.AddCommand(connectCmd(), serveCmd(&socket), clientsCmd(&socket), boardsCmd(&socket), restartCmd(&socket))

	if err := root.ExecuteContext(context.Background()); err != nil {
		if !errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	}
}

func connectCmd() *cobra.Command {
	var (
		link        string
		listen      string
		configPath  string
		bypassList  []string
		debug       bool
		localDNS    bool
		systemProxy bool
		enableUDP   bool
		maxLanes    = config.Default().Client.MaxLanes
	)
	cmd := &cobra.Command{
		Use:   "connect [config.toml]",
		Short: "run the local proxy, tunneling over the board",
		Long: "run the local proxy, tunneling over the board.\n\n" +
			"Credentials and options come from flags, or from a TOML config file\n" +
			"passed as a positional arg (or --config); flags override the file.",
		Args:    cobra.MaximumNArgs(1),
		Version: version,
		RunE: func(cmd *cobra.Command, args []string) error {
			level := "info"
			if debug {
				level = "debug"
			}
			log := logging.New(level)

			// Путь к конфигу: позиционный аргумент имеет приоритет над --config.
			path := configPath
			if len(args) == 1 {
				path = args[0]
			}

			f := cmd.Flags()
			var bcfg bproxy.Config
			if path != "" {
				var err error
				bcfg, err = clientconfig.Load(path)
				if err != nil {
					return err
				}
				// Флаги переопределяют файл только если заданы явно.
				if f.Changed("link") {
					bcfg.Keylink = link
				}
				if f.Changed("listen") {
					bcfg.Listen = listen
				}
				if f.Changed("local-dns") {
					bcfg.LocalDNS = localDNS
				}
				if f.Changed("system-proxy") {
					bcfg.SystemProxy = systemProxy
				}
				if f.Changed("udp") {
					bcfg.EnableUDP = enableUDP
				}
				if f.Changed("max-lanes") {
					bcfg.MaxLanes = maxLanes
				}
				if f.Changed("bypass") {
					bcfg.BypassList = bypassList
				}
			} else {
				bcfg = bproxy.Config{
					Keylink:     link,
					Listen:      listen,
					LocalDNS:    localDNS,
					SystemProxy: systemProxy,
					EnableUDP:   enableUDP,
					MaxLanes:    maxLanes,
					BypassList:  bypassList,
				}
			}
			bcfg.LogLevel = level
			bcfg.Logger = log

			c := bproxy.New(bcfg)
			c.OnStatus(func(s bproxy.Status, err error) {
				if err != nil {
					log.Error("status", "state", string(s), "err", err)
					return
				}
				log.Info("status", "state", string(s))
			})
			c.OnMetrics(func(m bproxy.Metrics) {
				log.Debug("metrics", "streams", m.Streams, "tx_bytes", m.TotalTx,
					"rx_bytes", m.TotalRx, "tx_bps", m.RateTx, "rx_bps", m.RateRx,
					"confirmed_tx_bps", m.RateConfirmedTx, "backlog_bytes", m.BacklogBytes,
					"blocked_writers", m.BlockedWriters, "rtt_ms", m.RTT.Milliseconds())
			})

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			// При работе с файлом — реактивно перечитываем bypass при его правке.
			if path != "" {
				go watchBypass(ctx, path, c, log)
			}
			return c.Run(ctx)
		},
	}
	f := cmd.Flags()
	f.StringVar(&link, "link", getenvOr("BPROXY_KEYLINK", ""), "bproxy:// connection string (required unless in config)")
	f.StringVar(&listen, "listen", "127.0.0.1:1080", "local SOCKS5/HTTP listen address")
	f.StringVar(&configPath, "config", "", "path to a TOML client config (same as the positional arg)")
	f.BoolVar(&debug, "debug", false, "verbose debug logging")
	f.BoolVar(&localDNS, "local-dns", false, "resolve DNS locally and tunnel IPs (default: the server resolves)")
	f.BoolVar(&systemProxy, "system-proxy", false, "set the OS system proxy while running, restore on exit")
	f.BoolVar(&enableUDP, "udp", false, "enable SOCKS5 UDP ASSOCIATE tunneling")
	f.IntVar(&maxLanes, "max-lanes", getenvIntOr("BPROXY_MAX_LANES", maxLanes),
		"maximum physical lanes per connection (1-32)")
	f.StringSliceVar(&bypassList, "bypass", nil, "comma-separated Go regexps; matching hosts go direct, bypassing the tunnel")
	return cmd
}

// watchBypass следит за файлом конфига и на каждое его изменение перечитывает
// только список bypass, применяя его к работающему клиенту без переподключения
// (реактивное обновление). Поллинг mtime — намеренно без внешней зависимости на
// inotify: список bypass меняют редко, секундная задержка некритична.
func watchBypass(ctx context.Context, path string, c *bproxy.Client, log *slog.Logger) {
	const interval = 2 * time.Second
	var lastMod time.Time
	if fi, err := os.Stat(path); err == nil {
		lastMod = fi.ModTime()
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			fi, err := os.Stat(path)
			if err != nil || !fi.ModTime().After(lastMod) {
				continue
			}
			lastMod = fi.ModTime()
			patterns, err := clientconfig.ReadBypass(path)
			if err != nil {
				log.Warn("bypass reload: parse config", "err", err)
				continue
			}
			if err := c.UpdateBypassList(patterns); err != nil {
				log.Warn("bypass reload: invalid pattern", "err", err)
				continue
			}
			log.Info("bypass list reloaded", "patterns", len(patterns))
		}
	}
}

func serveCmd(socket *string) *cobra.Command {
	cfg := config.Default()
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "run a server instance (hub + egress + management socket)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg.Server.Socket = *socket
			// Сервер копит хвост лога в кольцевой буфер, чтобы web-панель могла
			// показать последние записи (GET /logs).
			log, logs := logging.NewWithBuffer(cfg.LogLevel)
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			// Плавный перезапуск (через управляющий сокет) возвращает ErrRestart —
			// поднимаем сервер заново, заново резолвя доску.
			for {
				err := app.RunServer(ctx, cfg, log, logs)
				if errors.Is(err, app.ErrRestart) {
					log.Info("restarting server")
					continue
				}
				if err != nil && !errors.Is(err, context.Canceled) {
					return err
				}
				return nil
			}
		},
	}
	f := cmd.Flags()
	f.StringVarP(&cfg.Board.Hash, "board", "b", getenvOr("BPROXY_BOARD", ""), "whiteboard hash (empty = board-less start)")
	f.StringVar(&cfg.Store.Path, "db", getenvOr("BPROXY_DB", defaultDBPath()), "SQLite database file path (created if missing)")
	f.StringVar(&cfg.Server.KeyPath, "key-file", getenvOr("BPROXY_KEY_FILE", defaultKeyPath()), "server private key file path (generated next to the binary if missing)")
	f.StringVar(&cfg.Board.APIBase, "api", cfg.Board.APIBase, "board REST API base URL")
	f.StringVar(&cfg.Server.HubPage, "hub", "", "hub slide hash (empty = deterministic)")
	f.DurationVar(&cfg.Server.IdleTimeout, "idle-timeout", cfg.Server.IdleTimeout,
		"release a client page after no events/heartbeats for this duration (0 disables)")
	f.DurationVar(&cfg.Transport.StreamIdleTimeout, "stream-idle-timeout", cfg.Transport.StreamIdleTimeout,
		"reset an individual stream after no traffic for this duration (0 disables)")
	f.IntVar(&cfg.Server.MaxLanes, "max-lanes",
		getenvIntOr("BPROXY_SERVER_MAX_LANES", cfg.Server.MaxLanes),
		"maximum physical lanes accepted per client bundle (1-32)")
	f.IntVar(&cfg.Server.MaxSessionsPerUser, "max-sessions-per-user",
		getenvIntOr("BPROXY_SERVER_MAX_SESSIONS_PER_USER", cfg.Server.MaxSessionsPerUser),
		"maximum independent logical sessions per provisioned user (0 disables the limit)")
	f.BoolVar(&cfg.Server.AllowPrivateEgress, "allow-private-egress",
		getenvBoolOr("BPROXY_ALLOW_PRIVATE_EGRESS", cfg.Server.AllowPrivateEgress),
		"allow clients to reach RFC1918/ULA destinations (loopback and link-local stay blocked)")
	f.StringVar(&cfg.LogLevel, "log", getenvOr("BPROXY_LOG", cfg.LogLevel), "log level: debug|info|warn|error")
	f.StringVar(&cfg.Server.WebAPI, "web-api", getenvOr("WEB_API", ""),
		"HTTP address (host:port) to also expose the management API on, e.g. 127.0.0.1:8080 (empty = disabled)")
	f.StringVar(&cfg.Server.WebAPIToken, "web-api-token", getenvOr("BPROXY_WEB_API_TOKEN", ""),
		"require this bearer token on --web-api requests (recommended if not bound to loopback)")
	f.StringVar(&cfg.Server.WebUIPassword, "web-ui-password", getenvOr("BPROXY_WEB_UI_PASSWORD", ""),
		"password for the web panel login (POST /login issues a session cookie); empty disables password login")
	return cmd
}

// mgmtCtx — короткий контекст для одиночного управляющего вызова к сокету.
func mgmtCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}

func clientsCmd(socket *string) *cobra.Command {
	cmd := &cobra.Command{Use: "clients", Short: "manage clients (provisioned users)"}

	ls := &cobra.Command{
		Use:   "ls",
		Short: "list clients",
		RunE: func(*cobra.Command, []string) error {
			ctx, cancel := mgmtCtx()
			defer cancel()
			clients, err := mgmt.NewClient(*socket).ListClients(ctx)
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tNAME\tSTATUS\tCREATED\tLAST SEEN")
			for _, c := range clients {
				last := "never"
				if c.LastSeen != nil {
					last = c.LastSeen.Format(time.RFC3339)
				}
				fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n", c.ID, c.Name, c.Status,
					c.CreatedAt.Format(time.RFC3339), last)
			}
			return tw.Flush()
		},
	}
	add := &cobra.Command{
		Use:   "add <name>",
		Short: "provision a client and print its keylink",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			ctx, cancel := mgmtCtx()
			defer cancel()
			resp, err := mgmt.NewClient(*socket).AddClient(ctx, args[0])
			if err != nil {
				return err
			}
			fmt.Printf("client #%d %q created\n%s\n", resp.ID, resp.Name, resp.Keylink)
			return nil
		},
	}
	rm := &cobra.Command{
		Use:   "rm <id>",
		Short: "disable a client (revoke access)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid id %q", args[0])
			}
			ctx, cancel := mgmtCtx()
			defer cancel()
			if err := mgmt.NewClient(*socket).RemoveClient(ctx, id); err != nil {
				return err
			}
			fmt.Printf("client #%d disabled\n", id)
			return nil
		},
	}
	info := &cobra.Command{
		Use:   "info <id>",
		Short: "show one client",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid id %q", args[0])
			}
			ctx, cancel := mgmtCtx()
			defer cancel()
			c, err := mgmt.NewClient(*socket).GetClient(ctx, id)
			if err != nil {
				return err
			}
			fmt.Printf("id:         %d\nname:       %s\nstatus:     %s\npublic_key: %s\ncreated:    %s\n",
				c.ID, c.Name, c.Status, c.PublicKey, c.CreatedAt.Format(time.RFC3339))
			if c.LastSeen != nil {
				fmt.Printf("last_seen:  %s\n", c.LastSeen.Format(time.RFC3339))
			}
			return nil
		},
	}
	cmd.AddCommand(ls, add, rm, info)
	return cmd
}

func boardsCmd(socket *string) *cobra.Command {
	cmd := &cobra.Command{Use: "boards", Short: "manage boards (hubs)"}

	ls := &cobra.Command{
		Use:   "ls",
		Short: "list boards",
		RunE: func(*cobra.Command, []string) error {
			ctx, cancel := mgmtCtx()
			defer cancel()
			boards, err := mgmt.NewClient(*socket).ListBoards(ctx)
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "HASH\tNAME\tHUB SLIDE\tSTATUS")
			for _, b := range boards {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", b.ID, b.Name, b.HubSlide, b.Status)
			}
			return tw.Flush()
		},
	}
	var restartAfter bool
	add := &cobra.Command{
		Use:   "add <hash> [name]",
		Short: "register a board",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			name := ""
			if len(args) > 1 {
				name = args[1]
			}
			ctx, cancel := mgmtCtx()
			defer cancel()
			c := mgmt.NewClient(*socket)
			b, err := c.AddBoard(ctx, args[0], name)
			if err != nil {
				return err
			}
			fmt.Printf("board %s registered\n", b.ID)
			// Запущенный сервер обслуживает доску, выбранную на старте; чтобы он
			// подхватил новую, нужен плавный перезапуск.
			if restartAfter {
				if err := c.Restart(ctx); err != nil {
					return fmt.Errorf("restart: %w", err)
				}
				fmt.Println("server restarting to pick up the board")
			} else {
				fmt.Println("run `bproxy restart` (or add -r) to make the server serve it")
			}
			return nil
		},
	}
	add.Flags().BoolVarP(&restartAfter, "restart", "r", false, "gracefully restart the server after adding")
	rm := &cobra.Command{
		Use:   "rm <hash>",
		Short: "disable a board",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			ctx, cancel := mgmtCtx()
			defer cancel()
			if err := mgmt.NewClient(*socket).RemoveBoard(ctx, args[0]); err != nil {
				return err
			}
			fmt.Printf("board %s disabled\n", args[0])
			return nil
		},
	}
	cmd.AddCommand(ls, add, rm)
	return cmd
}

func restartCmd(socket *string) *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "gracefully restart the running server",
		RunE: func(*cobra.Command, []string) error {
			ctx, cancel := mgmtCtx()
			defer cancel()
			if err := mgmt.NewClient(*socket).Restart(ctx); err != nil {
				return err
			}
			fmt.Println("restart requested")
			return nil
		},
	}
}

func getenvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getenvIntOr(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return n
}

func getenvBoolOr(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	b, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return b
}

// defaultKeyPath резолвит путь к файлу приватного ключа сервера рядом с самим
// исполняемым файлом: так ключ переживает перезапуск/переустановку из любого
// рабочего каталога и не зависит от того, откуда запущен бинарник. Если
// os.Executable недоступен (редкий случай на некоторых платформах), откатываемся
// на относительный путь — тот же дефолт, что и в config.Default().
func defaultKeyPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "bproxy.key"
	}
	return filepath.Join(filepath.Dir(exe), "bproxy.key")
}

// defaultDBPath резолвит путь к файлу БД в пользовательском конфиг-каталоге
// (os.UserConfigDir → ~/.config/bproxy на Linux, $XDG_CONFIG_HOME с приоритетом),
// как принято у обычных приложений: не в /tmp (там только эфемерный сокет), не
// требует root (в отличие от /etc, /var/lib для системных демонов). Каталог
// создаётся при открытии БД (sqlite.Open делает MkdirAll). Если конфиг-каталог
// недоступен, откатываемся на относительный путь — дефолт config.Default().
func defaultDBPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "bproxy.db"
	}
	return filepath.Join(dir, "bproxy", "bproxy.db")
}
