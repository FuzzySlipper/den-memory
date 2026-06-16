# den-memory / den-memories

Initial Den Memories repository.

Den Memories is a standalone graph-guided long-term memory substrate owned by the Den `den-memory` project. It is adjacent to Den Core, but Den Core remains authoritative for projects, tasks, documents, messages, worker state, review state, and workflow records.

## Current contents

The canonical service implementation is Go. The old Python service/prototype has been deleted so agents do not patch legacy code by accident. The only Python kept in this repository is adapter/smoke tooling that talks to the Go service over HTTP, currently under `integrations/hermes/` and `scripts/`.

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

## Deploy to den-srv

Regular Den agents on den-k8 can deploy the service to den-srv with the checked-in deploy helper. The helper validates the Go repo, builds local Linux binaries, stages only the service artifacts, installs them over SSH, and smokes the remote systemd service.

Default mode is non-mutating:

```bash
scripts/deploy-den-srv.sh
```

Apply the deploy after the plan looks correct:

```bash
scripts/deploy-den-srv.sh --yes
```

Defaults:

- SSH target: `den-srv`
- systemd unit: `den-memory.service`
- service user/group: `den-memory`
- service root: `/data/services/den-memory/`
- env file: `/data/services/den-memory/env/server.env`
- data/SQLite path: `/data/services/den-memory/data/den-memories.sqlite`
- initial bind: `127.0.0.1:8780`

Use `--addr 0.0.0.0:8780 --force-env` only if selected Hermes/pi-crew clients need direct trusted-LAN access. v0 intentionally relies on LAN/service-boundary trust and does not add API auth/RBAC.

