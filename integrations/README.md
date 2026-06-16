# Den Memories integrations

The Den Memories service implementation is Go-only. Files under this directory are integration/adaptor helpers that call the Go HTTP API; they are not an alternate service implementation.

Current retained Python:

- `integrations/hermes/den_memory_hermes_adapter.py` — dependency-light Hermes adapter boundary used by dogfood smoke tooling until this code moves into a Den-owned Hermes plugin/overlay.

Do not add Python service routes, SQLite migrations, scoring logic, curation logic, recall logic, validators, or audit implementations here. Those belong in Go.

## Hermes v0 tool posture

The Hermes adapter v0 intentionally exposes only read/search/recall, candidate capture, and doctor tools. Curator/linking tools such as `den_memory_link` and `den_memory_curate` are deferred for post-v0 role-gated workflows; v0 curation and topic-edge linking remain direct service/API or curator-runner operations, not normal Hermes memory-provider tools.

Independent auditor/diagnostic profiles receive no Den Memories tools from this adapter.

## Selected Hermes profile rollout

Selected Hermes profiles use the Den Memories service-backed memory-provider plugin from `integrations/hermes/den_memory_plugin/den_memory`, installed into a shared runtime plugin directory and symlinked into each selected profile. This is dogfood scope, not global fleet enablement.

Install/update the selected rollout after the den-srv service is healthy:

```bash
python3 scripts/deploy-hermes-den-memory.py \
  --profiles kate,researcher,house-admin \
  --service-url http://192.168.1.10:8780
```

The installer backs up each profile `config.yaml`, sets `memory.provider: den_memory`, enables the `den_memory` plugin, writes conservative `den_memory` settings, and smokes schema readback plus manual `den_memory_recall`. Defaults remain conservative:

- `auto_recall: false`
- `capture_on_sync: false`
- `deny_auto_behavior: true`
- static prompt block only; no service data is injected into the prompt
- independent auditor/diagnostic contexts expose no Den Memories tools

The selected-profile service URL is `http://192.168.1.10:8780`.
