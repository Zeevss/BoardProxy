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

1. Open **Nodes** and create the first node together with its first board.
   The server private key is generated with Web Crypto and remains write-only.
2. Copy the returned `BPROXY_NODE_SECRET` into `.env` without Base64
   conversion.
3. Start the optional node profile:

```sh
docker compose --profile node up -d --build node
docker compose --profile node logs -f node
```

4. Create users in **Users**. The provisioning transaction generates the
   private key server-side, assigns the selected boards and returns either
   direct keylinks or one subscription URL. Plaintext credentials are shown
   exactly once.

API tokens remain available under **Access** for automation, node-independent
integrations and the `SUBSCRIBER` service role. `CONTROL_BOOTSTRAP_ADMIN_TOKEN`
is optional and should only be configured as a temporary emergency machine
credential.

Updating the catalog with its current `ETag` wakes the connected node stream
immediately. Compatible user/board changes are hot-applied by core; listener or
server changes may require node-agent to restart core.

The curl examples below assume `ADMIN_TOKEN` was issued under **Access** for
automation. Interactive browser sessions do not expose their session token.

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
