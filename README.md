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

The canonical live Den Memories v0 service runs on `den-srv` / `192.168.1.10` as a first-class systemd service:

- systemd unit: `den-memory.service`
- service user/group: `den-memory`
- service root: `/data/services/den-memory`
- app directory: `/data/services/den-memory/app`
- env file: `/data/services/den-memory/env/server.env`
- data/SQLite path: `/data/services/den-memory/data/den-memories.sqlite`
- current selected-profile LAN URL: `http://192.168.1.10:8780`
- health: `curl -fsS http://192.168.1.10:8780/health`

Regular Den agents on den-k8/k8plus can update the service with the checked-in deploy helper. The helper validates the Go repo, builds local Linux binaries, stages only the service artifacts, installs them over SSH, writes or backs up env/unit files as needed, restarts/enables the service, and smokes the remote systemd service.

Default mode is non-mutating:

```bash
scripts/deploy-den-srv.sh
```

Apply the trusted-LAN deploy used by the selected Hermes/pi-crew rollout:

```bash
scripts/deploy-den-srv.sh --yes --addr 0.0.0.0:8780 --force-env
```

The helper preserves `/data/services/den-memory/app.previous` on replacement and creates timestamped backups for overwritten env/unit files. Manual rollback outline:

```bash
ssh den-srv
sudo systemctl stop den-memory.service
sudo rm -rf /data/services/den-memory/app
sudo mv /data/services/den-memory/app.previous /data/services/den-memory/app
sudo systemctl daemon-reload
sudo systemctl restart den-memory.service
curl -fsS http://127.0.0.1:8780/health
```

Security posture: v0 intentionally relies on trusted-LAN/service-boundary trust and does not add API auth/RBAC. Keep `0.0.0.0:8780` on the trusted LAN only; do not expose it through WAN/reverse-proxy without a separate auth decision. The deployment record is mirrored in Den as `den-network/den-memory-service-deploy-2026-06-15`.

