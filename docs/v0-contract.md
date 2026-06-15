# Den Memories v0 contract artifacts

Task: Den #2467 (`den-memory`)
Contract version: `v0`

This directory locks the vocabulary, scoring defaults, JSON schemas, and example payloads used by the first Den Memories implementation slices. It is intentionally runtime-neutral: Hermes and pi-crew adapters should either generate from these artifacts or faithfully implement compatible types.

## Artifact map

- `contracts/v0/registry.json` — canonical enum/registry values.
- `contracts/v0/scoring-defaults.json` — named recall scoring constants for `v0-default`.
- `contracts/v0/schemas/*.schema.json` — JSON Schema contract artifacts for runtime context, source refs, capture, candidates, recall, and audit export.
- `examples/v0/*.example.json` — valid example payloads used by tests.
- `tests/test_contracts.py` — validation/readback tests.
- `scripts/validate-contracts.py` — stable validation command.

## Canonical vocabulary

The registry rejects unknown values by default. New values require a deliberate contract/registry migration rather than silent API acceptance.

Key vocabularies covered:

- layers: `source_evidence`, `candidate`, `curated_fact`, `identity_profile`, `topic_scene`, `operating_model`, `schema_model`, `intent_hypothesis`
- scope kinds and authority scope kinds
- discovery scopes: `same_project`, `linked_projects`, `global_discoverable`, `explicit_only`
- claim strengths: `observation`, `assessment`, `recommendation`, `policy`
- capture decisions and policy modes
- candidate statuses and curation actions
- source ref kinds and validation states
- topic edge relations and topic-view shapes

## Scoring constants

`contracts/v0/scoring-defaults.json` centralizes named weights for:

- authoritative vs discoverable scope matches;
- cross-scope discovered-evidence penalty;
- claim strength modifiers;
- curation state/confidence/status modifiers;
- source validation state modifiers;
- edge relation modifiers.

These are v0 defaults, not tuned truth. Dogfood task #2477 owns calibration after seed-corpus smokes.

## Independent auditor constraints

The contract includes `audit-export.schema.json` and an example proving the auditor path is memory-free with respect to Den Memories:

- `den_memory_provider_enabled: false`
- `recall_tools_allowed: false`
- `reads_via_audit_surfaces_only: true`

The auditor may inspect Den Memories records as exported data under audit; it must not use Den Memories recall/provider as background memory while auditing Den Memories.

## Observability surfaces included

Schemas and examples include capture, curation/curated candidate state, recall packet/log handles, provenance, skipped reasons, warnings, and audit export. That keeps permissive capture inspectable and curation strict from day one.

## Validation

```bash
python3 scripts/validate-contracts.py
```

Expected: pytest passes all contract/example/enum tests.
