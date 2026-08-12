# Control plane package architecture

The control plane is one Spring Boot process and one PostgreSQL database. It is
a modular monolith: packages are grouped by bounded context first, and each
context owns its use cases and adapters.

```text
io.boardproxy.control
├── access             API tokens, authentication and RBAC
│   ├── domain
│   ├── application
│   ├── api/rest
│   └── infrastructure/{config,security,persistence}
├── activity           authenticated frontend SSE feed
├── provisioning       catalog, TOML compilation and immutable revisions
│   ├── domain/model
│   ├── application
│   ├── api/rest
│   └── infrastructure/{compiler,config,persistence}
├── fleet              enrollment tokens, CA and node certificates
│   ├── domain
│   ├── application
│   ├── api/rest
│   └── infrastructure/{config,pki,persistence}
├── delivery           node stream, desired/applied state and status
│   ├── domain
│   ├── application
│   ├── api/{grpc,rest}
│   └── infrastructure/{config,events,persistence}
├── runtime            raw core facts and authoritative online projection
├── telemetry          interface and per-user traffic ingestion and queries
├── audit              append-only operator actions
└── shared             technical errors, encryption, transactions and outbox
```

Only packages with real code are created. `provisioning` deliberately owns both
the normalized catalog and config revision: compilation is part of one desired
state consistency boundary. `delivery` consumes revisions but never edits
them. `fleet` owns identity, while `runtime` and `telemetry` contain observed
facts and never become desired state.

## Dependency rules

- domain code has no Spring, SQL, protobuf, HTTP or gRPC imports;
- application code depends on domain and narrow ports;
- REST/gRPC adapters call application services, not JDBC repositories;
- infrastructure implements output ports;
- cross-feature calls target application interfaces only;
- business events belong to their producing feature; `shared` contains only
  transport envelopes and technical primitives;
- mutation, encrypted revision, audit and outbox append share one transaction.

ArchUnit checks the enforceable subset in `ArchitectureTest`.

## Reactive desired-state path

```text
REST mutation
  -> PostgreSQL transaction (catalog + encrypted revision + audit + outbox)
  -> outbox publisher + PostgreSQL NOTIFY
  -> LISTEN on every server replica + local event bus
  -> active mTLS gRPC stream
  -> node-agent validates/applies TOML
  -> ApplyResult updates desired/applied projection
```

A 30-second reconciliation check remains as recovery for a lost notification.
It is not the normal update mechanism. Status changes are ephemeral distributed
events; durable desired changes always originate from the transactional outbox.

The same local event bus fans safe event envelopes out to authenticated frontend
SSE clients. Every client has its own bounded serial queue, so a slow browser
cannot block PostgreSQL notifications or node delivery. The feed never exposes
encrypted desired payloads or credentials.

## Runtime projection path

```text
core journal -> node SQLite outbox -> mTLS node stream
  -> one PostgreSQL transaction
       (claim batch + decoded facts + locked projection)
  -> ACK
  -> PostgreSQL NOTIFY -> frontend SSE
```

Facts are deduplicated by `(node_id, event_id)` and ordered by
`(node_id, core_boot_id, sequence)`. A reused sequence with a different event
is rejected instead of being silently hidden. Incremental projection stops on
a sequence gap. A core snapshot replaces users/boards/counters and clears that
gap, after which later events can continue the projection. Snapshot session
counts are authoritative, while individual bundle details are explicitly
marked incomplete because the core snapshot contract does not contain them.
