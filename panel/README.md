# BoardProxy panel (legacy)

This directory contains the previous React/Go multi-node panel. It targeted the
stateful core HTTP API (user/board CRUD, restart, SQLite backup and revocable
access-key storage). That API was deliberately removed from stateless core.

The panel is therefore no longer part of the root Compose deployment and must
not be pointed at a current core instance: most screens and mutations are
incompatible even if the UI itself builds.

The intended replacement is a separate control plane that:

- owns durable desired-state files/secrets, audit history and operator auth;
- generates a complete versioned TOML snapshot per node;
- applies immediate changes through `core/api/control/v1/control.proto`;
- uses optimistic runtime revisions and reconciles drift;
- scrapes ephemeral `GetStats` into an external metrics store;
- reaches core through a local/sidecar Unix socket or an authenticated mTLS
  transport, instead of reintroducing credentials and a database into core.

The legacy sources are kept temporarily as UI/reference material. They still
build independently with:

```sh
npm install
npm run build
cd gateway && go test ./...
```
