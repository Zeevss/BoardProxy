# BoardProxy Core

BoardProxy Core is a SOCKS5/HTTP proxy whose transport is an online whiteboard.
The server is now a **stateless runtime**: its complete desired state is loaded
from a versioned TOML file, while live changes are applied through gRPC. Core
does not own a database, user catalogue, configuration history or accumulated
traffic history.

## Architecture

The data plane remains layered and independent from the control plane:

```text
client proxy -> mux -> bond -> link -> board -> hub -> egress -> target
                                          ^
                                          |
                    immutable policy snapshot + runtime reconciler
                                          ^
                                          |
                                  TOML / gRPC control
```

The important server-side packages are:

| Package | Responsibility |
|---|---|
| `internal/serverconfig` | strict, versioned TOML schema, defaults and validation |
| `internal/control` | atomic user policy snapshot and process-local session counters |
| `internal/app` | composition root and board/user reconciler |
| `internal/controlapi` | typed gRPC control adapter |
| `internal/telemetry` | secret-free, since-start runtime statistics DTOs |
| `internal/mgmt` | read-only HTTP adapter: health, stats and recent in-memory logs |
| `internal/hub` | authenticated rendezvous and live sessions; no persistence dependency |

The rest of the data plane (`board`, `link`, `bond`, `mux`, `proxy`, `egress`)
keeps the same narrow interfaces. A board is an independently managed runtime:
an unavailable board enters `retrying` with bounded exponential backoff without
stopping healthy boards or the control API.

## Server configuration

Start from [`config.example.toml`](config.example.toml). Generate secrets
offline and put them into the file:

```sh
bproxy generate server-key
bproxy generate user-key
cp config.example.toml config.toml
chmod 600 config.toml
bproxy serve --config config.toml --test
bproxy serve --config config.toml
```

The same config can be piped through stdin:

```sh
bproxy serve stdin: < config.toml
# `-` is an alias for `stdin:`
```

Unknown TOML fields are rejected. Tags, keys, board references, duplicate
identities and limits are validated before any board is started. The file must
contain `version = 1`, one server private key and explicit user-to-board
allowlists. Users normally contain their private key, which lets the operator
recover their keylink at any time. `public_key` exists only for migrating an
old identity whose private key is already lost.

Relevant semantics:

- `max_sessions = 0` means unlimited process-wide logical sessions for a user;
- `max_lanes` is constrained to 1..32 for both boards and users;
- a user's effective lane limit is the smaller user/board limit;
- disabled resources stay in desired state but do not accept traffic;
- changing a user policy disconnects its current sessions immediately;
- changing only board `max_lanes` affects new bundles without rebuilding it;
- changing board connection settings drains and replaces that board runtime;
- server identity and listener addresses cannot be changed in-place.

## Reactive updates

The default control endpoint is a mode-0600 Unix socket. The public API is
defined in [`api/control/v1/control.proto`](api/control/v1/control.proto) and
supports:

- `GetRuntime`, `Reload`, atomic `ApplySnapshot`;
- `ListUsers`, `ReplaceUser`, `SetUserEnabled`, `RemoveUser`, `GetKeylink`;
- `ListBoards`, `ReplaceBoard`, `SetBoardEnabled`, `RemoveBoard`;
- `GetStats`.

Every successful mutation increments a runtime revision. Clients may provide
`expected_revision`; a stale value is rejected with gRPC `ABORTED`, preventing
lost concurrent updates. Zero means "apply against the revision observed by
this operation" and is convenient for one-off administrative commands.

Runtime mutations intentionally change **memory only**. They are useful for
instant activation, disabling and emergency changes, but the TOML file remains
the durable source of truth. `Reload` rereads the file and reconciles the whole
runtime, replacing any ephemeral divergence. A stdin-started process cannot
reload because stdin is not replayable.

Built-in CLI examples:

