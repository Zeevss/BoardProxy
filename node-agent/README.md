# BoardProxy Node Agent

`bproxy-node` is the only stateful process on a data-plane host. Its state is
operational, not desired business state: an mTLS identity, last applied revision,
last-known-good core config, collector checkpoints and an unacknowledged outbox.
Deleting its volume causes re-enrollment and loses only telemetry that has not
yet reached the hub.

The agent:

1. enrolls using `BPROXY_NODE_SECRET` and keeps the generated private key local;
2. maintains an outbound mTLS stream to the hub with reconnect/backoff;
3. validates each desired TOML with `bproxy serve --config ... --test`;
4. hot-reloads core or supervises a controlled restart with rollback;
5. samples container-interface and per-user core counters independently;
6. atomically saves each delta with its checkpoint in SQLite and retries until
   hub ACK.

The executable entry point only parses configuration and process signals.
`internal/agent` owns orchestration, split into session, desired-state,
background-loop and state files; `coremgr`, `identity`, `localstore` and `stats`
remain focused infrastructure adapters.

## Two traffic streams

`BPROXY_STATS_INTERFACES` defaults to `eth0` and may contain a comma-separated
list. Counters are read from `/sys/class/net/<interface>/statistics` inside the
node container network namespace. This measures all container traffic and
overhead. Do not bind-mount a host interface path unless host-wide accounting is
explicitly desired.

Per-user traffic comes from core `GetStats`. It represents decrypted BoardProxy
payload attributed by `user_tag`; RX is client upload and TX is client download.
These values answer a different question from interface RX/TX and must not be
added together.

The first interface sample only establishes a baseline. Kernel counter resets
and core restarts are treated as new epochs. The local `node.sqlite3` uses WAL,
`synchronous=FULL`, schema migrations and mode 0600. One SQL transaction
advances collector checkpoints only together with insertion of the corresponding
outbox events, so a crash cannot create a missing interval.

The old bbolt prototype used `node.db`. On startup, a volume containing that
file but no `node.sqlite3` is rejected explicitly: the agent never silently
drops its unacknowledged telemetry. Archive or remove the legacy file only after
deciding that its prototype outbox is no longer required; the runtime does not
link bbolt merely to support a one-time conversion.
