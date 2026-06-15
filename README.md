# den-memory / den-memories

Initial Den Memories repository.

Den Memories is a standalone graph-guided long-term memory substrate owned by the Den `den-memory` project. It is adjacent to Den Core, but Den Core remains authoritative for projects, tasks, documents, messages, worker state, review state, and workflow records.

## Current contents

The canonical service implementation is Go. The existing Python package is a
pre-deployment prototype/spec kept temporarily for parity while the Go rewrite
settles.

Task #2467 establishes the v0 contract foundation:

- canonical vocabulary registry;
- recall scoring constants;
- JSON Schema contract artifacts;
- example payloads;
- validation tests.

See `docs/v0-contract.md`.

## Validate

```bash
go test ./...
go vet ./...
go run ./cmd/den-memory-validate
```

## Boundary

Hermes and pi-crew should integrate through thin adapters over this service/API model. They must not fork the memory ontology locally.

## Service skeleton

Run a local development server:

```bash
go run ./cmd/den-memories -db /tmp/den-memories.sqlite -addr 127.0.0.1:8765
```

Smoke endpoints:

```bash
curl -fsS http://127.0.0.1:8765/health
curl -fsS http://127.0.0.1:8765/api/version
```
