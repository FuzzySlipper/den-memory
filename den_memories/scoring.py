from __future__ import annotations

from dataclasses import dataclass
from typing import Any

from .registry import load_scoring_defaults

_DEFAULTS = load_scoring_defaults()

AUTHORITATIVE_MATCH_WEIGHT = float(_DEFAULTS["root_match"]["authoritative_scope_match"])
DISCOVERABLE_MATCH_WEIGHT = float(_DEFAULTS["root_match"]["discoverable_scope_match"])
EXPLICIT_ONLY_MATCH_WEIGHT = float(_DEFAULTS["root_match"]["explicit_only_match"])
SAME_PROJECT_MATCH_WEIGHT = float(_DEFAULTS["root_match"]["same_project_scope_match"])
LINKED_PROJECT_MATCH_WEIGHT = float(_DEFAULTS["root_match"]["linked_project_scope_match"])
CROSS_SCOPE_PENALTY = float(_DEFAULTS["penalties"]["cross_scope_discovered_evidence"])
AUTHORITY_SCOPE_MISMATCH_PENALTY = float(_DEFAULTS["penalties"]["authority_scope_mismatch"])
CLAIM_STRENGTH_POLICY_WEIGHT = float(_DEFAULTS["claim_strength_modifier"]["policy"])
CLAIM_STRENGTH_RECOMMENDATION_WEIGHT = float(_DEFAULTS["claim_strength_modifier"]["recommendation"])
CLAIM_STRENGTH_ASSESSMENT_WEIGHT = float(_DEFAULTS["claim_strength_modifier"]["assessment"])
CLAIM_STRENGTH_OBSERVATION_WEIGHT = float(_DEFAULTS["claim_strength_modifier"]["observation"])
CURATED_STATUS_WEIGHT = float(_DEFAULTS["curation_modifier"]["curated"])
SOURCE_VERIFIED_WEIGHT = float(_DEFAULTS["source_validation_modifier"]["verified"])
SOURCE_BROKEN_PENALTY = float(_DEFAULTS["source_validation_modifier"]["broken"])
EDGE_TRAVERSAL_DECAY = float(_DEFAULTS["traversal"]["edge_traversal_decay"])
SUPERSEDED_STATUS_PENALTY = float(_DEFAULTS["penalties"]["superseded_status"])
DEPRECATED_STATUS_PENALTY = float(_DEFAULTS["penalties"]["deprecated_status"])
ARCHIVED_STATUS_PENALTY = float(_DEFAULTS["penalties"]["archived_status"])

CLAIM_STRENGTH_WEIGHTS = {
    "observation": CLAIM_STRENGTH_OBSERVATION_WEIGHT,
    "assessment": CLAIM_STRENGTH_ASSESSMENT_WEIGHT,
    "recommendation": CLAIM_STRENGTH_RECOMMENDATION_WEIGHT,
    "policy": CLAIM_STRENGTH_POLICY_WEIGHT,
}

STATUS_WEIGHTS = {key: float(value) for key, value in _DEFAULTS["entry_status_modifier"].items()}

CONFIDENCE_WEIGHTS = {key: float(value) for key, value in _DEFAULTS["confidence_modifier"].items()}
SOURCE_VALIDATION_WEIGHTS = {key: float(value) for key, value in _DEFAULTS["source_validation_modifier"].items()}
EDGE_RELATION_WEIGHTS = {key: float(value) for key, value in _DEFAULTS["edge_relation_modifier"].items()}

EXCLUDED_ENTRY_STATUSES = {"superseded", "deprecated", "archived"}


@dataclass(frozen=True)
class RecallContext:
    scope_kind: str = "global"
    scope_id: str | None = None


def authority_matches(item: dict[str, Any], context: RecallContext) -> bool:
    return item.get("authority_scope_kind") == context.scope_kind and item.get("authority_scope_id") == context.scope_id


def is_global_authority(item: dict[str, Any]) -> bool:
    return item.get("authority_scope_kind") == "global" and not item.get("authority_scope_id")


def is_discoverable(item: dict[str, Any], context: RecallContext) -> bool:
    discovery = item.get("discovery_scope", "explicit_only")
    if authority_matches(item, context) or is_global_authority(item):
        return True
    if discovery == "global_discoverable":
        return True
    if discovery == "same_project" and context.scope_kind == item.get("scope_kind") == "project" and context.scope_id == item.get("scope_id"):
        return True
    if discovery == "linked_projects" and context.scope_kind == "project":
        return True
    return False


def authority_label(item: dict[str, Any], context: RecallContext) -> str:
    if authority_matches(item, context) or is_global_authority(item):
        return "authoritative"
    if is_discoverable(item, context):
        return "discovered_evidence"
    return "out_of_scope"


def source_validation_score(source_statuses: list[str]) -> float:
    if not source_statuses:
        return SOURCE_VALIDATION_WEIGHTS.get("unverified", 0.0)
    return sum(SOURCE_VALIDATION_WEIGHTS.get(status, 0.0) for status in source_statuses) / len(source_statuses)


def score_item(item: dict[str, Any], context: RecallContext, source_statuses: list[str] | None = None, *, edge_depth: int = 0, relation: str | None = None) -> dict[str, Any]:
    label = authority_label(item, context)
    if label == "authoritative":
        score = AUTHORITATIVE_MATCH_WEIGHT
    elif label == "discovered_evidence":
        score = DISCOVERABLE_MATCH_WEIGHT + CROSS_SCOPE_PENALTY
    else:
        score = AUTHORITY_SCOPE_MISMATCH_PENALTY
    score += CLAIM_STRENGTH_WEIGHTS.get(item.get("claim_strength", "observation"), 0.0)
    score += STATUS_WEIGHTS.get(item.get("status", "active"), 0.0)
    score += CONFIDENCE_WEIGHTS.get(item.get("confidence", "unverified"), 0.0)
    score += source_validation_score(source_statuses or [])
    if relation:
        score += EDGE_RELATION_WEIGHTS.get(relation, 0.0)
    if edge_depth:
        score *= EDGE_TRAVERSAL_DECAY ** edge_depth
    return {"score": round(score, 3), "authority_label": label}


def scoring_defaults_readback() -> dict[str, Any]:
    return {
        "profile": _DEFAULTS["profile"],
        "description": _DEFAULTS["description"],
        "AUTHORITATIVE_MATCH_WEIGHT": AUTHORITATIVE_MATCH_WEIGHT,
        "DISCOVERABLE_MATCH_WEIGHT": DISCOVERABLE_MATCH_WEIGHT,
        "CROSS_SCOPE_PENALTY": CROSS_SCOPE_PENALTY,
        "CLAIM_STRENGTH_POLICY_WEIGHT": CLAIM_STRENGTH_POLICY_WEIGHT,
        "CLAIM_STRENGTH_RECOMMENDATION_WEIGHT": CLAIM_STRENGTH_RECOMMENDATION_WEIGHT,
        "CLAIM_STRENGTH_ASSESSMENT_WEIGHT": CLAIM_STRENGTH_ASSESSMENT_WEIGHT,
        "CLAIM_STRENGTH_OBSERVATION_WEIGHT": CLAIM_STRENGTH_OBSERVATION_WEIGHT,
        "CURATED_STATUS_WEIGHT": CURATED_STATUS_WEIGHT,
        "SOURCE_VERIFIED_WEIGHT": SOURCE_VERIFIED_WEIGHT,
        "SOURCE_BROKEN_PENALTY": SOURCE_BROKEN_PENALTY,
        "EDGE_TRAVERSAL_DECAY": EDGE_TRAVERSAL_DECAY,
    }
