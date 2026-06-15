from __future__ import annotations

import json
from pathlib import Path

from referencing import Registry, Resource
from jsonschema import Draft202012Validator

ROOT = Path(__file__).resolve().parents[1]
CONTRACT = ROOT / "contracts" / "v0"
SCHEMAS = CONTRACT / "schemas"
EXAMPLES = ROOT / "examples" / "v0"


def load_json(path: Path):
    return json.loads(path.read_text(encoding="utf-8"))


def validator_for(schema_name: str) -> Draft202012Validator:
    schema = load_json(SCHEMAS / f"{schema_name}.schema.json")
    registry = Registry().with_resources(
        (loaded["$id"], Resource.from_contents(loaded))
        for loaded in (load_json(p) for p in SCHEMAS.glob("*.schema.json"))
    )
    return Draft202012Validator(schema, registry=registry)


def test_registry_has_required_vocabularies_and_no_duplicates():
    registry = load_json(CONTRACT / "registry.json")
    required = [
        "layers", "scope_kinds", "discovery_scopes", "claim_strengths",
        "capture_decisions", "candidate_statuses", "curation_actions", "edge_relations",
        "source_validation_states", "capture_policy_modes",
    ]
    for key in required:
        values = registry[key]
        assert values, key
        assert len(values) == len(set(values)), key


def test_registry_contains_v0_reconciliation_values():
    registry = load_json(CONTRACT / "registry.json")
    assert "source_evidence" in registry["layers"]
    assert "curated_fact" in registry["layers"]
    assert "global_discoverable" in registry["discovery_scopes"]
    assert "policy" in registry["claim_strengths"]
    assert "permissive_candidates" in registry["capture_policy_modes"]
    assert "errored" in registry["capture_decisions"]
    assert "rescope" in registry["curation_actions"]
    assert "contextual_assessment" in registry["edge_relations"]


def test_scoring_defaults_are_named_and_complete():
    scoring = load_json(CONTRACT / "scoring-defaults.json")
    assert scoring["profile"] == "v0-default"
    assert scoring["root_match"]["authoritative_scope_match"] > scoring["root_match"]["discoverable_scope_match"]
    assert scoring["penalties"]["cross_scope_discovered_evidence"] < 0
    assert scoring["claim_strength_modifier"]["policy"] > scoring["claim_strength_modifier"]["observation"]
    assert scoring["curation_modifier"]["curated"] > scoring["curation_modifier"]["candidate"]
    assert scoring["source_validation_modifier"]["broken"] < scoring["source_validation_modifier"]["verified"]
    assert scoring["readback_required"] is True


def test_examples_validate_against_schemas():
    mapping = {
        "runtime-context.example.json": "runtime-context",
        "source-ref.example.json": "source-ref",
        "candidate.example.json": "candidate",
        "capture-request.example.json": "capture-request",
        "capture-response.example.json": "capture-response",
        "recall-request.example.json": "recall-request",
        "recall-packet.example.json": "recall-packet",
        "audit-export.example.json": "audit-export",
    }
    for example_file, schema_name in mapping.items():
        data = load_json(EXAMPLES / example_file)
        errors = sorted(validator_for(schema_name).iter_errors(data), key=lambda e: list(e.path))
        assert not errors, f"{example_file}: {[e.message for e in errors]}"


def test_unknown_enum_values_are_rejected():
    data = load_json(EXAMPLES / "candidate.example.json")
    data["common"]["layer"] = "captured_observation"
    errors = list(validator_for("candidate").iter_errors(data))
    assert errors
    assert any("not one of" in e.message for e in errors)


def test_independent_auditor_example_is_memory_free():
    data = load_json(EXAMPLES / "audit-export.example.json")
    constraints = data["auditor_constraints"]
    assert constraints == {
        "den_memory_provider_enabled": False,
        "recall_tools_allowed": False,
        "reads_via_audit_surfaces_only": True,
    }


def test_independent_auditor_constraints_are_schema_enforced():
    data = load_json(EXAMPLES / "audit-export.example.json")
    data["auditor_constraints"] = {
        "den_memory_provider_enabled": True,
        "recall_tools_allowed": True,
        "reads_via_audit_surfaces_only": False,
    }
    errors = list(validator_for("audit-export").iter_errors(data))
    assert errors
    assert {tuple(error.path) for error in errors} >= {
        ("auditor_constraints", "den_memory_provider_enabled"),
        ("auditor_constraints", "recall_tools_allowed"),
        ("auditor_constraints", "reads_via_audit_surfaces_only"),
    }
