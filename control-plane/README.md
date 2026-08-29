# BoardProxy Control Plane

The control plane is the Spring Boot modular monolith in [`server`](server/).
It owns durable desired state in PostgreSQL. Nodes dial its TLS 1.3 gRPC port;
core remains private to node-agent and has no database.

```text
frontend -> authenticated HTTP :8080 -> Kotlin server -> PostgreSQL
                                            |
                                            +-- mTLS gRPC :8443 <- node-agent -> core
```

The canonical protobuf source and standalone generated Go client module live
under [`contracts`](contracts/). The old Go control-plane implementation has
been removed.

## Local Docker bootstrap

Generate the encryption master key and keep it stable for the lifetime of the
database and PKI volume:

```sh
cp .env.example .env
openssl rand -base64 32 # CONTROL_MASTER_KEY
docker compose up -d --build
```

Open `http://localhost:8080/`. On an empty database the panel asks for the
first administrator username and password. The password is stored only as a
BCrypt hash. Successful setup/login returns an opaque, expiring panel session;
the browser keeps it in `sessionStorage` and the database keeps only its
SHA-256 hash.

The first-run screen guides the remaining bootstrap:

1. Open **Nodes** and create the first node, then create its board separately.
   The server private key is generated and encrypted by control-plane; it is
   never accepted from or returned to the browser.
2. Issue a one-time enrollment secret for that node and copy the returned
   `BPROXY_NODE_SECRET` into `.env` without Base64 conversion.
3. Start the optional node profile:

```sh
docker compose --profile node up -d --build node
docker compose --profile node logs -f node
```

4. Create users in **Users**, then replace their `/grants` subresource with the
   target nodes and boards. Direct keylinks are derived from those grants.
   A subscription is a separate resource bound to the user and follows future
   grant changes automatically.

API tokens remain available under **Access** for automation, node-independent
integrations and the `SUBSCRIBER` service role. `CONTROL_BOOTSTRAP_ADMIN_TOKEN`
is optional and should only be configured as a temporary emergency machine
credential.

Updating a node, board, user or grants with the current numeric `ETag` rebuilds
the affected desired configuration and wakes the connected node stream.
Compatible changes are hot-applied by core; listener or server changes may
require node-agent to restart core.

The curl examples below assume `ADMIN_TOKEN` was issued under **Access** for
automation. Interactive browser sessions do not expose their session token.

```sh
curl -X PUT http://localhost:8080/api/v1/users/alice \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'If-Match: "1"' \
  --data '{"name":"Alice","state":"enabled","maxSessions":2,"maxLanes":4}'
```

Use `state: "disabled"` for reversible suspension and `state: "revoked"` for
terminal revocation. Revoked resources and private credentials are omitted
from compiled node configuration. `maxSessions` is enforced independently by
each node/core; it is not a fleet-wide concurrent-session semaphore.

Replace the user's placements as one operation:

```sh
curl -X PUT http://localhost:8080/api/v1/users/alice/grants \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  --data '[{"nodeId":"node-1","boardIds":["board-1"]}]'
```

## HTTP access

- `VIEWER` reads nodes, boards, users, subscriptions, status, traffic and events;
- `OPERATOR` also mutates these resources and creates enrollment secrets;
- `ADMIN` also creates, lists and revokes API tokens and reads protected
  actuator endpoints.

OpenAPI JSON is available at `/v3/api-docs`, Swagger UI at
`/swagger-ui/index.html`, and frontend SSE at `/api/v1/events`. Health remains
public at `/actuator/health`; all business endpoints require a bearer token.

Current core runtime state is available at
`/api/v1/nodes/{nodeId}/runtime`; decoded facts are available at the sibling
`/runtime/events` endpoint. Forward event pagination must include both
`coreBootId` and `afterSequence`. Runtime projection changes are also emitted
as `runtime.projection.changed` through the frontend SSE stream.

Catalog history/diff/rollback, traffic quotas, certificate revocation and
outbox dead-letter recovery are documented in generated OpenAPI.

See [`ARCHITECTURE.md`](ARCHITECTURE.md) and [`ROADMAP.md`](ROADMAP.md).
