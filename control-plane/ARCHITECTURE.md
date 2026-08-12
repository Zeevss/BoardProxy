# Control Plane Architecture

```text
frontend -> versioned HTTP API -> Kotlin modular monolith -> PostgreSQL
                                      |             |
                              desired-state     observed facts
                                      |
                              gRPC/TLS + mTLS
                                      |
                              node-agent -> core
```

The active control plane is the Spring Boot application in `server`. It is one
deployable process and one database, split by bounded context rather than by
technical layer:

- `provisioning` owns the normalized per-node catalog, TOML compiler and
  immutable desired revisions;
- `fleet` owns one-time enrollment tokens, the control-plane CA and node
  certificates;
- `delivery` owns connected node sessions, desired/applied drift and node
  status;
- `runtime` stores decoded core facts and projects users, boards and sessions;
- `telemetry` stores and queries interface and per-user traffic separately;
- `audit` stores append-only operator actions;
- `access` owns bearer API tokens, authentication and role authorization;
- `activity` exposes the authenticated, bounded frontend SSE feed;
- `shared` contains technical transaction, encryption and outbox primitives.

Inside a bounded context the dependency direction is
`api/infrastructure -> application -> domain`. Domain code does not know about
Spring, SQL, protobuf, HTTP or gRPC. The more detailed package rules are in
[`server/ARCHITECTURE.md`](server/ARCHITECTURE.md).

`Catalog` is the desired-state consistency boundary. Every accepted mutation
validates the complete aggregate, compiles deterministic core TOML, encrypts it
and appends an immutable SHA-256 revision. Catalog, revision, audit event and
outbox message are committed in one PostgreSQL transaction. `NodeStatus` is a
projection, never desired state.

Encrypted catalog snapshots form append-only history. Granular node, board,
user and assignment commands rebuild the complete validated aggregate.
Rollback reads an old snapshot but writes a new catalog/config revision;
versions never move backwards and private keys never appear in history diff.

The old Go control-plane has been removed. The canonical node protobuf source
lives under `contracts`; generated Go files form a small standalone module used
by node-agent and Kotlin classes are generated during the server build.

## Reactive desired-state delivery

After a successful catalog transaction an outbox worker publishes the event
with PostgreSQL `NOTIFY`. Every server replica owns a `LISTEN` connection and
wakes its matching connected node stream. The node validates and applies the
new TOML and reports `ApplyResult`; the control plane updates desired/applied
drift. A 30-second reconcile remains as loss recovery, not the normal path.

Node-status changes use best-effort PostgreSQL notifications and are forwarded
to authenticated frontend clients through SSE. Failure to notify the UI never
breaks the node gRPC session.

## Reactive runtime facts

Core publishes resource, board-lifecycle and client-session events through a
bounded server-streaming gRPC journal. Node-agent checkpoints
`(core_boot_id, sequence)` and appends each event to its SQLite outbox in one
transaction, then wakes its hub stream immediately. Its periodic scan is only
delivery recovery and ACK-timeout protection.

On restart, cursor mismatch or journal overflow, core sends an explicit reset
and node-agent captures an authoritative snapshot. The Kotlin server commits
the batch claim, decoded facts and the locked per-node projection in one
transaction before ACK, so retransmission is idempotent. Sequence gaps freeze
incremental projection until a snapshot replaces it. Projection changes reach
the frontend SSE stream through PostgreSQL `NOTIFY` on every server replica.

The snapshot contains exact per-user active-session counters but no individual
bundle identities. The runtime REST model therefore exposes
`sessionDetailsComplete`; it never presents a partial session list as complete.

## Security boundary

Enrollment is the only node RPC allowed without a client certificate. A node
creates its private key locally and submits a CSR with a one-time token. All
subsequent node traffic requires TLS 1.3 mutual authentication and matching
certificate identity. The CA/server private keys, user private keys and
compiled TOML are encrypted with AES-256-GCM using a master key supplied
outside PostgreSQL.

Browser/control requests use bearer API tokens stored only as SHA-256 hashes.
`VIEWER` is read-only, `OPERATOR` can mutate desired state and enroll nodes, and
`ADMIN` additionally manages tokens and protected actuator endpoints. Audit
actor identity comes from the authenticated principal, never a request header.

The node certificate inventory records serial, SHA-256 fingerprint, validity,
revocation and last-seen time. A disabled node or revoked certificate is denied
before opening the control stream. Browser request size/rate protection, a
strict CORS allowlist and an AES-GCM master-key keyring cover the other ingress
and rotation boundaries.

## High availability

Each connected node owns a short PostgreSQL lease. Takeover is allowed only
after expiry and increments a fencing token. Status updates carry that token,
so a paused old replica cannot overwrite its successor. Outbox delivery uses
row locks, exponential retry and an admin-visible dead-letter queue.

## Traffic and recovery

Raw interface and per-user deltas have independent queries and hourly rollups.
Retention deletes raw batches after the rollup window. Quotas are UTC
daily/monthly policies; `alert` is non-mutating and `disable` is explicit.

Decoded authoritative runtime snapshots are stored alongside raw protobuf
batches. Admin rebuild starts at the newest snapshot and replays later events
of the same core boot. It refuses to fabricate state without a snapshot.
