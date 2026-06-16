# Den Memories LLM curator proposer

Task: #2525
Status: implementation note for the first LLM-assisted proposer path.

## Placement

The LLM-assisted proposer lives in the adjacent curator CLI/package, not inside the core Den Memories HTTP service:

- `cmd/den-memory-curator`
- `internal/curator`

The service remains deterministic and owns proposal storage, queue APIs, and explicit apply/reject/defer semantics. The LLM proposer only emits `POST /api/curation/proposals` payloads through the existing proposal-only runner. It never calls proposal apply endpoints.

## Provider surface

LLM mode uses an OpenAI-compatible chat-completions endpoint.

CLI flags / environment:

| Purpose | CLI flag | Environment |
| --- | --- | --- |
| model gateway base URL | `--llm-base-url` | `DEN_MEMORY_CURATOR_LLM_BASE_URL`, fallback `OPENAI_BASE_URL` |
| API key | `--llm-api-key` | `DEN_MEMORY_CURATOR_LLM_API_KEY`, fallback `OPENAI_API_KEY` |
| model name | `--llm-model` | `DEN_MEMORY_CURATOR_LLM_MODEL` |
| temperature | `--llm-temperature` | n/a |
| prompt packet byte cap | `--llm-max-packet-bytes` | n/a |

API keys are optional so local LAN gateways that do not require bearer auth can be used.

## Safety boundary

The proposer enforces the curation workflow boundary:

```text
candidate queue → LLM proposal JSON → proposal storage → explicit human/trusted curator apply later
```

It does not:

- create memory entries directly;
- mark candidates promoted/rejected;
- call `/api/curation/proposals/{id}/apply`;
- turn malformed model output into memory truth.

Malformed model output returns an error before storage. No proposal is stored for invalid JSON or structurally invalid proposals.

## Bounded input packet

Each model call receives a bounded packet containing:

- candidate fields, with large body/summary truncated;
- source refs, capped;
- existing proposals, capped;
- queue state;
- registry hints for allowed proposal kinds/statuses.

The full world, raw transcripts, and unrestricted Den docs are not inserted into the model prompt by this implementation.

## Output contract

The model must return strict JSON:

```json
{
  "proposals": [
    {
      "proposal_kind": "promote_candidate",
      "status": "proposed",
      "candidate_ids": [123],
      "reason": "why this action is proposed",
      "confidence": "source_backed",
      "proposed_entry": { "slug": "...", "body_md": "..." },
      "proposed_action": { "action": "promote_candidate", "candidate_id": 123, "payload": { } },
      "evidence_refs": []
    }
  ]
}
```

Supported proposal kinds include normal memory curation actions and non-memory routing:

- `promote_candidate`
- `reject_candidate`
- `split_candidate`
- `merge_candidates`
- `rescope_candidate`
- `relabel_candidate`
- `supersede_entry`
- `knowledge_candidate`
- `doc_update_candidate`
- `defer`

`knowledge_candidate` and `doc_update_candidate` are intentionally only proposal records for now; downstream Knowledge Library / Den doc application is future work.

## Audit metadata

Stored proposals include model metadata:

- `mode: llm`
- `provider: openai_compatible`
- model name;
- bounded prompt packet SHA-256;
- model response SHA-256;
- max packet byte cap.

This gives curator operators enough audit handles without storing full prompt/response dumps in the proposal record.

## Example

```bash
DEN_MEMORY_CURATOR_LLM_BASE_URL=http://127.0.0.1:11434 \
DEN_MEMORY_CURATOR_LLM_MODEL=curator-model \
go run ./cmd/den-memory-curator \
  --base-url http://127.0.0.1:8780 \
  --mode llm \
  --candidate-ids 12 \
  --proposer-identity den-memory-llm-curator \
  --dry-run
```

Remove `--dry-run` to store proposals. Applying proposals remains a separate explicit curator action.
