from __future__ import annotations

from fastapi.testclient import TestClient

from den_memories.app import create_app
from den_memories.config import Settings


def client(tmp_path):
    app = create_app(Settings(database_path=tmp_path / "capture.sqlite"))
    return TestClient(app)


def test_capture_captured_creates_pending_candidate_with_source_scope_and_event(tmp_path):
    with client(tmp_path) as c:
        res = c.post("/api/capture", json={
            "runtime": "hermes",
            "actor_identity": "durable-runner",
            "actor_role": "durable_agent",
            "event_kind": "task_message",
            "raw_text": "Plugin X failed twice in project A; treat it as risky evidence there.",
            "summary": "Plugin X failure evidence",
            "proposed_kind": "warning",
            "scope_kind": "project",
            "scope_id": "project-a",
            "authority_scope_kind": "project",
            "authority_scope_id": "project-a",
            "discovery_scope": "global_discoverable",
            "claim_strength": "assessment",
            "source_refs": [{"source_kind": "den_task", "source_id": "2470"}],
        })
        assert res.status_code == 200, res.text
        body = res.json()
        assert body["decision"] == "captured"
        assert body["candidate_ids"]
        candidate = body["candidate"]
        assert candidate["status"] == "pending"
        assert candidate["layer"] == "candidate"
        assert candidate["scope_kind"] == "project"
        assert candidate["scope_id"] == "project-a"
        assert candidate["authority_scope_kind"] == "project"
        assert candidate["discovery_scope"] == "global_discoverable"
        assert candidate["claim_strength"] == "assessment"
        assert candidate["source_refs"] == [{"source_id": "2470", "source_kind": "den_task"}]

        event = c.get(f"/api/capture-events/{body['capture_event_id']}").json()
        assert event["decision"] == "captured"
        assert event["candidate_ids"] == body["candidate_ids"]
        assert event["source_refs"] == [{"source_id": "2470", "source_kind": "den_task"}]
        assert event["proposed_scope_kind"] == "project"
        assert event["proposed_scope_id"] == "project-a"


def test_capture_logs_ignored_filtered_and_errored_decisions(tmp_path):
    with client(tmp_path) as c:
        ignored = c.post("/api/capture", json={
            "runtime": "hermes",
            "actor_role": "worker",
            "actor_identity": "bounded-worker",
            "raw_text": "metadata only worker text",
            "scope_kind": "task",
            "scope_id": "2470",
        }).json()
        assert ignored["decision"] == "ignored"
        assert ignored["reason"] == "metadata_only"

        auditor = c.post("/api/capture", json={
            "runtime": "hermes",
            "actor_role": "auditor",
            "actor_identity": "independent-auditor",
            "raw_text": "auditor should not capture through Den Memories",
        }).json()
        assert auditor["decision"] == "ignored"
        assert auditor["reason"] == "capture_mode_off"

        filtered = c.post("/api/capture", json={
            "runtime": "hermes",
            "actor_identity": "durable-runner",
            "raw_text": "BEGIN PRIVATE KEY\nnot a real key but secret-like",
            "proposed_kind": "warning",
        }).json()
        assert filtered["decision"] == "filtered"
        assert filtered["reason"] == "secret_like_content_filtered"

        errored = c.post("/api/capture", json={
            "runtime": "hermes",
            "actor_identity": "durable-runner",
            "raw_text": "error path",
            "capture_mode": "definitely_not_valid",
        }).json()
        assert errored["decision"] == "errored"
        assert errored["reason"] == "invalid_capture_mode"

        recent = c.get("/api/capture-events/recent?limit=10").json()["items"]
        decisions = {event["decision"] for event in recent}
        assert {"ignored", "filtered", "errored"} <= decisions
        assert c.get("/api/candidates").json()["items"] == []


def test_capture_cannot_promote_or_create_policy_strength_authority(tmp_path):
    with client(tmp_path) as c:
        filtered = c.post("/api/capture", json={
            "runtime": "hermes",
            "actor_identity": "durable-runner",
            "raw_text": "This should not become global law.",
            "title": "Attempted policy capture",
            "proposed_kind": "policy",
            "claim_strength": "policy",
            "status": "promoted",
            "layer": "curated_fact",
            "capture_mode": "permissive_candidates",
        })
        assert filtered.status_code == 200, filtered.text
        assert filtered.json()["decision"] == "filtered"
        assert filtered.json()["reason"] == "policy_strength_capture_filtered"
        assert c.get("/api/candidates").json()["items"] == []
        assert c.post("/api/memory-entries/search", json={"query": "global", "limit": 5}).json()["items"] == []

        direct_promoted_candidate = c.post("/api/candidates", json={
            "title": "Bad direct candidate",
            "body_md": "attempted promoted candidate",
            "summary": "attempt",
            "proposed_kind": "fact",
            "status": "promoted",
            "created_by": "test",
        })
        assert direct_promoted_candidate.status_code == 400
        assert direct_promoted_candidate.json()["detail"] == "candidate_create_status_must_be_pending"

        direct_policy_candidate = c.post("/api/candidates", json={
            "title": "Bad policy candidate",
            "body_md": "attempted policy candidate",
            "summary": "attempt",
            "proposed_kind": "policy",
            "claim_strength": "policy",
            "created_by": "test",
        })
        assert direct_policy_candidate.status_code == 400
        assert direct_policy_candidate.json()["detail"] == "candidate_create_cannot_use_policy_claim_strength"
