# BoardProxy Desktop

Wails v2 desktop client based on the UI and state structure of the ICMP
reference application.

The BoardProxy core runs inside the application process. Closing the window
hides it; the connection continues while the tray process remains active.
Choosing **Выйти** in the tray stops the client gracefully and terminates the
application.

## Development

```bash
cd desktop-app
pnpm --dir frontend install --store-dir frontend/.pnpm-store
wails dev
```

## Production build

```bash
cd desktop-app
CI=true wails build -clean
```

The Linux build uses the `webkit2_41` tag because the current development
environment provides WebKitGTK 4.1. The binary is written to:

```text
build/bin/boardproxy
```

## Logging

Every log line and status change is printed to the binary's stdout in addition
to the in-app Logs screen, so running from a terminal shows what is happening:

```bash
./build/bin/boardproxy
```

Helper-side logs are included: they travel to the GUI over the socket and go
through the same path. Metrics are deliberately not printed (once per second).

## Modes: proxy and TUN

The client supports two routing modes, chosen in **Proxy settings → Режим
работы**:

- **Локальный прокси** — a local SOCKS5/HTTP endpoint on `listenAddr:port`,
  optionally installed as the OS system proxy. The default.
- **TUN-туннель (VPN)** — a full tunnel: a virtual network interface captures
  all OS traffic and forwards it through the board via the local SOCKS proxy
  (the desktop analog of the Android VpnService path). Implemented with
  [`tun2socks/v2`](https://github.com/xjasonlyu/tun2socks) (WireGuard TUN device
  + gVisor netstack) in `internal/tun`.

### TUN privileges — the privileged helper (same binary)

TUN mode creates a network interface and edits system routes/DNS, which needs
root/Administrator. Rather than running the whole GUI elevated, the dataplane
runs as a **privileged helper** — the **same executable** re-launched as
`boardproxy --helper <bootstrap>` (no second file to ship). The GUI spawns it on
demand with a **system elevation dialog**:

- **Linux** — `pkexec` (polkit graphical prompt). Requires `pkexec`/polkit.
- **macOS** — `osascript … with administrator privileges` (admin password
  dialog).
- **Windows** — `powershell Start-Process -Verb RunAs` (UAC prompt). `wintun.dll`
  is embedded in the binary (`internal/wintundll`) and the elevated helper
  unpacks it next to the `.exe` on first TUN use (the Go wintun loader searches
  only the application dir and System32). Nothing extra to ship — one `.exe`.

The helper is launched **once per GUI session** and reused: a connect sends a
`start` command, a disconnect sends `stop` (the process stays alive), so the
elevation dialog appears only once. The GUI stays unprivileged; the helper runs
`bproxy.Client` + `internal/tun` as root. They talk over a loopback socket
(`internal/helperipc`): the GUI listens, launches the helper with elevation, the
helper authenticates with a token from a `0600` bootstrap file, connects back,
streams status/metrics/logs and accepts `start`/`stop`/`shutdown`/`bypass`. The
keylink travels over the socket in the `start` command, never on the command
line. If the GUI dies, the helper sees the socket close and reverts routes/DNS
automatically (no orphaned root tunnel). Loop avoidance: the helper binds
BoardProxy's own control-plane sockets (WSS/REST/DNS to the board) to the
physical interface via the core `Protector` hook, so board traffic never
re-enters the tunnel.

Hostnames in stats: in TUN mode the helper runs a local DNS forwarder on the TUN
address (`internal/dnsproxy`), records IP→domain from answers, and enriches
stream metrics so the UI can show domains instead of bare IPs.

> Cross-compiling the full app to macOS from Linux is not supported because the
> `fyne.io/systray` dependency needs cgo/native macOS APIs — build the macOS
> binary on macOS. The `internal/tun`, `internal/helperipc`, `internal/elevate`,
> `internal/dnsproxy` and `internal/helper` packages are pure Go and
> cross-compile for all three targets.

## Structure

- `main.go` — entry point; `--helper` branches into the privileged daemon
  (`internal/helper`), otherwise starts the GUI;
- `app.go` — Wails adapter; in-process proxy mode (`connectProxy`);
- `app_tun.go` — TUN mode: launches and drives the reusable privileged helper
  (`helperSession`);
- `internal/helper` — privileged daemon: runs `bproxy.Client` + `internal/tun`
  as root, streams events to the GUI;
- `internal/dnsproxy` — local DNS forwarder that maps IP→domain for stats;
- `internal/tun` — cross-platform TUN engine (`engine.go`), orchestrator
  (`tunnel.go`) and per-OS routing/protector (`*_linux.go`, `*_darwin.go`,
  `*_windows.go`);
- `internal/helperipc` — GUI⇄helper protocol (config + events/commands);
- `internal/elevate` — per-OS privilege-elevation launcher;
- `tray.go` — tray menu (open / connect-toggle / profile submenu / quit). Uses
  the `energye/systray` fork rather than `fyne.io/systray`: on macOS the latter
  declares an Objective-C `AppDelegate` that clashes with wails at link time
  (`duplicate symbol _OBJC_METACLASS_$_AppDelegate`). The fork also supports
  separate left/right click handlers;
- `frontend/src/store` — reactive Zustand state;
- `frontend/src/screens` — overview, profiles, statistics, proxy settings and
  logs;
- `frontend/src/components` — reusable ICMP-derived UI components.
