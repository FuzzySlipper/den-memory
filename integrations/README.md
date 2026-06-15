# Den Memories integrations

The Den Memories service implementation is Go-only. Files under this directory are integration/adaptor helpers that call the Go HTTP API; they are not an alternate service implementation.

Current retained Python:

- `integrations/hermes/den_memory_hermes_adapter.py` — dependency-light Hermes adapter boundary used by dogfood smoke tooling until this code moves into a Den-owned Hermes plugin/overlay.

Do not add Python service routes, SQLite migrations, scoring logic, curation logic, recall logic, validators, or audit implementations here. Those belong in Go.
