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

Generate two independent secrets and keep the master key stable for the
lifetime of the database and PKI volume:

```sh
cp .env.example .env
openssl rand -base64 32 # CONTROL_MASTER_KEY
openssl rand -base64 32 # CONTROL_BOOTSTRAP_ADMIN_TOKEN
docker compose up -d --build postgres hub
```

Open `http://localhost:8080/`. The production image serves the React login
shell publicly, while every `/api/v1` request remains bearer-authenticated.

Use the bootstrap token to create a persistent admin token. Its plaintext is
returned exactly once; the database stores only its SHA-256 hash.

```sh
export BOOTSTRAP_TOKEN='<CONTROL_BOOTSTRAP_ADMIN_TOKEN>'
curl -X POST http://localhost:8080/api/v1/access/tokens \
  -H "Authorization: Bearer $BOOTSTRAP_TOKEN" \
  -H 'Content-Type: application/json' \
  --data '{"name":"local-admin","role":"ADMIN"}'
```

Save the returned `secret` as `ADMIN_TOKEN`. After at least one persistent
admin token exists, `CONTROL_BOOTSTRAP_ADMIN_TOKEN` may be cleared and hub
restarted.

Create the first catalog:

```sh
cp control-plane/catalog.example.json catalog.json
# Replace board hash and both private-key placeholders.
curl -X POST http://localhost:8080/api/v1/catalogs \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  --data-binary @catalog.json
```

Issue the one-time node secret:

```sh
curl -X POST http://localhost:8080/api/v1/nodes/node-1/enrollment-tokens \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  --data '{"hubUrl":"hub:8443","ttlSeconds":900}'
```

Copy `nodeSecret` into `BPROXY_NODE_SECRET` in `.env`, then start the node:

```sh
docker compose up -d --build node
curl -H "Authorization: Bearer $ADMIN_TOKEN" \
  http://localhost:8080/api/v1/nodes/node-1/status
```

Updating the catalog with its current `ETag` wakes the connected node stream
immediately. Compatible user/board changes are hot-applied by core; listener or
server changes may require node-agent to restart core.

```sh
curl -X PUT http://localhost:8080/api/v1/catalogs/node-1 \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'If-Match: "1"' \
  --data-binary @catalog.json
```

Use `state: "disabled"` for reversible suspension and `state: "revoked"` for
terminal revocation. Revoked resources and private credentials are omitted
from compiled node configuration.

Granular example:

```sh
curl -X PUT http://localhost:8080/api/v1/nodes/node-1/users/alice \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H 'If-Match: "1"' \
  -H 'Content-Type: application/json' \
  --data '{"name":"Alice","privateKey":"...","state":"enabled","maxSessions":2,"maxLanes":4}'
```

## HTTP access

- `VIEWER` reads catalogs, status, traffic and frontend events;
- `OPERATOR` also mutates catalogs and creates enrollment secrets;
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
