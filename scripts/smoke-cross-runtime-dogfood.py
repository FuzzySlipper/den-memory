#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
import tempfile
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path
from typing import Any

REPO_ROOT = Path(__file__).resolve().parents[1]
if str(REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(REPO_ROOT))

from integrations.hermes.den_memory_hermes_adapter import DenMemoryHermesProvider, UrllibDenMemoryClient

QUERY = "plugin"


def request_json(base_url: str, method: str, path: str, payload: dict[str, Any] | None = None, params: dict[str, Any] | None = None) -> Any:
    url = f"{base_url.rstrip('/')}{path}"
    if params:
        url += "?" + urllib.parse.urlencode({k: v for k, v in params.items() if v is not None})
    data = None
    headers = {"Accept": "application/json"}
    if payload is not None:
        data = json.dumps(payload).encode("utf-8")
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=10) as response:
            raw = response.read().decode("utf-8")
            return json.loads(raw) if raw else {}
    except urllib.error.HTTPError as exc:
        raise RuntimeError(f"{method} {path} -> HTTP {exc.code}: {exc.read().decode('utf-8', errors='replace')}") from exc


def write_text_from_url(base_url: str, path: str, out: Path) -> None:
    with urllib.request.urlopen(f"{base_url.rstrip('/')}{path}", timeout=10) as response:
        out.write_bytes(response.read())


def source_ref(kind: str, source_id: str, summary: str, project: str = "den-memory") -> dict[str, Any]:
    return {
        "source_kind": kind,
        "source_project_id": project,
        "source_id": source_id,
        "source_locator": {"task_id": 2477, "smoke": True},
        "source_summary": summary,
        "verification_status": "unverified",
    }


def create_entry(base_url: str, slug: str, title: str, body: str, *, scope_id: str | None, authority_scope_kind: str, authority_scope_id: str | None, discovery_scope: str, claim_strength: str, kind: str = "fact", status: str = "active") -> dict[str, Any]:
    return request_json(base_url, "POST", "/api/memory-entries", {
        "slug": slug,
        "title": title,
        "kind": kind,
        "body_md": body,
        "summary": body.split(".")[0],
        "scope_kind": "project" if scope_id else "global",
        "scope_id": scope_id,
        "authority_scope_kind": authority_scope_kind,
        "authority_scope_id": authority_scope_id,
        "discovery_scope": discovery_scope,
        "claim_strength": claim_strength,
        "status": status,
        "created_by": "task-2477-smoke",
        "source_refs": [source_ref("den_task", "2477", f"seed:{slug}")],
    })


