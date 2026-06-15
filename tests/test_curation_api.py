from __future__ import annotations

from fastapi.testclient import TestClient

from den_memories.app import create_app
from den_memories.config import Settings


def client(tmp_path):
    app = create_app(Settings(database_path=tmp_path / "curation.sqlite"))
    return TestClient(app)


def capture_candidate(c: TestClient, *, text: str = "Candidate body", title: str = "Candidate", source_id: str = "2471") -> dict:
    res = c.post("/api/capture", json={
        "runtime": "hermes",
        "actor_identity": "capture-agent",
        "raw_text": text,
        "title": title,
        "summary": title,
        "proposed_kind": "fact",
        "scope_kind": "project",
        "scope_id": "den-memory",
        "authority_scope_kind": "project",
        "authority_scope_id": "den-memory",
        "discovery_scope": "same_project",
        "claim_strength": "observation",
        "source_refs": [{"source_kind": "den_task", "source_id": source_id, "source_project_id": "den-memory"}],
    })
    assert res.status_code == 200, res.text
    assert res.json()["decision"] == "captured"
    return res.json()["candidate"]


def test_claim_reject_and_queryable_rejection_reason(tmp_path):
    with client(tmp_path) as c:
        candidate = capture_candidate(c, text="noise candidate", title="Noise")
        claim = c.post(f"/api/curation/candidates/{candidate['id']}/claim", json={"actor_identity": "curator", "reason": "begin review"})
        assert claim.status_code == 200, claim.text
        assert claim.json()["candidate"]["status"] == "claimed"

        reject = c.post(f"/api/curation/candidates/{candidate['id']}/reject", json={"actor_identity": "curator", "reason": "noise: duplicate chatter"})
        assert reject.status_code == 200, reject.text
        assert reject.json()["candidate"]["status"] == "rejected"

        events = c.get("/api/curation-events?action=reject&reason_contains=duplicate").json()["items"]
        assert len(events) == 1
        assert events[0]["action"] == "reject"
        assert events[0]["actor_identity"] == "curator"
        assert events[0]["reason"] == "noise: duplicate chatter"
        assert events[0]["before"]["status"] == "claimed"
        assert events[0]["after"]["status"] == "rejected"


def test_promote_requires_actor_reason_and_preserves_source_refs(tmp_path):
    with client(tmp_path) as c:
        candidate = capture_candidate(c, text="Curated fact from task source", title="Curated fact", source_id="2471")
        missing_actor = c.post(f"/api/curation/candidates/{candidate['id']}/promote", json={"reason": "missing actor"})
        assert missing_actor.status_code == 400
        assert missing_actor.json()["detail"] == "actor_identity_required"
        missing_reason = c.post(f"/api/curation/candidates/{candidate['id']}/promote", json={"actor_identity": "curator"})
        assert missing_reason.status_code == 400
        assert missing_reason.json()["detail"] == "reason_required"

        promoted = c.post(f"/api/curation/candidates/{candidate['id']}/promote", json={
            "actor_identity": "curator",
            "reason": "verified against source task",
            "slug": "curated-fact",
            "claim_strength": "assessment",
        })
        assert promoted.status_code == 200, promoted.text
        body = promoted.json()
        assert body["candidate"]["status"] == "promoted"
        entry = body["memory_entry"]
        assert entry["slug"] == "curated-fact"
        assert entry["status"] == "active"
        assert entry["curation_state"] == "curated"
        assert entry["claim_strength"] == "assessment"
        assert body["source_ref_ids"]

        refs = c.get(f"/api/source-refs?target_kind=memory_entry&target_id={entry['id']}").json()["items"]
        assert refs[0]["source_kind"] == "den_task"
        assert refs[0]["source_id"] == "2471"

        event = c.get(f"/api/curation-events/{body['curation_event_id']}").json()
        assert event["action"] == "promote"
        assert event["reason"] == "verified against source task"
        assert event["before"]["status"] == "pending"
        assert event["after"]["memory_entry"]["slug"] == "curated-fact"
        assert event["after"]["source_ref_ids"] == body["source_ref_ids"]


