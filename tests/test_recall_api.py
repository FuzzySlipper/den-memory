from __future__ import annotations

from fastapi.testclient import TestClient
from referencing import Registry, Resource
from jsonschema.validators import validator_for
import json
from pathlib import Path

from den_memories.app import create_app
from den_memories.config import Settings
from den_memories.scoring import (
    AUTHORITATIVE_MATCH_WEIGHT,
    CLAIM_STRENGTH_ASSESSMENT_WEIGHT,
    CROSS_SCOPE_PENALTY,
    DISCOVERABLE_MATCH_WEIGHT,
    RecallContext,
    score_item,
    scoring_defaults_readback,
)


def assert_recall_packet_contract(packet: dict) -> None:
    root = Path(__file__).resolve().parents[1]
    schema = json.loads((root / "contracts/v0/schemas/recall-packet.schema.json").read_text())
    source_ref = json.loads((root / "contracts/v0/schemas/source-ref.schema.json").read_text())
    registry = Registry().with_resource("https://den.local/schemas/den-memories/v0/source-ref.schema.json", Resource.from_contents(source_ref)).with_resource("source-ref.schema.json", Resource.from_contents(source_ref))
    Validator = validator_for(schema)
    Validator.check_schema(schema)
    Validator(schema, registry=registry).validate(packet)


def client(tmp_path):
    app = create_app(Settings(database_path=tmp_path / "recall.sqlite"))
    return TestClient(app)


def create_promoted_entry(c: TestClient, *, slug: str, title: str, body: str, scope_id: str, authority_scope_id: str | None = None, discovery_scope: str = "same_project", claim_strength: str = "assessment", source_id: str = "2472") -> dict:
    cap = c.post("/api/capture", json={
        "runtime": "hermes",
        "actor_identity": "capture-agent",
        "raw_text": body,
        "title": title,
        "summary": title,
        "proposed_kind": "fact",
        "scope_kind": "project",
        "scope_id": scope_id,
        "authority_scope_kind": "project" if authority_scope_id else "global",
        "authority_scope_id": authority_scope_id,
        "discovery_scope": discovery_scope,
        "claim_strength": claim_strength,
        "source_refs": [{"source_kind": "den_task", "source_id": source_id, "source_project_id": "den-memory", "verification_status": "verified"}],
    })
    assert cap.status_code == 200, cap.text
    candidate_id = cap.json()["candidate"]["id"]
    promote = c.post(f"/api/curation/candidates/{candidate_id}/promote", json={
        "actor_identity": "curator",
        "reason": "verified seed",
        "slug": slug,
        "claim_strength": claim_strength,
    })
    assert promote.status_code == 200, promote.text
    return promote.json()["memory_entry"]


def test_scoring_constants_are_named_and_authoritative_beats_discoverable():
    readback = scoring_defaults_readback()
    assert readback["AUTHORITATIVE_MATCH_WEIGHT"] == AUTHORITATIVE_MATCH_WEIGHT
    assert readback["DISCOVERABLE_MATCH_WEIGHT"] == DISCOVERABLE_MATCH_WEIGHT
    assert readback["CROSS_SCOPE_PENALTY"] == CROSS_SCOPE_PENALTY

    context = RecallContext(scope_kind="project", scope_id="project-a")
    authoritative = score_item({
        "authority_scope_kind": "project",
        "authority_scope_id": "project-a",
        "discovery_scope": "same_project",
        "claim_strength": "assessment",
        "status": "active",
        "confidence": "source_backed",
    }, context, ["verified"])
    discoverable = score_item({
        "authority_scope_kind": "project",
        "authority_scope_id": "project-b",
        "discovery_scope": "global_discoverable",
        "claim_strength": "assessment",
        "status": "active",
        "confidence": "source_backed",
    }, context, ["verified"])
    assert authoritative["authority_label"] == "authoritative"
    assert discoverable["authority_label"] == "discovered_evidence"
    assert authoritative["score"] > discoverable["score"]
    assert discoverable["score"] >= DISCOVERABLE_MATCH_WEIGHT + CROSS_SCOPE_PENALTY + CLAIM_STRENGTH_ASSESSMENT_WEIGHT