def seed_corpus(base_url: str) -> dict[str, Any]:
    entries: dict[str, Any] = {}
    entries["plugin"] = create_entry(
        base_url,
        "plugin-x-project-a-failure-assessment",
        "Plugin X failed in project A",
        "Plugin X failed in project A when the memory adapter reused stale worker isolation state; treat as cautionary discovered evidence outside project A.",
        scope_id="project-a",
        authority_scope_kind="project",
        authority_scope_id="project-a",
        discovery_scope="global_discoverable",
        claim_strength="assessment",
        kind="warning",
    )
    entries["den_core_policy"] = create_entry(
        base_url,
        "den-core-boundary-global-policy",
        "Den Core owns workflow truth",
        "Den Core remains canonical workflow truth; Den Memories stores memory candidates and recall evidence, not task state authority.",
        scope_id=None,
        authority_scope_kind="global",
        authority_scope_id=None,
        discovery_scope="global_discoverable",
        claim_strength="policy",
        kind="policy",
    )
    entries["hermes_invariant"] = create_entry(
        base_url,
        "hermes-prompt-cache-provider-invariant",
        "Hermes prompt cache provider invariant",
        "Hermes memory adapters must keep prompt blocks static; mid-session capture must not mutate cached system prompt text.",
        scope_id="den-memory",
        authority_scope_kind="project",
        authority_scope_id="den-memory",
        discovery_scope="same_project",
        claim_strength="policy",
        kind="policy",
    )
    entries["pi_worker"] = create_entry(
        base_url,
        "pi-crew-worker-memory-isolation-warning",
        "pi-crew worker memory isolation warning",
        "pi-crew worker assignment contexts should default to metadata_only capture unless explicit task-handoff candidate capture is enabled.",
        scope_id="den-memory",
        authority_scope_kind="project",
        authority_scope_id="den-memory",
        discovery_scope="same_project",
        claim_strength="recommendation",
        kind="warning",
    )
    entries["reviewer_risk"] = create_entry(
        base_url,
        "reviewer-risk-path-memory-adapter",
        "Reviewer risk path for memory adapters",
        "Reviewer mode should inspect source refs, scope labels, capture/curation logs, and recall packet reconstruction before approving memory adapter changes.",
        scope_id="den-memory",
        authority_scope_kind="project",
        authority_scope_id="den-memory",
        discovery_scope="same_project",
        claim_strength="recommendation",
        kind="warning",
    )
    entries["implementation_prereq"] = create_entry(
        base_url,
        "implementation-prerequisite-path-memory-adapter",
        "Implementation prerequisite path for memory adapters",
        "Implementation mode should seed source refs and preserve runtime_context before running cross-runtime recall dogfood.",
        scope_id="den-memory",
        authority_scope_kind="project",
        authority_scope_id="den-memory",
        discovery_scope="same_project",
        claim_strength="recommendation",
        kind="fact",
    )
    old = create_entry(
        base_url,
        "old-gateway-direct-delivery-fact",
        "Old gateway direct-delivery fact",
        "Old gateway direct-delivery fact: route all direct agent messages through den-gateway compatibility endpoints.",
        scope_id="den-memory",
        authority_scope_kind="project",
        authority_scope_id="den-memory",
        discovery_scope="same_project",
        claim_strength="observation",
        status="active",
    )
    supersede = request_json(base_url, "POST", "/api/curation/memory-entries/old-gateway-direct-delivery-fact/supersede", {
        "actor_identity": "task-2477-curator",
        "reason": "Intentional superseded stale gateway/direct-delivery fact for dogfood skipped-content smoke",
    })

    noisy = request_json(base_url, "POST", "/api/capture", {
        "runtime": "hermes",
        "actor_identity": "task-2477-noise-fixture",
        "raw_text": "uhhh random transient chatter blah blah not durable memory",
        "title": "Noisy transient capture",
        "summary": "Noisy transient capture",
        "proposed_kind": "note",
        "scope_kind": "global",
        "scope_id": None,
        "authority_scope_kind": "global",
        "authority_scope_id": None,
        "discovery_scope": "explicit_only",
        "claim_strength": "observation",
        "source_refs": [source_ref("manual_note", "noise-1", "intentional rejected noise")],
    })
    noisy_reject = request_json(base_url, "POST", f"/api/curation/candidates/{noisy['candidate_ids'][0]}/reject", {
        "actor_identity": "task-2477-curator",
        "reason": "Intentional noisy candidate rejected by curator for dogfood smoke",
    })
    promoted_capture = request_json(base_url, "POST", "/api/capture", {
        "runtime": "hermes",
        "actor_identity": "task-2477-curator-seed",
        "raw_text": "Curated capture candidate for plugin X postmortem should become a memory entry with source refs.",
        "title": "Curated plugin X postmortem candidate",
        "summary": "Curated plugin X postmortem candidate",
        "proposed_kind": "fact",
        "scope_kind": "project",
        "scope_id": "den-memory",
        "authority_scope_kind": "project",
        "authority_scope_id": "den-memory",
        "discovery_scope": "same_project",
        "claim_strength": "assessment",
        "source_refs": [source_ref("den_task", "2477", "intentional promoted candidate")],
    })
    promoted = request_json(base_url, "POST", f"/api/curation/candidates/{promoted_capture['candidate_ids'][0]}/promote", {
        "actor_identity": "task-2477-curator",
        "reason": "Promote one dogfood candidate to prove strict curation path",
        "slug": "curated-plugin-x-postmortem-candidate",
    })
    intentional_issue = create_entry(
        base_url,
        "intentional-auditor-secret-like-fixture",
        "Intentional auditor secret-like fixture",
        "Intentional doctor fixture contains token marker api_key=TEST_TOKEN_FOR_AUDIT_FLAG so the memory-free auditor has a known issue to flag.",
        scope_id="den-memory",
        authority_scope_kind="project",
        authority_scope_id="den-memory",
        discovery_scope="explicit_only",
        claim_strength="observation",
        kind="warning",
    )
    return {
        "entries": {key: value.get("id") for key, value in entries.items()},
        "supersede_event_id": supersede["curation_event_id"],
        "noisy_capture_event_id": noisy["capture_event_id"],
        "noisy_candidate_id": noisy["candidate_ids"][0],
        "noisy_reject_event_id": noisy_reject["curation_event_id"],
        "promoted_capture_event_id": promoted_capture["capture_event_id"],
        "promoted_candidate_id": promoted_capture["candidate_ids"][0],
        "promote_event_id": promoted["curation_event_id"],
        "promoted_entry_id": promoted["memory_entry"]["id"],
        "intentional_issue_entry_id": intentional_issue["id"],
    }