def test_split_merge_rescope_relabel_and_supersede_events(tmp_path):
    with client(tmp_path) as c:
        candidate = capture_candidate(c, text="First fact. Second fact.", title="Two facts")
        split = c.post(f"/api/curation/candidates/{candidate['id']}/split", json={
            "actor_identity": "curator",
            "reason": "two distinct facts",
            "fragments": [
                {"title": "First fact", "body_md": "First fact", "summary": "First"},
                {"title": "Second fact", "body_md": "Second fact", "summary": "Second"},
            ],
        })
        assert split.status_code == 200, split.text
        split_ids = split.json()["split_candidate_ids"]
        assert len(split_ids) == 2
        assert split.json()["candidate"]["status"] == "needs_split"
        split_event = c.get(f"/api/curation-events/{split.json()['curation_event_id']}").json()
        assert len(split_event["after"]["split_candidates"]) == 2
        assert {item["id"] for item in split_event["after"]["split_candidates"]} == set(split_ids)

        relabel = c.post(f"/api/curation/candidates/{split_ids[0]}/relabel", json={
            "actor_identity": "curator",
            "reason": "classify as warning",
            "proposed_kind": "warning",
            "summary": "First warning",
        })
        assert relabel.status_code == 200, relabel.text
        assert relabel.json()["candidate"]["proposed_kind"] == "warning"

        rescope = c.post(f"/api/curation/candidates/{split_ids[0]}/rescope", json={
            "actor_identity": "curator",
            "reason": "project-local authority with global discoverability",
            "scope_kind": "project",
            "scope_id": "den-memory",
            "authority_scope_kind": "project",
            "authority_scope_id": "den-memory",
            "discovery_scope": "global_discoverable",
            "claim_strength": "assessment",
        })
        assert rescope.status_code == 200, rescope.text
        body = rescope.json()["candidate"]
        assert body["authority_scope_kind"] == "project"
        assert body["discovery_scope"] == "global_discoverable"
        assert body["claim_strength"] == "assessment"

        merge = c.post("/api/curation/candidates/merge", json={
            "actor_identity": "curator",
            "reason": "merge after split smoke",
            "candidate_ids": split_ids,
            "title": "Merged fact",
        })
        assert merge.status_code == 200, merge.text
        merged_id = merge.json()["merged_candidate"]["id"]
        assert c.get(f"/api/candidates/{merged_id}").json()["status"] == "pending"
        merge_event = c.get(f"/api/curation-events/{merge.json()['curation_event_id']}").json()
        assert merge_event["after"]["merged_candidate"]["id"] == merged_id
        assert {item["status"] for item in merge_event["after"]["source_candidates"]} == {"needs_merge"}

        promoted = c.post(f"/api/curation/candidates/{merged_id}/promote", json={"actor_identity": "curator", "reason": "promote merged", "slug": "merged-fact"}).json()
        superseded = c.post("/api/curation/memory-entries/merged-fact/supersede", json={"actor_identity": "curator", "reason": "newer memory replaces this"})
        assert superseded.status_code == 200, superseded.text
        assert superseded.json()["memory_entry"]["status"] == "superseded"

        actions = [item["action"] for item in c.get("/api/curation-events?limit=20").json()["items"]]
        for action in ["split", "relabel", "rescope", "merge", "promote", "supersede"]:
            assert action in actions


def test_link_and_unlink_curation_aliases_write_events(tmp_path):
    with client(tmp_path) as c:
        a = c.post("/api/topic-nodes", json={"slug": "a", "title": "A", "node_type": "concept", "created_by": "test"}).json()
        b = c.post("/api/topic-nodes", json={"slug": "b", "title": "B", "node_type": "warning", "created_by": "test"}).json()
        missing = c.post("/api/curation/topic-edges/link", json={
            "from_node_id": a["id"],
            "to_node_id": b["id"],
            "relation": "warning",
        })
        assert missing.status_code == 400
        assert missing.json()["detail"] == "actor_identity_required"
        edge = c.post("/api/curation/topic-edges/link", json={
            "actor_identity": "curator",
            "reason": "connect risk",
            "from_node_id": a["id"],
            "to_node_id": b["id"],
            "relation": "warning",
        })
        assert edge.status_code == 200, edge.text
        unlink = c.post(f"/api/curation/topic-edges/{edge.json()['id']}/unlink", json={"actor_identity": "curator", "reason": "remove bad edge"})
        assert unlink.status_code == 200, unlink.text
        events = c.get("/api/curation-events?limit=5").json()["items"]
        actions = [item["action"] for item in events]
        assert "link" in actions
        assert "unlink" in actions
        assert next(item for item in events if item["action"] == "link")["reason"] == "connect risk"
        assert next(item for item in events if item["action"] == "unlink")["reason"] == "remove bad edge"
