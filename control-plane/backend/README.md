# Control Plane Backend

Source dependencies point inward:

```text
cmd -> cli/bootstrap -> adapters -> application -> ports/domain
```

- `domain` contains the catalog aggregate, lifecycle/version invariants,
  desired revisions, node status, traffic kinds and domain errors;
- `application` contains catalog mutation/reconciliation, enrollment,
  desired-state and node-session use cases;
- `ports` declares narrow catalog, revision, status, audit, traffic,
  notification and certificate-authority capabilities;
- `adapters` implements filesystem, PKI and gRPC boundaries;
- `bootstrap` wires concrete adapters;
- `cmd` contains no business logic.

The current filesystem store is a single-instance development adapter.
PostgreSQL/ClickHouse implementations can satisfy the same ports without
changing application services or the node protobuf.

Run locally:

```sh
go test ./...
go run ./cmd/bproxy-hub serve --data /tmp/bproxy-hub --server-names localhost
```

Create managed desired state from strict JSON (human-readable durations are
accepted), then replace individual resources with optimistic versions:

```sh
go run ./cmd/bproxy-hub catalog seed --data /tmp/bproxy-hub --file ../catalog.example.json --actor operator
go run ./cmd/bproxy-hub catalog board --data /tmp/bproxy-hub --node node-1 \
  --file board.json --expected-version 1 --actor operator
```