def recall(base_url: str, runtime: str, agent: str, role: str, mode: str) -> dict[str, Any]:
    return request_json(base_url, "POST", "/api/recall", {
        "query": QUERY,
        "runtime_context": {
            "runtime": runtime,
            "agent_identity": agent,
            "profile_id": agent,
            "session_id": f"task-2477-{runtime}-session",
            "session_kind": "worker_assignment" if runtime == "pi_crew" else "durable_agent",
            "project_id": "den-memory",
            "task_id": 2477,
            "assignment_id": "task-2477-assignment" if runtime == "pi_crew" else None,
            "run_id": "task-2477-run" if runtime == "pi_crew" else None,
            "role": role,
            "audience": [role],
            "mode": mode,
            "source_surface": "smoke",
        },
        "scope_kind": "project",
        "scope_id": "den-memory",
    })


def hermes_adapter_smoke(base_url: str) -> dict[str, Any]:
    provider = DenMemoryHermesProvider(client=UrllibDenMemoryClient(base_url))
    provider.initialize(
        "task-2477-hermes-adapter-session",
        agent_identity="den-mcp-runner",
        profile_id="den-mcp-runner",
        project_id="den-memory",
        task_id=2477,
        role="runner",
        mode="implementation",
        platform="dogfood-smoke",
    )
    recall_result = json.loads(provider.handle_tool_call("den_memory_recall", {"query": QUERY, "budget_tokens": 3000, "include_candidates": False, "scope_kind": "project", "scope_id": "den-memory"}))
    capture_result = json.loads(provider.handle_tool_call("den_memory_capture_event", {
        "raw_text": "Hermes adapter dogfood capture candidate for task 2477",
        "title": "Hermes adapter dogfood capture",
        "summary": "Hermes adapter dogfood capture",
        "event_kind": "manual_note",
    }))
    return {"recall": recall_result, "capture": capture_result, "prompt_block": provider.system_prompt_block()}


