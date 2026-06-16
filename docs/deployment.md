# Den Memories deployment

## den-srv service

The live v0 Den Memories service is deployed on `den-srv` / `192.168.1.10` as:

```text
Systemd unit:  den-memory.service
Service user:  den-memory
Service group: den-memory
Root:          /data/services/den-memory
App:           /data/services/den-memory/app
Env:           /data/services/den-memory/env/server.env
Data:          /data/services/den-memory/data
SQLite DB:     /data/services/den-memory/data/den-memories.sqlite
LAN URL:       http://192.168.1.10:8780
Health:        http://192.168.1.10:8780/health
```

## Agent update path

From den-k8/k8plus, agents should deploy from the reviewed checkout with:

```bash
cd /home/dev/den-memory
./scripts/deploy-den-srv.sh --yes --addr 0.0.0.0:8780 --force-env
```

The helper refuses dirty checkouts, runs Go tests/vet/contract validation by default, runs a local smoke, uploads a service artifact, updates `/data/services/den-memory/app`, writes or backs up env/unit files, restarts/enables `den-memory.service`, and runs remote smoke checks.

Use the dry run first when changing flags:

```bash
./scripts/deploy-den-srv.sh
```

## Verification

```bash
ssh den-srv 'systemctl is-active den-memory.service && systemctl is-enabled den-memory.service'
curl -fsS http://192.168.1.10:8780/health
curl -fsS http://192.168.1.10:8780/api/version
curl -fsS http://192.168.1.10:8780/api/doctor/report
curl -fsS 'http://192.168.1.10:8780/api/audit/export?format=jsonl' | head
```

Expected service/user layout:

```text
/data/services/den-memory                  root:root          0755
/data/services/den-memory/app              root:root          0755
/data/services/den-memory/env              root:den-memory    0750
/data/services/den-memory/env/server.env   root:den-memory    0640
/data/services/den-memory/data             den-memory:den-memory 0750
```

## Rollback

The helper preserves `/data/services/den-memory/app.previous` when replacing an existing app directory and writes timestamped backups beside env/unit files.

```bash
ssh den-srv
sudo systemctl stop den-memory.service
sudo rm -rf /data/services/den-memory/app
sudo mv /data/services/den-memory/app.previous /data/services/den-memory/app
sudo systemctl daemon-reload
sudo systemctl restart den-memory.service
curl -fsS http://127.0.0.1:8780/health
```

## Security note

`0.0.0.0:8780` is approved only for the trusted Den LAN. v0 has no API auth/RBAC; do not expose the service to WAN or a public reverse proxy without a separate product/security decision.

The 2026-06-15 deployment record is stored in Den as `den-network/den-memory-service-deploy-2026-06-15`.
