from __future__ import annotations

import json

from fastapi.testclient import TestClient

from den_memories.app import create_app
from den_memories.config import Settings


def client(tmp_path):
    app = create_app(Settings(database_path=tmp_path / "observability.sqlite"))
    return TestClient(app)


def seed_pipeline(c: TestClient) -> dict[str, int | str]:
    capture = c.post("/api/capture", json={
        "runtime": "hermes",
        "actor_identity": "runner-agent",
        "raw_text": "Plugin X failed in project A and needs caution.",
        "title": "Plugin X caution",
        "summary": "Plugin X caution",
        "proposed_kind": "fact",
        "scope_kind": "project",
        "scope_id": "project-a",
        "authority_scope_kind": "project",
        "authority_scope_id": "project-a",
        "discovery_scope": "global_discoverable",
        "claim_strength": "assessment",
        "source_refs": [{"source_kind": "den_task", "source_project_id": "project-a", "source_id": "123", "verification_status": "verified"}],
    })
    assert capture.status_code == 200, capture.text
    candidate_id = capture.json()["candidate"]["id"]

    promote = c.post(f"/api/curation/candidates/{candidate_id}/promote", json={"actor_identity": "curator", "reason": "verified", "slug": "plugin-x-caution"})
    assert promote.status_code == 200, promote.text

    recall = c.post("/api/recall", json={"query": "Plugin", "scope_kind": "project", "scope_id": "project-b", "packet_id": "audit-packet", "actor_identity": "auditor-smoke"})
    assert recall.status_code == 200, recall.text

    duplicate_one = c.post("/api/candidates", json={"title": "Dup one", "body_md": "same duplicate body", "proposed_kind": "fact", "created_by": "fixture"})
    duplicate_two = c.post("/api/candidates", json={"title": "Dup two", "body_md": "same duplicate body", "proposed_kind": "fact", "created_by": "fixture"})
    assert duplicate_one.status_code == 200, duplicate_one.text
    assert duplicate_two.status_code == 200, duplicate_two.text

    secret = c.post("/api/candidates", json={"title": "Secret candidate", "body_md": "contains sk-test-token-looking-value", "proposed_kind": "fact", "created_by": "fixture"})
    assert secret.status_code == 200, secret.text

    broken = c.post("/api/source-refs", json={"target_kind": "memory_candidate", "target_id": duplicate_one.json()["id"], "source_kind": "den_task", "source_project_id": "project-a", "source_id": "broken-ref", "verification_status": "broken", "created_by": "fixture"})
    assert broken.status_code == 200, broken.text
    return {"candidate_id": candidate_id, "packet_id": "audit-packet"}


def test_observability_surfaces_include_counts_and_drilldown_ids(tmp_path):
    with client(tmp_path) as c:
        handles = seed_pipeline(c)

        summary = c.get("/api/observability/summary").json()
        assert summary["counts"]["capture_events"] >= 1
        assert summary["counts"]["curation_events"] >= 1
        assert summary["counts"]["recall_logs"] == 1
        assert summary["recent_ids"]["capture_event_ids"]

        pending = c.get("/api/observability/pending-candidates?scope_kind=global").json()
        assert pending["count"] >= 3
        assert all(item["status"] == "pending" for item in pending["items"])

        timeline = c.get("/api/observability/curation-timeline?action=promote").json()
        assert timeline["count"] >= 1
        assert timeline["items"][0]["action"] == "promote"

        recall_logs = c.get("/api/observability/recall-logs?actor=auditor-smoke&project_id=project-b").json()
        assert recall_logs["count"] == 1
        assert recall_logs["items"][0]["packet_id"] == handles["packet_id"]


def test_doctor_report_is_read_only_and_reports_initial_checks(tmp_path):
    with client(tmp_path) as c:
        seed_pipeline(c)
        before = c.get("/api/observability/summary").json()["counts"]
        report = c.get("/api/doctor/report").json()
        after = c.get("/api/observability/summary").json()["counts"]
        assert report["read_only"] is True
        assert before == after
        issue_kinds = {item["kind"] for item in report["issues"]}
        assert "broken_or_unverified_source_refs" in issue_kinds
        assert "unscoped_candidates" in issue_kinds
        assert "duplicate_candidate_bodies" in issue_kinds
        assert "secret_like_strings" in issue_kinds
        assert all("count" in item and "ids" in item for item in report["issues"])


def test_audit_export_jsonl_and_markdown_are_memory_free_offline_artifacts(tmp_path):
    with client(tmp_path) as c:
        seed_pipeline(c)

        jsonl = c.get("/api/audit/export?format=jsonl").text.strip().splitlines()
        records = [json.loads(line) for line in jsonl]
        assert records[0]["record_type"] == "metadata"
        assert records[0]["recall_used"] is False
        assert {record["record_type"] for record in records} >= {"capture_events", "memory_candidates", "curation_events", "recall_logs", "doctor"}
        assert any(record.get("packet_id") == "audit-packet" for record in records)
        assert any(record.get("record_type") == "doctor" and record["read_only"] is True for record in records)

        # Memory-free audit process can read the export without calling /api/recall.
        offline_counts = {}
        for record in records:
            offline_counts[record["record_type"]] = offline_counts.get(record["record_type"], 0) + 1
        assert offline_counts["capture_events"] >= 1
        assert offline_counts["recall_logs"] == 1

        markdown = c.get("/api/audit/export?format=markdown").text
        assert "# Den Memories v0 audit export" in markdown
        assert "Recall used: `false`" in markdown
        assert "## Drill-down IDs" in markdown


def test_json_backed_filters_apply_before_limit(tmp_path):
    with client(tmp_path) as c:
        matching = c.post("/api/candidates", json={
            "title": "Older source-matching candidate",
            "body_md": "old candidate with den task source",
            "proposed_kind": "fact",
            "source_refs": [{"source_kind": "den_task", "source_id": "old", "verification_status": "verified"}],
            "created_by": "fixture",
        })
        assert matching.status_code == 200, matching.text
        nonmatching = c.post("/api/candidates", json={
            "title": "Newer source-mismatched candidate",
            "body_md": "new candidate with manual source",
            "proposed_kind": "fact",
            "source_refs": [{"source_kind": "manual_note", "source_id": "new", "verification_status": "verified"}],
            "created_by": "fixture",
        })
        assert nonmatching.status_code == 200, nonmatching.text
        filtered = c.get("/api/observability/pending-candidates?source_kind=den_task&limit=1").json()
        assert filtered["count"] == 1
        assert filtered["items"][0]["id"] == matching.json()["id"]

        old = c.post("/api/recall", json={"query": "nothing", "scope_kind": "project", "scope_id": "old-project", "packet_id": "old-project-packet", "actor_identity": "audit"})
        assert old.status_code == 200, old.text
        new = c.post("/api/recall", json={"query": "nothing", "scope_kind": "project", "scope_id": "new-project", "packet_id": "new-project-packet", "actor_identity": "audit"})
        assert new.status_code == 200, new.text
        recall_logs = c.get("/api/observability/recall-logs?project_id=old-project&limit=1").json()
        assert recall_logs["count"] == 1
        assert recall_logs["items"][0]["packet_id"] == "old-project-packet"
        legacy_alias = c.get("/api/recall-logs?project_id=old-project&limit=1").json()
        assert legacy_alias["items"][0]["packet_id"] == "old-project-packet"