def pi_adapter_smoke(base_url: str, pi_crew_repo: Path) -> dict[str, Any]:
    script = f"""
import {{ DenMemoryClient, PiCrewDenMemoryAdapter }} from './pi-memory/src/index.ts';
const client = new DenMemoryClient({{ baseUrl: {json.dumps(base_url)} }});
const adapter = PiCrewDenMemoryAdapter.fromContext(client, {{
  agentIdentity: 'pi-crew-runner', profileId: 'pi-crew-runner', sessionId: 'task-2477-pi-session',
  projectId: 'den-memory', taskId: 2477, assignmentId: 'task-2477-assignment', runId: 'task-2477-run',
  role: 'worker', mode: 'implementation'
}});
const recall = await adapter.callTool('den_memory_recall', {{ query: {json.dumps(QUERY)}, budget_tokens: 3000, include_candidates: false }});
const storedCandidate = await adapter.callTool('den_memory_store_candidate', {{ title: 'pi explicit candidate', body_md: 'pi-crew explicit task-handoff dogfood candidate', summary: 'pi explicit candidate', proposed_kind: 'fact' }});
console.log(JSON.stringify({{ recall, storedCandidate }}));
"""
    completed = subprocess.run(
        ["node", "--input-type=module"],
        input=script,
        text=True,
        capture_output=True,
        cwd=pi_crew_repo,
        check=False,
        timeout=30,
    )
    if completed.returncode != 0:
        raise RuntimeError(f"pi adapter smoke failed: stdout={completed.stdout}\nstderr={completed.stderr}")
    return json.loads(completed.stdout)


def node_slugs(packet: dict[str, Any]) -> list[str]:
    return sorted(str(node.get("slug")) for node in packet.get("included_nodes", []))


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Seed Den Memories v0 corpus and run service/Hermes/pi-crew/auditor dogfood smoke.")
    parser.add_argument("--service-url", default="http://127.0.0.1:8777")
    parser.add_argument("--pi-crew-repo", default="/home/dev/pi-crew")
    parser.add_argument("--output-dir", default="/tmp/den-memory-dogfood-2477")
    args = parser.parse_args(argv)

    base_url = args.service_url.rstrip("/")
    out_dir = Path(args.output_dir)
    out_dir.mkdir(parents=True, exist_ok=True)

    seed = seed_corpus(base_url)
    service_packet = recall(base_url, "manual", "service-smoke", "runner", "implementation")
    hermes = hermes_adapter_smoke(base_url)
    pi = pi_adapter_smoke(base_url, Path(args.pi_crew_repo))

    hermes_packet = hermes["recall"]["data"]
    pi_packet = pi["recall"]["data"]
    service_slugs = node_slugs(service_packet)
    hermes_slugs = node_slugs(hermes_packet)
    pi_slugs = node_slugs(pi_packet)
    semantics_match = service_slugs == hermes_slugs == pi_slugs

    export_jsonl = out_dir / "audit-export.jsonl"
    audit_report_md = out_dir / "independent-audit-report.md"
    write_text_from_url(base_url, "/api/audit/export?format=jsonl", export_jsonl)
    audit = subprocess.run(
        ["go", "run", "./cmd/den-memory-audit", "--export-file", str(export_jsonl), "--markdown-out", str(audit_report_md)],
        text=True,
        capture_output=True,
        cwd=REPO_ROOT,
        check=False,
        timeout=30,
    )
    if audit.returncode != 0:
        raise RuntimeError(f"memory-free audit smoke failed: stdout={audit.stdout}\nstderr={audit.stderr}")
    summary = request_json(base_url, "GET", "/api/observability/summary")
    recall_logs = request_json(base_url, "GET", "/api/observability/recall-logs", params={"limit": 20})
    doctor = request_json(base_url, "GET", "/api/doctor/report")

    dogfood_findings = []
    if not semantics_match:
        dogfood_findings.append({"severity": "high", "kind": "cross_runtime_semantics_mismatch", "service_slugs": service_slugs, "hermes_slugs": hermes_slugs, "pi_slugs": pi_slugs})
    if not any("plugin-x-project-a-failure-assessment" == slug for slug in hermes_slugs):
        dogfood_findings.append({"severity": "medium", "kind": "plugin_x_seed_missing_from_recall"})
    secret_issues = [issue for issue in doctor.get("issues", []) if issue.get("kind") == "secret_like_strings"]
    if not secret_issues:
        dogfood_findings.append({"severity": "medium", "kind": "intentional_audit_fixture_not_flagged"})
    else:
        expected_ref = f"entry:{seed['intentional_issue_entry_id']}"
        unexpected = sorted(ref for issue in secret_issues for ref in issue.get("ids", []) if ref != expected_ref)
        if unexpected:
            dogfood_findings.append({"severity": "medium", "kind": "secret_marker_false_positive_or_extra_issue", "expected_ref": expected_ref, "unexpected_refs": unexpected})
    if not any(item.get("packet_id") == hermes_packet.get("packet_id") for item in recall_logs.get("items", [])):
        dogfood_findings.append({"severity": "low", "kind": "hermes_packet_not_in_recent_recall_log_window"})

    report = {
        "ok": semantics_match and not any(f["severity"] in {"critical", "high"} for f in dogfood_findings),
        "query": QUERY,
        "seed": seed,
        "packets": {
            "service": {"packet_id": service_packet["packet_id"], "recall_log_id": service_packet["audit"]["recall_log_id"], "slugs": service_slugs},
            "hermes": {"packet_id": hermes_packet["packet_id"], "recall_log_id": hermes_packet["audit"]["recall_log_id"], "slugs": hermes_slugs},
            "pi_crew": {"packet_id": pi_packet["packet_id"], "recall_log_id": pi_packet["audit"]["recall_log_id"], "slugs": pi_slugs},
        },
        "captures": {
            "hermes": hermes["capture"],
            "pi_store_candidate": pi.get("storedCandidate") or pi.get("stored_candidate"),
        },
        "semantics_match": semantics_match,
        "observability_summary": summary,
        "doctor_issue_kinds": [issue.get("kind") for issue in doctor.get("issues", [])],
        "audit_export": str(export_jsonl),
        "audit_report": str(audit_report_md),
        "audit_stdout_tail": audit.stdout[-2000:],
        "dogfood_findings": dogfood_findings,
    }
    report_json = out_dir / "dogfood-report.json"
    report_md = out_dir / "dogfood-report.md"
    report_json.write_text(json.dumps(report, indent=2, sort_keys=True), encoding="utf-8")
    report_md.write_text(render_markdown(report), encoding="utf-8")
    print(json.dumps({"ok": report["ok"], "report_json": str(report_json), "report_md": str(report_md), "audit_report": str(audit_report_md), "packets": report["packets"], "seed": seed, "dogfood_findings": dogfood_findings}, indent=2, sort_keys=True))
    return 0 if report["ok"] else 1