```sh
bproxy --control unix:///run/bproxy/control.sock users list
bproxy --control unix:///run/bproxy/control.sock users keylink alice
bproxy --control unix:///run/bproxy/control.sock users disable alice
bproxy --control unix:///run/bproxy/control.sock users enable alice
bproxy --control unix:///run/bproxy/control.sock users remove alice
bproxy --control unix:///run/bproxy/control.sock boards list
bproxy --control unix:///run/bproxy/control.sock boards disable primary
bproxy --control unix:///run/bproxy/control.sock boards enable primary
bproxy --control unix:///run/bproxy/control.sock boards remove primary
bproxy --control unix:///run/bproxy/control.sock reload
bproxy --control unix:///run/bproxy/control.sock stats
```

The protobuf API exposes full resource replacement and atomic user/board
snapshots directly. The CLI deliberately keeps full specs out of flags:
edit/reload TOML for durable changes, or use a generated gRPC client for an
external reconciler/control plane.

Plaintext management listeners are restricted to Unix sockets or loopback TCP.
The Unix socket is the recommended production default. Remote management should
be provided by a separate authenticated control plane or a future mTLS adapter,
not by exposing core directly.

## Statistics and observability

Statistics are deliberately ephemeral and reset on every process start. Core
keeps only values needed to operate the current runtime:

- configured/enabled/online users and configured/enabled/running boards;
- active logical connections, lanes and streams;
- payload RX/TX totals since process start, globally and per user;
- board pool/cleanup state and failures;
- reconnect, circuit-breaker and snapshot counters;
- network-interface counters relative to the process start sample;
- the most recent disconnect/reconnect timestamps and reason.

There are no billing totals, historical time series or per-target records.
Exporting long-term metrics belongs outside core: scrape `GetStats`/`GET
/stats` into Prometheus, ClickHouse or another observability system.

Optional read-only HTTP is configured by `management.http_listen` and must bind
to loopback. It exposes only:

| Method | Path | Meaning |
|---|---|---|
| `GET` | `/healthz` | process is serving HTTP |
| `GET` | `/readyz` | at least one enabled board is active; otherwise 503 |
| `GET` | `/stats` | secret-free runtime snapshot |
| `GET` | `/logs?limit=500` | bounded recent in-memory structured logs |

There is intentionally no HTTP CRUD, restart, backup or authentication-key
store. Desired-state mutation is gRPC-only.

## Client

The client path is unchanged:

```sh
bproxy connect --link 'bproxy://…' --listen 127.0.0.1:1080
curl --socks5 127.0.0.1:1080 https://example.com
curl -x http://127.0.0.1:1080 https://example.com
```

A complete annotated client configuration is available in
[`client.example.toml`](client.example.toml):

```sh
cp client.example.toml client.toml
bproxy connect client.toml
```

A keylink contains the client private key, pinned server public key and allowed
board hashes. It can be obtained for config users with a private key via
`users keylink <tag>`. Migration-only public-key users can authenticate with
their existing key but core cannot reconstruct their lost keylink.

The client TOML and Android facade remain independent of the new server config.
The data-plane lifecycle still performs explicit graceful close/GOAWAY,
heartbeat-based stale-page reclamation and board websocket reconnect.
`retry_initial_connection = true` keeps a supervised client alive if its first
rendezvous happens before the network or board becomes available. CLI flags can
enable the same behavior with `--retry-initial`; Android enables it by default.

## Docker

The root Compose file mounts the desired state read-only and keeps only the
ephemeral gRPC socket in a runtime volume:

```sh
cp core/config.example.toml config.toml
# fill keys and board values
docker compose run --rm core serve --config /etc/bproxy/config.toml --test
docker compose up -d --build core
docker compose exec core bproxy --control unix:///run/bproxy/control.sock stats
```

No `/data` volume is required. The old multi-node panel depended on the removed
stateful HTTP CRUD API and is intentionally not wired into Compose. Its proper
replacement is a separate control-plane reconciler that owns durable desired
state and calls the gRPC API.

## Build and tests

```sh
make build
make test
go test -race ./...
```

Live Yandex Board tests remain opt-in:

```sh
BPROXY_LIVE=1 BPROXY_BOARD=<boardHash> go test ./... -run Live
```

The core module requires Go 1.26+. Main dependencies are
`github.com/coder/websocket`, `github.com/flynn/noise`,
`github.com/BurntSushi/toml`, Cobra and gRPC/protobuf. SQLite is no longer in
the dependency graph.
