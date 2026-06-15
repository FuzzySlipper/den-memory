from __future__ import annotations

import json
from dataclasses import dataclass
from typing import Any, Iterable

FORBIDDEN_DEN_MEMORY_TOOLS = {
    "den_memory_recall",
    "den_memory_search",
    "den_memory_read",
    "den_memory_store_candidate",
    "den_memory_capture_event",
    "den_memory_link",
    "den_memory_curate",
}

ALLOWED_AUDIT_SURFACES = {
    "GET /api/audit/export",
    "GET /api/doctor/report",
    "GET /api/observability/summary",
    "GET /api/observability/pending-candidates",
    "GET /api/observability/curation-timeline",
    "GET /api/observability/recall-logs",
    "GET /api/capture-events/{id}",
    "GET /api/curation-events/{id}",
    "GET /api/recall-logs/{id}",
}

AUDITOR_CONSTRAINTS = {
    "den_memory_provider_enabled": False,
    "recall_tools_allowed": False,
    "reads_via_audit_surfaces_only": True,
}

DEFAULT_AUDITOR_PROFILE = {
    "profile_id": "den-memory-independent-auditor",
    "runtime_context": {
        "runtime": "auditor",
        "session_kind": "diagnostic",
        "role": "auditor",
        "mode": "audit",
        "project_id": "den-memory",
    },
    "memory": {
        "provider": "off",
        "den_memory": {
            "enabled": False,
            "auto_recall": False,
            "capture_on_sync": False,
            "prefetch": False,
        },
    },
    "tools": {
        "allow": sorted(ALLOWED_AUDIT_SURFACES),
        "deny": sorted(FORBIDDEN_DEN_MEMORY_TOOLS),
    },
}

REQUIRED_EXPORT_RECORD_TYPES = {
    "metadata",
    "capture_events",
    "memory_candidates",
    "curation_events",
    "recall_logs",
    "doctor",
}


@dataclass(frozen=True)
class AuditorCheck:
    name: str
    ok: bool
    detail: str


def validate_auditor_profile(profile: dict[str, Any], tool_names: Iterable[str]) -> dict[str, Any]:
    """Validate that an auditor runtime is memory-free with respect to Den Memories.

    This is intentionally deterministic and data-only. It does not call Den Memories
    recall/search/provider paths; callers pass a config readback and a tool-list
    readback gathered from the host runtime.
    """

    tools = set(tool_names)
    memory = profile.get("memory", {}) if isinstance(profile.get("memory", {}), dict) else {}
    den_memory = memory.get("den_memory", {}) if isinstance(memory.get("den_memory", {}), dict) else {}
    runtime_context = profile.get("runtime_context", {}) if isinstance(profile.get("runtime_context", {}), dict) else {}

    checks = [
        AuditorCheck(
            "runtime_role_is_auditor",
            runtime_context.get("role") == "auditor" and runtime_context.get("mode") == "audit",
            f"role={runtime_context.get('role')!r} mode={runtime_context.get('mode')!r}",
        ),
        AuditorCheck(
            "den_memory_provider_disabled",
            memory.get("provider") in {None, "off", "none", "disabled"} and den_memory.get("enabled") is False,
            f"memory.provider={memory.get('provider')!r} den_memory.enabled={den_memory.get('enabled')!r}",
        ),
        AuditorCheck(
            "automatic_den_memory_paths_disabled",
            all(den_memory.get(key) is False for key in ("auto_recall", "capture_on_sync", "prefetch")),
            "auto_recall/capture_on_sync/prefetch must all be false",
        ),
        AuditorCheck(
            "forbidden_den_memory_tools_absent",
            not (tools & FORBIDDEN_DEN_MEMORY_TOOLS),
            f"present_forbidden_tools={sorted(tools & FORBIDDEN_DEN_MEMORY_TOOLS)}",
        ),
    ]
    return {
        "ok": all(check.ok for check in checks),
        "checks": [check.__dict__ for check in checks],
        "auditor_constraints": dict(AUDITOR_CONSTRAINTS),
    }


def load_jsonl_records(text: str) -> list[dict[str, Any]]:
    records: list[dict[str, Any]] = []
    for line_number, line in enumerate(text.splitlines(), start=1):
        if not line.strip():
            continue
        try:
            record = json.loads(line)
        except json.JSONDecodeError as exc:
            raise ValueError(f"invalid_jsonl_line:{line_number}:{exc.msg}") from exc
        if not isinstance(record, dict):
            raise ValueError(f"invalid_jsonl_record:{line_number}:not_object")
        records.append(record)
    return records