def test_recall_labels_project_scoped_cross_scope_evidence_and_logs_packet(tmp_path):
    with client(tmp_path) as c:
        create_promoted_entry(c, slug="plugin-x-project-a", title="Plugin X project A warning", body="Plugin X failed repeatedly in project A.", scope_id="project-a", authority_scope_id="project-a", discovery_scope="global_discoverable", claim_strength="assessment")
        create_promoted_entry(c, slug="den-core-policy", title="Den Core global policy", body="Den Core owns workflow truth.", scope_id="den-memory", authority_scope_id=None, discovery_scope="global_discoverable", claim_strength="recommendation")

        packet = c.post("/api/recall", json={"query": "Plugin", "scope_kind": "project", "scope_id": "project-b", "packet_id": "packet-plugin-b", "actor_identity": "tester"})
        assert packet.status_code == 200, packet.text
        body = packet.json()
        assert_recall_packet_contract(body)
        item = next(entry for entry in body["included_nodes"] if entry["slug"] == "plugin-x-project-a")
        assert item["interpretation"] == "discovered_evidence"
        assert item["authority_scope_id"] == "project-a"
        assert item["scope_id"] == "project-a"
        assert body["provenance"][0]["source_id"] == "2472"
        assert any("discovered cross-scope evidence" in warning for warning in body["warnings"])
        assert "## Included" in body["packet_md"]
        assert body["audit"]["recall_log_id"]

        log = c.get("/api/recall-logs/by-packet/packet-plugin-b")
        assert log.status_code == 200, log.text
        assert log.json()["packet_id"] == "packet-plugin-b"
        assert log.json()["packet_id"] == "packet-plugin-b"
        assert log.json()["audit"]["recall_log_id"] == body["audit"]["recall_log_id"]
        assert log.json()["warnings"] == body["warnings"]


def test_recall_excludes_superseded_content_with_skipped_reason(tmp_path):
    with client(tmp_path) as c:
        entry = create_promoted_entry(c, slug="old-gateway-fact", title="Gateway old fact", body="Gateway old fact should disappear.", scope_id="den-memory", authority_scope_id="den-memory", discovery_scope="same_project")
        supersede = c.post("/api/curation/memory-entries/old-gateway-fact/supersede", json={"actor_identity": "curator", "reason": "old gateway fact superseded"})
        assert supersede.status_code == 200, supersede.text
        packet = c.post("/api/recall", json={"query": "Gateway", "scope_kind": "project", "scope_id": "den-memory", "packet_id": "packet-skip"}).json()
        assert not any(item.get("slug") == "old-gateway-fact" for item in packet["included_nodes"])
        skipped = next(item for item in packet["skipped"] if item["node_slug"] == "old-gateway-fact")
        assert skipped["reason"] == "status:superseded"


def test_topic_view_traversal_includes_typed_edge_nodes(tmp_path):
    with client(tmp_path) as c:
        root = c.post("/api/topic-nodes", json={
            "slug": "implementation-root",
            "title": "Implementation root",
            "node_type": "concept",
            "summary": "Plugin implementation path",
            "scope_kind": "project",
            "scope_id": "den-memory",
            "authority_scope_kind": "project",
            "authority_scope_id": "den-memory",
            "discovery_scope": "same_project",
            "claim_strength": "recommendation",
            "created_by": "test",
        }).json()
        risk = c.post("/api/topic-nodes", json={
            "slug": "implementation-risk",
            "title": "Implementation risk",
            "node_type": "warning",
            "summary": "Review risk path",
            "scope_kind": "project",
            "scope_id": "den-memory",
            "authority_scope_kind": "project",
            "authority_scope_id": "den-memory",
            "discovery_scope": "same_project",
            "claim_strength": "assessment",
            "created_by": "test",
        }).json()
        c.post("/api/topic-edges", json={"from_node_id": root["id"], "to_node_id": risk["id"], "relation": "warning", "created_by": "test"})
        view = c.post("/api/topic-views", json={
            "slug": "implementation-view",
            "title": "Implementation view",
            "root_node_id": root["id"],
            "view_type": "implementation_path",
            "mode": "implementation",
            "include_relations": ["warning"],
            "max_depth": 1,
            "created_by": "test",
        })
        assert view.status_code == 200, view.text

        packet = c.post("/api/recall", json={"query": "Plugin", "scope_kind": "project", "scope_id": "den-memory", "topic_view_slug": "implementation-view", "packet_id": "packet-view"}).json()
        assert_recall_packet_contract(packet)
        slugs = {item["slug"] for item in packet["included_nodes"] if item["kind"] == "topic_node"}
        assert {"implementation-root", "implementation-risk"} <= slugs
        risk_item = next(item for item in packet["included_nodes"] if item.get("slug") == "implementation-risk")
        assert risk_item["edge_relation"] == "warning"
        assert risk_item["edge_depth"] == 1
