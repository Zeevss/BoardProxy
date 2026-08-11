# Control Plane Architecture

```text
frontend -> future versioned HTTP API -> backend application services
                           catalog commands | reconciler | node sessions
                                      |          |            |
                              narrow storage ports    node gRPC + mTLS
                                      |                       |
                             filesystem/Postgres    node-agent -> core
```

`backend/internal/domain` contains stable business vocabulary. `application`
implements use cases and the node-session state machine. `ports` declares the
outside capabilities required by those use cases. `adapters` contains gRPC,
filesystem and PKI details. `bootstrap` is the only composition root.

`Catalog` is the per-node control aggregate: `Node`, `Board`, `User` and
`NodeAssignment` have optimistic resource versions and explicit lifecycle
states. Every accepted mutation validates the complete aggregate, compiles a
deterministic core TOML snapshot and appends an immutable SHA-256 revision.
Connected streams receive an in-process wake-up; a periodic reconcile repairs
missed/cross-process notifications. `NodeStatus` is a projection, never desired
state, and records online/readiness plus desired/applied drift and ApplyResult.

The development filesystem adapter implements the same narrow ports as the
future PostgreSQL adapter, but it cannot make catalog, audit and desired files
one transaction. The periodic reconciler makes the compiled revision
eventually consistent; production storage will make the write plus outbox event
atomic.

The node protobuf is not the frontend API. Node traffic is optimized for
reconciliation and delivery acknowledgements; panel endpoints will instead
return authorization-aware read models suited to screens and pagination.
