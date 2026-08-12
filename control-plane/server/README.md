# BoardProxy control-plane server

Single Spring Boot modular monolith and the only control-plane implementation.

Implemented paths:

- PostgreSQL/Flyway catalog and encrypted immutable config revisions;
- bearer API tokens with `VIEWER`, `OPERATOR` and `ADMIN` RBAC;
- versioned REST catalog API with `ETag`/`If-Match`;
- generated OpenAPI JSON and Swagger UI;
- one-time enrollment, encrypted local CA material and 30-day node certificates;
- TLS 1.3 gRPC with unauthenticated `Enroll` and mandatory mTLS identity for
  `Renew` and `Connect`;
- PostgreSQL outbox/`LISTEN`/`NOTIFY` delivery across server replicas;
- reactive desired-state delivery and applied/readiness projection;
- frontend SSE for desired-state, node-status and runtime-projection updates;
- idempotent interface/per-user traffic ingestion with ACK;
- decoded runtime facts, sequence/gap detection, snapshot replacement and a
  queryable per-node users/boards/sessions projection;
- granular resources, encrypted history/diff/rollback;
- traffic series, hourly rollups, retention and quotas;
- certificate revocation, keyring rotation and request hardening;
- HA lease/fencing and retry/dead-letter outbox;
- runtime replay and embedded React operations UI.

Private server/user keys and compiled TOML are stored as AES-256-GCM
ciphertext. REST responses never expose private material. API token plaintext
is returned only at creation; PostgreSQL stores its SHA-256 hash.

## Local build

```sh
export CONTROL_MASTER_KEY="$(openssl rand -base64 32)"
export CONTROL_BOOTSTRAP_ADMIN_TOKEN="$(openssl rand -base64 32)"
export CONTROL_DB_URL=jdbc:postgresql://localhost:5432/boardproxy
./gradlew clean test bootJar
./gradlew bootRun
```

HTTP listens on `8080`; node gRPC listens on `8443`.

## HTTP API

```text
POST   /api/v1/access/tokens                       ADMIN
GET    /api/v1/access/tokens                       ADMIN
DELETE /api/v1/access/tokens/{id}                  ADMIN
POST   /api/v1/catalogs                            OPERATOR
GET    /api/v1/nodes                               VIEWER, paginated
GET    /api/v1/catalogs/{nodeId}                   VIEWER
PUT    /api/v1/catalogs/{nodeId}                   OPERATOR, If-Match
PATCH  /api/v1/nodes/{nodeId}                      OPERATOR, If-Match
PUT/DELETE /api/v1/nodes/{nodeId}/boards/{id}      OPERATOR, If-Match
PUT/DELETE /api/v1/nodes/{nodeId}/users/{id}       OPERATOR, If-Match
PUT    /api/v1/nodes/{nodeId}/assignment           OPERATOR, If-Match
GET    /api/v1/nodes/{nodeId}/catalog-revisions    VIEWER
GET    /api/v1/nodes/{nodeId}/catalog-diff         VIEWER
POST   /api/v1/nodes/{nodeId}/catalog-rollback     OPERATOR, If-Match
POST   /api/v1/nodes/{nodeId}/enrollment-tokens   OPERATOR
GET/DELETE /api/v1/nodes/{nodeId}/certificates/**  VIEWER / ADMIN
GET    /api/v1/nodes/{nodeId}/status               VIEWER
GET    /api/v1/nodes/{nodeId}/traffic/interfaces  VIEWER
GET    /api/v1/nodes/{nodeId}/traffic/users       VIEWER
GET    /api/v1/nodes/{nodeId}/traffic/series       VIEWER
GET/PUT/DELETE /api/v1/nodes/{nodeId}/traffic/quotas/** VIEWER / OPERATOR
GET    /api/v1/nodes/{nodeId}/runtime             VIEWER
GET    /api/v1/nodes/{nodeId}/runtime/events      VIEWER
POST   /api/v1/nodes/{nodeId}/runtime/rebuild      ADMIN
GET/POST /api/v1/operations/outbox/dead-letters/** ADMIN
GET    /api/v1/events                              VIEWER, text/event-stream
```

`runtime/events` returns the latest events by default. Stable forward replay
uses both `coreBootId` and `afterSequence`; a sequence has meaning only inside
one core boot. `runtime` exposes `sessionDetailsComplete=false` after a snapshot
that contains active-session counts but cannot reconstruct individual bundle
IDs. The flag becomes true again once all such sessions have closed.

Roles are hierarchical: `ADMIN` includes operator/viewer authorities and
`OPERATOR` includes viewer authority. Actor attribution is always derived from
the authenticated principal.