def render_markdown(report: dict[str, Any]) -> str:
    lines = ["# Den Memories v0 cross-runtime dogfood report", "", f"Result: `{'pass' if report['ok'] else 'fail'}`", "", f"Query: `{report['query']}`", "", "## Recall packet handles"]
    for runtime, packet in report["packets"].items():
        lines.append(f"- {runtime}: packet `{packet['packet_id']}`, recall_log `{packet['recall_log_id']}`, slugs `{packet['slugs']}`")
    lines.extend(["", "## Capture and curation handles"])
    for key, value in report["seed"].items():
        lines.append(f"- seed.{key}: `{value}`")
    lines.append(f"- hermes capture: `{report['captures']['hermes'].get('data', {}).get('capture_event_id')}`")
    lines.append(f"- pi store candidate ok: `{report['captures']['pi_store_candidate'].get('ok')}`")
    lines.extend(["", "## Independent audit", f"- audit export: `{report['audit_export']}`", f"- audit report: `{report['audit_report']}`", f"- doctor issue kinds: `{report['doctor_issue_kinds']}`"])
    lines.extend(["", "## Dogfood findings"])
    if report["dogfood_findings"]:
        for finding in report["dogfood_findings"]:
            lines.append(f"- {finding}")
    else:
        lines.append("- none: cross-runtime packet semantics matched; intentional doctor/auditor fixture was flagged")
    return "\n".join(lines) + "\n"


if __name__ == "__main__":
    raise SystemExit(main())
