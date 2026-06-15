# Runtime context mapping

Den Memories v0 keeps one runtime-neutral `runtime_context` contract for Hermes, pi-crew, manual imports, and other callers. The Go service deliberately stores the normalized fields used by the v0 SQLite schema rather than persisting the full object on every row.

## Accepted input

Endpoints may receive either flattened fields or a `runtime_context` object. If both are present, explicit flattened fields win.

| `runtime_context` field | Flattened service field | Notes |
| --- | --- | --- |
| `runtime` | `runtime` | Used for capture policy defaults and capture-event audit rows. |
| `agent_identity` | `actor_identity`, `created_by`, `updated_by` | Used only when the flattened field is absent. |
| `role` | `actor_role` | Used for capture policy defaults; `auditor` maps to capture off. |
| `session_kind` | `session_kind` | Preserved for request handling/audit semantics where applicable. |
| `source_surface` | `source_surface` | Preserved for request handling/audit semantics where applicable. |
| `project_id` | `scope_kind=project`, `scope_id=<project_id>` | Used when no explicit `scope_kind`/`scope_id` or `proposed_scope_*` is supplied. |

Capture requests also accept contract-shaped `proposed_scope_kind`/`proposed_scope_id`; these are normalized to `scope_kind`/`scope_id` before candidate creation and capture logging. `raw_content` is normalized to `raw_text` for compatibility with contract examples.

Recall requests use the same mapping: absent flattened scope fields are inferred from `runtime_context.project_id`, which prevents Hermes and pi-crew adapters from silently drifting between project-scope and global recall.

The service does not currently persist opaque, caller-specific `runtime_context` keys. New keys that need durable behavior should be promoted into the contract/schema and mapped explicitly here.