def audit_export_records(records: list[dict[str, Any]]) -> dict[str, Any]:
    """Produce a compact independent audit report from exported Den Memories data.

    The function accepts already-exported data and never performs recall/search. This
    keeps Den Memories as the object under inspection rather than background memory.
    """

    record_types = {str(record.get("record_type")) for record in records}
    metadata = next((record for record in records if record.get("record_type") == "metadata"), {})
    doctor = [record for record in records if record.get("record_type") == "doctor"]
    recall_logs = [record for record in records if record.get("record_type") == "recall_logs"]
    candidate_records = [record for record in records if record.get("record_type") == "memory_candidates"]

    constraints = metadata.get("auditor_constraints") or {}
    missing_record_types = sorted(REQUIRED_EXPORT_RECORD_TYPES - record_types)
    constraint_mismatches = {
        key: {"expected": expected, "actual": constraints.get(key)}
        for key, expected in AUDITOR_CONSTRAINTS.items()
        if constraints.get(key) is not expected
    }
    contamination_risks: list[dict[str, Any]] = []
    if metadata.get("recall_used") is not False:
        contamination_risks.append({"kind": "export_recall_used_not_false", "severity": "critical", "detail": metadata.get("recall_used")})
    if constraint_mismatches:
        contamination_risks.append({"kind": "auditor_constraints_mismatch", "severity": "critical", "detail": constraint_mismatches})
    if missing_record_types:
        contamination_risks.append({"kind": "missing_required_record_types", "severity": "warning", "detail": missing_record_types})

    doctor_issues = []
    for record in doctor:
        doctor_issues.extend(record.get("issues") or [])
    high_severity_doctor = [
        issue for issue in doctor_issues
        if str(issue.get("severity", "")).lower() in {"critical", "high", "error"}
    ]
    unscoped_candidate_ids = [
        record.get("id") for record in candidate_records
        if not record.get("scope_id") or not record.get("authority_scope_id")
    ]

    return {
        "ok": not any(risk["severity"] == "critical" for risk in contamination_risks),
        "record_type_counts": {record_type: sum(1 for record in records if record.get("record_type") == record_type) for record_type in sorted(record_types)},
        "auditor_constraints": dict(AUDITOR_CONSTRAINTS),
        "contamination_risks": contamination_risks,
        "evidence_handles": {
            "capture_event_ids": [record.get("id") for record in records if record.get("record_type") == "capture_events"],
            "candidate_ids": [record.get("id") for record in candidate_records],
            "curation_event_ids": [record.get("id") for record in records if record.get("record_type") == "curation_events"],
            "recall_log_ids": [record.get("id") for record in recall_logs],
            "recall_packet_ids": [record.get("packet_id") for record in recall_logs if record.get("packet_id")],
        },
        "findings": {
            "doctor_issue_count": len(doctor_issues),
            "high_severity_doctor_issues": high_severity_doctor,
            "unscoped_candidate_ids": unscoped_candidate_ids,
        },
    }


def audit_export_jsonl(text: str) -> dict[str, Any]:
    return audit_export_records(load_jsonl_records(text))


def report_markdown(report: dict[str, Any]) -> str:
    lines = ["# Den Memories independent auditor report", ""]
    lines.append(f"Result: `{'pass' if report.get('ok') else 'fail'}`")
    lines.append("")
    lines.append("## Contamination-risk checks")
    risks = report.get("contamination_risks") or []
    if risks:
        for risk in risks:
            lines.append(f"- **{risk['kind']}** ({risk['severity']}): `{risk.get('detail')}`")
    else:
        lines.append("- none")
    lines.append("")
    lines.append("## Evidence handles")
    for key, values in (report.get("evidence_handles") or {}).items():
        lines.append(f"- {key}: {values}")
    lines.append("")
    lines.append("## Findings")
    findings = report.get("findings") or {}
    lines.append(f"- doctor_issue_count: {findings.get('doctor_issue_count', 0)}")
    lines.append(f"- high_severity_doctor_issues: {findings.get('high_severity_doctor_issues', [])}")
    lines.append(f"- unscoped_candidate_ids: {findings.get('unscoped_candidate_ids', [])}")
    return "\n".join(lines) + "\n"
