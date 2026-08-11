# BoardProxy Control Plane

`bproxy-hub` owns node enrollment and durable desired state. Nodes always dial
out to one TLS 1.3 bidirectional gRPC endpoint; core itself is never exposed to
the network.

The control plane is split into two deployable components:

- [`backend`](backend/) — Go application, node contract and infrastructure adapters;
- [`frontend`](frontend/) — reserved UI boundary; its read models and commands
  will be designed before selecting and implementing the frontend stack.

This mirrors the useful Remnawave component boundary while keeping our node
transport different: BoardProxy nodes initiate a durable outbound mTLS stream.
See [`ARCHITECTURE.md`](ARCHITECTURE.md) for dependency direction.
The staged backend, telemetry and frontend work is tracked in
[`ROADMAP.md`](ROADMAP.md).

## Local Docker bootstrap

Prepare the environment and build the two runtime images:

```sh
cp .env.example .env
docker compose build hub node
docker compose up -d hub
```

Generate a one-time, node-bound bootstrap secret from the same persistent hub
volume. Paste the output as `BPROXY_NODE_SECRET` in `.env`:

```sh
docker compose run --rm --no-deps hub token \
  --node node-1 --hub-url hub:8443 --server-names hub,localhost
```

Create a normalized catalog, which compiles and publishes the initial desired
state, then start the node:

```sh
cp control-plane/catalog.example.json catalog.json
# Replace the board hash and both private-key placeholders first.
docker compose run --rm --no-deps -T hub catalog seed \
  --file - --actor operator < catalog.json
docker compose up -d node
```

`catalog node|board|user|assignment` replaces one resource using
`--expected-version`; every successful change validates the whole aggregate,
creates an immutable core config revision and wakes a connected node. The
legacy `config` command remains a break-glass raw TOML publisher and prints a
warning because it bypasses the normalized catalog.

Use `state: "disabled"` for reversible suspension and `state: "revoked"` for
terminal revocation. Revoked users and their private keys are omitted from the
compiled node config. The node validates every revision before activation,
hot-reloads compatible changes and restarts core only for immutable
listener/server changes.

Inspect immutable revision metadata without printing TOML/private keys:

```sh
docker compose run --rm --no-deps hub catalog history --node node-1
```

For separate hosts, issue the hub server certificate with the public DNS name,
publish port 8443, generate the secret with `--hub-url dns-name:8443`, and run
only the node container on the node host. The Compose name `hub` is intended for
the one-host development topology.

## Security and state

The bootstrap token is random, expires after 15 minutes by default, is bound to
one node ID and is consumed once. The agent generates its ECDSA private key
locally; the hub receives only a CSR. The resulting client certificate is valid
for 30 days and is automatically rotated seven days before expiry over existing
mTLS. If it has already expired, issue a new enrollment token and replace
`BPROXY_NODE_SECRET`; the agent then performs a fresh enrollment automatically.

The single-instance development filesystem adapter stores:

- normalized catalogs, immutable desired revision logs, node-status projections,
  audit events, CA/server keys and enrollment tokens under `/var/lib/bproxy-hub`;
- interface batches under `traffic/interface/<node>/`;
- per-user batches under `traffic/user/<node>/`.

Traffic files are protobuf messages named by batch UUID and created with
exclusive semantics, so a retry is idempotent. The adapter is intentionally not
a production database: catalog, audit and revision writes are repaired by the
reconciler but are not one cross-file transaction. PostgreSQL and transactional
outbox delivery are the next stage.
