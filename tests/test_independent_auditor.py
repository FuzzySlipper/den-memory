from __future__ import annotations

import json

from fastapi.testclient import TestClient

from den_memories.app import create_app
from den_memories.auditor import (
    AUDITOR_CONSTRAINTS,
    DEFAULT_AUDITOR_PROFILE,
    audit_export_jsonl,
    report_markdown,
    validate_auditor_profile,
)
from den_memories.config import Settings
from tests.test_observability_api import seed_pipeline


def client(tmp_path):
    app = create_app(Settings(database_path=tmp_path / "auditor.sqlite"))
    return TestClient(app)


def test_default_auditor_profile_disables_den_memory_provider_and_tools():
    profile = DEFAULT_AUDITOR_PROFILE
    allowed_tools = ["GET /api/audit/export", "GET /api/doctor/report", "GET /api/observability/recall-logs"]

    result = validate_auditor_profile(profile, allowed_tools)

    assert result["ok"] is True
    assert result["auditor_constraints"] == AUDITOR_CONSTRAINTS
    assert all(check["ok"] for check in result["checks"])


def test_profile_validation_rejects_recall_tool_or_enabled_provider():
    unsafe_profile = {
        **DEFAULT_AUDITOR_PROFILE,
        "memory": {"provider": "den_memory", "den_memory": {"enabled": True, "auto_recall": True, "capture_on_sync": False, "prefetch": False}},
    }

    result = validate_auditor_profile(unsafe_profile, ["den_memory_recall", "GET /api/audit/export"])

    assert result["ok"] is False
    failed = {check["name"] for check in result["checks"] if not check["ok"]}
    assert "den_memory_provider_disabled" in failed
    assert "automatic_den_memory_paths_disabled" in failed
    assert "forbidden_den_memory_tools_absent" in failed


def test_audit_export_contains_memory_free_constraints_and_offline_report(tmp_path):
    with client(tmp_path) as c:
        seed_pipeline(c)
        response = c.get("/api/audit/export?format=jsonl")
        assert response.status_code == 200, response.text

        first = json.loads(response.text.splitlines()[0])
        assert first["record_type"] == "metadata"
        assert first["recall_used"] is False
        assert first["auditor_constraints"] == AUDITOR_CONSTRAINTS

        report = audit_export_jsonl(response.text)
        assert report["ok"] is True
        assert report["contamination_risks"] == []
        assert report["evidence_handles"]["recall_packet_ids"] == ["audit-packet"]
        assert report["findings"]["doctor_issue_count"] >= 1
        rendered = report_markdown(report)
        assert "# Den Memories independent auditor report" in rendered
        assert "Result: `pass`" in rendered
        assert "audit-packet" in rendered


def test_offline_auditor_flags_contaminated_export_metadata():
    contaminated = "\n".join([
        json.dumps({
            "record_type": "metadata",
            "format": "den-memories-v0-audit-export",
            "recall_used": True,
            "auditor_constraints": {
                "den_memory_provider_enabled": True,
                "recall_tools_allowed": True,
                "reads_via_audit_surfaces_only": False,
            },
        }),
        json.dumps({"record_type": "doctor", "read_only": True, "issues": []}),
    ])

    report = audit_export_jsonl(contaminated)

    assert report["ok"] is False
    risk_kinds = {risk["kind"] for risk in report["contamination_risks"]}
    assert "export_recall_used_not_false" in risk_kinds
    assert "auditor_constraints_mismatch" in risk_kinds
    assert "missing_required_record_types" in risk_kinds
