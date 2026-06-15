# Den Memories integrations

The Den Memories service implementation is Go-only. Files under this directory are integration/adaptor helpers that call the Go HTTP API; they are not an alternate service implementation.

Current retained Python:

- `integrations/hermes/den_memory_hermes_adapter.py` — dependency-light Hermes adapter boundary used by dogfood smoke tooling until this code moves into a Den-owned Hermes plugin/overlay.

Do not add Python service routes, SQLite migrations, scoring logic, curation logic, recall logic, validators, or audit implementations here. Those belong in Go.

## Hermes v0 tool posture

The Hermes adapter v0 intentionally exposes only read/search/recall, candidate capture, and doctor tools. Curator/linking tools such as `den_memory_link` and `den_memory_curate` are deferred for post-v0 role-gated workflows; v0 curation and topic-edge linking remain direct service/API or curator-runner operations, not normal Hermes memory-provider tools.

Independent auditor/diagnostic profiles receive no Den Memories tools from this adapter.
