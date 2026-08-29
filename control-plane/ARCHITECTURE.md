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

- `provisioning` owns normalized nodes, boards, users and grants, the TOML
  compiler, current desired config and encrypted source snapshots;
- `fleet` owns one-time enrollment tokens, the control-plane CA and node
  certificates;
- `delivery` owns connected node sessions, desired/applied drift and node
  status;
- `delivery` also stores authoritative runtime snapshots and the activity log;
- `telemetry` stores and queries interface and per-user traffic separately;
- `audit` stores append-only operator actions;
- `access` owns bearer API tokens, authentication and role authorization;
- `activity` exposes the authenticated, bounded frontend SSE feed;
- `shared` contains technical transaction, encryption and outbox primitives.

Inside a bounded context the dependency direction is
`api/infrastructure -> application -> domain`. Domain code does not know about
Spring, SQL, protobuf, HTTP or gRPC. The more detailed package rules are in
[`server/ARCHITECTURE.md`](server/ARCHITECTURE.md).

Owned entities have independent optimistic versions. A mutation locks each
affected node in stable order, reloads the complete owned state, compiles
deterministic TOML and advances the node revision only when its SHA-256 changes.
The row lock prevents concurrent entity edits from publishing the same revision
with different bytes. Config, encrypted source snapshot, audit and outbox event
commit in one transaction. Rollback restores a snapshot as new owned-state
writes; versions never move backwards and private keys never appear in diff.

The old Go control-plane has been removed. The canonical node protobuf source
lives under `contracts`; generated Go files form a small standalone module used
by node-agent and Kotlin classes are generated during the server build.

## Reactive desired-state delivery

After a successful desired-state transaction an outbox worker publishes the event
with PostgreSQL `NOTIFY`. Every server replica owns a `LISTEN` connection and
wakes its matching connected node stream. The node validates and applies the
new TOML and reports `ApplyResult`; the control plane updates desired/applied
drift. A 30-second reconcile remains as loss recovery, not the normal path.

Node-status changes use best-effort PostgreSQL notifications and are forwarded
to authenticated frontend clients through SSE. Failure to notify the UI never
breaks the node gRPC session.

## Runtime facts

Node-agent reports an authoritative runtime snapshot, additive traffic deltas
and activity events in an idempotent batch. The hub claims `(agent_id,
batch_id)` before recording it. Status and snapshot replacement are fenced by
`boot_id` and monotonic `seq`; a boot seen before can never replace its
successor. An older unique batch may still contribute traffic and historical
events, but cannot regress status, runtime or receive a pending command.

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

Desired compilation is serialized per node with PostgreSQL row locks. Agent
reports use persistent boot history plus sequence fencing, so a delayed old
process cannot overwrite its successor. Outbox delivery uses row locks,
exponential retry and an admin-visible dead-letter queue.

## Traffic and recovery

Raw interface and per-user deltas have independent queries and immediate hourly
rollups. Reads combine complete rollup hours with raw boundary/missing hours
without double counting. Before rollup retention removes per-user history it is
folded into lifetime totals. Quotas support UTC daily, weekly, monthly and
lifetime windows; `alert`, `reset` and `disable` keep threshold and blocking
state separate. A durable generation queue guarantees quota-driven desired
config reconciliation even if PostgreSQL NOTIFY is missed.
