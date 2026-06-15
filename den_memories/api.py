from __future__ import annotations

import json
import sqlite3
from typing import Any

from fastapi import APIRouter, HTTPException, Query, Request, Response

from .scoring import EXCLUDED_ENTRY_STATUSES, RecallContext, score_item, scoring_defaults_readback

router = APIRouter(prefix="/api")

JSON_FIELDS = {
    "audience_json",
    "tags_json",
    "source_refs_json",
    "source_locator_json",
    "candidate_ids_json",
    "before_json",
    "after_json",
    "request_json",
    "root_node_ids_json",
    "included_node_ids_json",
    "skipped_json",
    "warnings_json",
    "packet_json",
    "include_relations_json",
    "exclude_relations_json",
    "default_unroll_policy_json",
}


def db(request: Request) -> sqlite3.Connection:
    return request.app.state.db


def decode_row(row: sqlite3.Row | None) -> dict[str, Any] | None:
    if row is None:
        return None
    result = dict(row)
    for key in list(result):
        if key in JSON_FIELDS and isinstance(result[key], str):
            try:
                result[key.removesuffix("_json")] = json.loads(result[key])
            except json.JSONDecodeError:
                result[key.removesuffix("_json")] = result[key]
            del result[key]
    return result


def rows(cur: sqlite3.Cursor) -> list[dict[str, Any]]:
    return [decode_row(row) for row in cur.fetchall()]


def j(value: Any, default: Any = None) -> str:
    if value is None:
        value = default if default is not None else []
    return json.dumps(value, separators=(",", ":"), sort_keys=True)


def require(row: sqlite3.Row | None, label: str) -> sqlite3.Row:
    if row is None:
        raise HTTPException(status_code=404, detail=f"{label}_not_found")
    return row


def execute_insert(conn: sqlite3.Connection, sql: str, params: tuple[Any, ...]) -> int:
    try:
        cur = conn.execute(sql, params)
        conn.commit()
        return int(cur.lastrowid)
    except sqlite3.IntegrityError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc


def execute_update(conn: sqlite3.Connection, sql: str, params: tuple[Any, ...]) -> None:
    try:
        conn.execute(sql, params)
        conn.commit()
    except sqlite3.IntegrityError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc


def log_curation(conn: sqlite3.Connection, *, action: str, actor: str, reason: str = "", candidate_id: int | None = None, memory_entry_id: int | None = None, node_id: int | None = None, edge_id: int | None = None, before: Any = None, after: Any = None) -> int:
    return execute_insert(
        conn,
        """INSERT INTO curation_events(candidate_id,memory_entry_id,node_id,edge_id,action,actor_identity,reason,before_json,after_json)
           VALUES (?,?,?,?,?,?,?,?,?)""",
        (candidate_id, memory_entry_id, node_id, edge_id, action, actor, reason, j(before, {}), j(after, {})),
    )


def refresh_entry_fts(conn: sqlite3.Connection, rowid: int, payload: dict[str, Any]) -> None:
    conn.execute("DELETE FROM memory_entries_fts WHERE rowid=?", (rowid,))
    conn.execute("INSERT INTO memory_entries_fts(rowid,slug,title,summary,body_md,tags_json) VALUES (?,?,?,?,?,?)", (rowid, payload.get("slug",""), payload.get("title",""), payload.get("summary",""), payload.get("body_md",""), j(payload.get("tags", []))))


def refresh_candidate_fts(conn: sqlite3.Connection, rowid: int, payload: dict[str, Any]) -> None:
    conn.execute("DELETE FROM memory_candidates_fts WHERE rowid=?", (rowid,))
    conn.execute("INSERT INTO memory_candidates_fts(rowid,title,summary,body_md,proposed_kind) VALUES (?,?,?,?,?)", (rowid, payload.get("title",""), payload.get("summary",""), payload.get("body_md",""), payload.get("proposed_kind","")))


def refresh_node_fts(conn: sqlite3.Connection, rowid: int, payload: dict[str, Any]) -> None:
    conn.execute("DELETE FROM topic_nodes_fts WHERE rowid=?", (rowid,))
    conn.execute("INSERT INTO topic_nodes_fts(rowid,slug,title,summary) VALUES (?,?,?,?)", (rowid, payload.get("slug",""), payload.get("title",""), payload.get("summary","")))



CAPTURE_MODES = {"off", "metadata_only", "permissive_candidates", "curated_manual_only"}
CAPTURE_DECISIONS = {"captured", "ignored", "filtered", "errored"}
SECRET_MARKERS = ("BEGIN PRIVATE KEY", "BEGIN RSA PRIVATE KEY", "api_key=", "xoxb-", "ghp_", "sk-")


def default_capture_mode(runtime: str, actor_role: str | None = None) -> str:
    role = (actor_role or "").lower()
    runtime = (runtime or "manual").lower()
    if role == "auditor" or runtime == "audit":
        return "off"
    if role == "worker" or runtime in {"worker", "subagent"}:
        return "metadata_only"
    return "permissive_candidates"


def log_capture(
    conn: sqlite3.Connection,
    *,
    payload: dict[str, Any],
    decision: str,
    reason: str,
    candidate_ids: list[int] | None = None,
    raw_size: int | None = None,
    extracted_size: int | None = None,
) -> int:
    candidate_ids = candidate_ids or []
    body = str(payload.get("body_md") or payload.get("raw_text") or payload.get("content") or "")
    summary = str(payload.get("summary") or "")
    return execute_insert(
        conn,
        """INSERT INTO capture_events(event_kind,source_refs_json,actor_identity,runtime,proposed_scope_kind,proposed_scope_id,capture_policy_id,decision,reason,candidate_ids_json,raw_size,extracted_size) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)""",
        (
            payload.get("event_kind", "manual_note"),
            j(payload.get("source_refs", [])),
            payload.get("actor_identity") or payload.get("created_by", "api"),
            payload.get("runtime", "manual"),
            payload.get("scope_kind", "global"),
            payload.get("scope_id"),
            payload.get("capture_policy_id", "api-capture"),
            decision,
            reason,
            j(candidate_ids),
            raw_size if raw_size is not None else len(body),
            extracted_size if extracted_size is not None else len(summary),
        ),
    )


def candidate_payload_from_capture(payload: dict[str, Any]) -> dict[str, Any]:
    body = str(payload.get("body_md") or payload.get("raw_text") or payload.get("content") or "")
    title = payload.get("title") or body.strip().splitlines()[0][:80] or "Captured memory candidate"
    summary = payload.get("summary") or (body.strip()[:180] if body.strip() else title)
    return {
        "proposed_slug": payload.get("proposed_slug"),
        "title": title,
        "body_md": body,
        "summary": summary,
        "proposed_kind": payload.get("proposed_kind", payload.get("kind", "fact")),
        "layer": "candidate",
        "scope_kind": payload.get("scope_kind", "global"),
        "scope_id": payload.get("scope_id"),
        "authority_scope_kind": payload.get("authority_scope_kind", payload.get("scope_kind", "global")),
        "authority_scope_id": payload.get("authority_scope_id", payload.get("scope_id")),
        "discovery_scope": payload.get("discovery_scope", "explicit_only"),
        "claim_strength": payload.get("claim_strength", "observation"),
        "audience": payload.get("audience", []),
        "source_refs": payload.get("source_refs", []),
        "extraction_confidence": payload.get("extraction_confidence"),
        "status": "pending",
        "created_by": payload.get("actor_identity") or payload.get("created_by", "api"),
        "updated_by": payload.get("actor_identity") or payload.get("created_by", "api"),
    }


def validate_capture_candidate(candidate: dict[str, Any]) -> str | None:
    if not str(candidate.get("body_md") or candidate.get("summary") or candidate.get("title") or "").strip():
        return "empty_capture_content"
    text = "\n".join(str(candidate.get(key, "")) for key in ("title", "summary", "body_md"))
    if any(marker.lower() in text.lower() for marker in SECRET_MARKERS):
        return "secret_like_content_filtered"
    if candidate.get("claim_strength") == "policy":
        return "policy_strength_capture_filtered"
    return None


def insert_candidate(conn: sqlite3.Connection, payload: dict[str, Any]) -> int:
    if payload.get("status") and payload.get("status") != "pending":
        raise HTTPException(status_code=400, detail="candidate_create_status_must_be_pending")
    if payload.get("layer") and payload.get("layer") != "candidate":
        raise HTTPException(status_code=400, detail="candidate_create_layer_must_be_candidate")
    if payload.get("claim_strength") == "policy":
        raise HTTPException(status_code=400, detail="candidate_create_cannot_use_policy_claim_strength")
    payload = {**payload, "status": "pending", "layer": "candidate"}
    cid = execute_insert(conn, """INSERT INTO memory_candidates(proposed_slug,title,body_md,summary,proposed_kind,layer,scope_kind,scope_id,authority_scope_kind,authority_scope_id,discovery_scope,claim_strength,audience_json,source_refs_json,extraction_confidence,status,created_by,updated_by) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)""", (
        payload.get("proposed_slug"), payload["title"], payload.get("body_md", ""), payload.get("summary", ""), payload["proposed_kind"], "candidate", payload.get("scope_kind", "global"), payload.get("scope_id"), payload.get("authority_scope_kind", "global"), payload.get("authority_scope_id"), payload.get("discovery_scope", "explicit_only"), payload.get("claim_strength", "observation"), j(payload.get("audience", [])), j(payload.get("source_refs", [])), payload.get("extraction_confidence"), "pending", payload.get("created_by", "api"), payload.get("updated_by", payload.get("created_by", "api"))))
    refresh_candidate_fts(conn, cid, payload)
    return cid



def require_actor_reason(payload: dict[str, Any]) -> tuple[str, str]:
    actor = payload.get("actor_identity")
    reason = payload.get("reason")
    if not actor:
        raise HTTPException(status_code=400, detail="actor_identity_required")
    if not reason:
        raise HTTPException(status_code=400, detail="reason_required")
    return str(actor), str(reason)


def update_candidate_fields(conn: sqlite3.Connection, candidate_id: int, updates: dict[str, Any]) -> dict[str, Any]:
    allowed = {"status", "proposed_slug", "title", "body_md", "summary", "proposed_kind", "scope_kind", "scope_id", "authority_scope_kind", "authority_scope_id", "discovery_scope", "claim_strength", "layer", "audience", "source_refs", "updated_by"}
    set_parts: list[str] = []
    params: list[Any] = []
    for key, value in updates.items():
        if key not in allowed:
            continue
        column = key
        if key in {"audience", "source_refs"}:
            column = f"{key}_json"
            value = j(value)
        set_parts.append(f"{column}=?")
        params.append(value)
    if not set_parts:
        return decode_row(require(conn.execute("SELECT * FROM memory_candidates WHERE id=?", (candidate_id,)).fetchone(), "candidate"))
    set_parts.append("updated_at=datetime('now')")
    params.append(candidate_id)
    execute_update(conn, f"UPDATE memory_candidates SET {', '.join(set_parts)} WHERE id=?", tuple(params))
    row = decode_row(require(conn.execute("SELECT * FROM memory_candidates WHERE id=?", (candidate_id,)).fetchone(), "candidate"))
    refresh_candidate_fts(conn, candidate_id, {
        "title": row["title"],
        "summary": row.get("summary", ""),
        "body_md": row.get("body_md", ""),
        "proposed_kind": row.get("proposed_kind", ""),
    })
    conn.commit()
    return row


def attach_candidate_source_refs(conn: sqlite3.Connection, *, candidate: dict[str, Any], entry_id: int, actor: str) -> list[int]:
    ref_ids: list[int] = []
    for source in candidate.get("source_refs", []) or []:
        if not isinstance(source, dict):
            continue
        sid = execute_insert(
            conn,
            "INSERT INTO source_refs(target_kind,target_id,source_kind,source_project_id,source_id,source_locator_json,source_summary,observed_at,verified_at,verification_status,created_by) VALUES (?,?,?,?,?,?,?,?,?,?,?)",
            (
                "memory_entry",
                entry_id,
                source.get("source_kind", "manual_note"),
                source.get("source_project_id"),
                source.get("source_id", str(candidate["id"])),
                j(source.get("source_locator", {}), {}),
                source.get("source_summary", candidate.get("summary", "")),
                source.get("observed_at"),
                source.get("verified_at"),
                source.get("verification_status", "unverified"),
                actor,
            ),
        )
        ref_ids.append(sid)
    return ref_ids


def insert_memory_entry_from_candidate(conn: sqlite3.Connection, candidate: dict[str, Any], payload: dict[str, Any], actor: str) -> int:
    slug = payload.get("slug") or candidate.get("proposed_slug") or f"candidate-{candidate['id']}"
    entry = {
        "slug": slug,
        "title": payload.get("title", candidate["title"]),
        "summary": payload.get("summary", candidate.get("summary", "")),
        "body_md": payload.get("body_md", candidate.get("body_md", "")),
        "kind": payload.get("kind", candidate.get("proposed_kind", "fact")),
        "layer": payload.get("layer", "curated_fact"),
        "scope_kind": payload.get("scope_kind", candidate.get("scope_kind", "global")),
        "scope_id": payload.get("scope_id", candidate.get("scope_id")),
        "authority_scope_kind": payload.get("authority_scope_kind", candidate.get("authority_scope_kind", "global")),
        "authority_scope_id": payload.get("authority_scope_id", candidate.get("authority_scope_id")),
        "discovery_scope": payload.get("discovery_scope", candidate.get("discovery_scope", "explicit_only")),
        "claim_strength": payload.get("claim_strength", candidate.get("claim_strength", "observation")),
        "status": "active",
        "curation_state": "curated",
        "confidence": payload.get("confidence", "source_backed"),
        "stability": payload.get("stability", "evolving"),
        "audience": payload.get("audience", candidate.get("audience", [])),
        "tags": payload.get("tags", []),
        "created_by": actor,
        "updated_by": actor,
    }
    entry_id = execute_insert(conn, """
        INSERT INTO memory_entries(slug,title,summary,body_md,content_format,kind,layer,scope_kind,scope_id,authority_scope_kind,authority_scope_id,discovery_scope,claim_strength,status,curation_state,confidence,stability,audience_json,tags_json,created_by,updated_by)
        VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
    """, (
        entry["slug"], entry["title"], entry["summary"], entry["body_md"], "markdown", entry["kind"], entry["layer"], entry["scope_kind"], entry["scope_id"], entry["authority_scope_kind"], entry["authority_scope_id"], entry["discovery_scope"], entry["claim_strength"], entry["status"], entry["curation_state"], entry["confidence"], entry["stability"], j(entry["audience"]), j(entry["tags"]), actor, actor,
    ))
    refresh_entry_fts(conn, entry_id, entry)
    return entry_id


@router.post("/memory-entries")
def create_entry(request: Request, payload: dict[str, Any]) -> dict[str, Any]:
    conn = db(request)
    entry_id = execute_insert(conn, """
        INSERT INTO memory_entries(slug,title,summary,body_md,content_format,kind,layer,scope_kind,scope_id,authority_scope_kind,authority_scope_id,discovery_scope,claim_strength,status,curation_state,confidence,stability,audience_json,tags_json,created_by,updated_by)
        VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
    """, (
        payload["slug"], payload["title"], payload.get("summary", ""), payload.get("body_md", ""), payload.get("content_format", "markdown"), payload["kind"], payload.get("layer", "curated_fact"), payload.get("scope_kind", "global"), payload.get("scope_id"), payload.get("authority_scope_kind", "global"), payload.get("authority_scope_id"), payload.get("discovery_scope", "explicit_only"), payload.get("claim_strength", "observation"), payload.get("status", "draft"), payload.get("curation_state", "candidate"), payload.get("confidence", "unverified"), payload.get("stability", "evolving"), j(payload.get("audience", [])), j(payload.get("tags", [])), payload.get("created_by", "api"), payload.get("updated_by", payload.get("created_by", "api")),
    ))
    refresh_entry_fts(conn, entry_id, payload)
    log_curation(conn, action="promote" if payload.get("curation_state") == "curated" else "relabel", actor=payload.get("created_by", "api"), reason="entry created", memory_entry_id=entry_id, after=payload)
    conn.commit()
    return decode_row(conn.execute("SELECT * FROM memory_entries WHERE id=?", (entry_id,)).fetchone())


@router.get("/memory-entries/{slug}")
def get_entry(request: Request, slug: str) -> dict[str, Any]:
    return decode_row(require(db(request).execute("SELECT * FROM memory_entries WHERE slug=?", (slug,)).fetchone(), "memory_entry"))


@router.put("/memory-entries/{slug}")
def update_entry(request: Request, slug: str, payload: dict[str, Any]) -> dict[str, Any]:
    conn = db(request)
    before = get_entry(request, slug)
    merged = {**before, **payload, "slug": slug}
    execute_update(conn, """UPDATE memory_entries SET title=?,summary=?,body_md=?,layer=?,scope_kind=?,scope_id=?,authority_scope_kind=?,authority_scope_id=?,discovery_scope=?,claim_strength=?,status=?,curation_state=?,confidence=?,stability=?,audience_json=?,tags_json=?,updated_by=?,updated_at=datetime('now') WHERE slug=?""", (
        merged["title"], merged.get("summary", ""), merged.get("body_md", ""), merged.get("layer", "curated_fact"), merged.get("scope_kind", "global"), merged.get("scope_id"), merged.get("authority_scope_kind", "global"), merged.get("authority_scope_id"), merged.get("discovery_scope", "explicit_only"), merged.get("claim_strength", "observation"), merged.get("status", "draft"), merged.get("curation_state", "candidate"), merged.get("confidence", "unverified"), merged.get("stability", "evolving"), j(merged.get("audience", [])), j(merged.get("tags", [])), payload.get("updated_by", "api"), slug))
    rowid = db(request).execute("SELECT id FROM memory_entries WHERE slug=?", (slug,)).fetchone()["id"]
    refresh_entry_fts(conn, rowid, merged)
    log_curation(conn, action="relabel", actor=payload.get("updated_by", "api"), reason=payload.get("reason", "entry updated"), memory_entry_id=rowid, before=before, after=merged)
    conn.commit()
    return get_entry(request, slug)


@router.post("/memory-entries/{slug}/archive")
def archive_entry(request: Request, slug: str, payload: dict[str, Any] | None = None) -> dict[str, Any]:
    payload = payload or {}
    return update_entry(request, slug, {"status": "archived", "updated_by": payload.get("actor_identity", "api"), "reason": payload.get("reason", "archived")})


@router.post("/memory-entries/{slug}/supersede")
def supersede_entry(request: Request, slug: str, payload: dict[str, Any] | None = None) -> dict[str, Any]:
    payload = payload or {}
    return update_entry(request, slug, {"status": "superseded", "updated_by": payload.get("actor_identity", "api"), "reason": payload.get("reason", "superseded")})


@router.post("/memory-entries/search")
def search_entries(request: Request, payload: dict[str, Any]) -> dict[str, Any]:
    q = payload["query"]
    limit = int(payload.get("limit", 10))
    cur = db(request).execute("""SELECT e.* FROM memory_entries_fts f JOIN memory_entries e ON e.id=f.rowid WHERE memory_entries_fts MATCH ? LIMIT ?""", (q, limit))
    return {"items": rows(cur)}


@router.post("/candidates")
def create_candidate(request: Request, payload: dict[str, Any]) -> dict[str, Any]:
    conn = db(request)
    cid = insert_candidate(conn, payload)
    capture_id = log_capture(
        conn,
        payload={**payload, "actor_identity": payload.get("created_by", "api"), "capture_policy_id": payload.get("capture_policy_id", "api-candidate-create")},
        decision="captured",
        reason=payload.get("reason", "candidate created"),
        candidate_ids=[cid],
    )
    conn.commit()
    item = decode_row(conn.execute("SELECT * FROM memory_candidates WHERE id=?", (cid,)).fetchone())
    item["capture_event_id"] = capture_id
    return item


@router.post("/capture")
def capture(request: Request, payload: dict[str, Any]) -> dict[str, Any]:
    conn = db(request)
    runtime = payload.get("runtime", "manual")
    capture_mode = payload.get("capture_mode") or default_capture_mode(runtime, payload.get("actor_role"))
    if capture_mode not in CAPTURE_MODES:
        event_id = log_capture(conn, payload=payload, decision="errored", reason="invalid_capture_mode")
        conn.commit()
        return {"decision": "errored", "reason": "invalid_capture_mode", "capture_event_id": event_id, "candidate_ids": []}
    if payload.get("simulate_error") or payload.get("force_error"):
        event_id = log_capture(conn, payload=payload, decision="errored", reason=payload.get("reason", "simulated_capture_error"))
        conn.commit()
        return {"decision": "errored", "reason": payload.get("reason", "simulated_capture_error"), "capture_event_id": event_id, "candidate_ids": []}
    if capture_mode == "off":
        event_id = log_capture(conn, payload=payload, decision="ignored", reason=payload.get("reason", "capture_mode_off"))
        conn.commit()
        return {"decision": "ignored", "reason": payload.get("reason", "capture_mode_off"), "capture_event_id": event_id, "candidate_ids": []}
    if capture_mode == "metadata_only":
        event_id = log_capture(conn, payload=payload, decision="ignored", reason=payload.get("reason", "metadata_only"), raw_size=0, extracted_size=0)
        conn.commit()
        return {"decision": "ignored", "reason": payload.get("reason", "metadata_only"), "capture_event_id": event_id, "candidate_ids": []}
    if capture_mode == "curated_manual_only":
        event_id = log_capture(conn, payload=payload, decision="ignored", reason=payload.get("reason", "curated_manual_only"))
        conn.commit()
        return {"decision": "ignored", "reason": payload.get("reason", "curated_manual_only"), "capture_event_id": event_id, "candidate_ids": []}

    candidate = candidate_payload_from_capture(payload)
    filter_reason = validate_capture_candidate(candidate)
    if filter_reason:
        event_id = log_capture(conn, payload=payload, decision="filtered", reason=payload.get("reason", filter_reason))
        conn.commit()
        return {"decision": "filtered", "reason": payload.get("reason", filter_reason), "capture_event_id": event_id, "candidate_ids": []}

    try:
        cid = insert_candidate(conn, candidate)
    except HTTPException as exc:
        event_id = log_capture(conn, payload=payload, decision="errored", reason=str(exc.detail))
        conn.commit()
        return {"decision": "errored", "reason": str(exc.detail), "capture_event_id": event_id, "candidate_ids": []}
    event_id = log_capture(conn, payload=payload, decision="captured", reason=payload.get("reason", "captured candidate"), candidate_ids=[cid])
    conn.commit()
    return {
        "decision": "captured",
        "reason": payload.get("reason", "captured candidate"),
        "capture_event_id": event_id,
        "candidate_ids": [cid],
        "candidate": decode_row(conn.execute("SELECT * FROM memory_candidates WHERE id=?", (cid,)).fetchone()),
    }


@router.get("/candidates")
def list_candidates(request: Request, status: str | None = None, scope_kind: str | None = None, scope_id: str | None = None, runtime: str | None = None, actor: str | None = None, limit: int = Query(50, ge=1, le=500)) -> dict[str, Any]:
    clauses = []
    params: list[Any] = []
    if status: clauses.append("status=?"); params.append(status)
    if scope_kind: clauses.append("scope_kind=?"); params.append(scope_kind)
    if scope_id: clauses.append("scope_id=?"); params.append(scope_id)
    if actor: clauses.append("created_by=?"); params.append(actor)
    # runtime filter follows capture events created by /candidates.
    sql = "SELECT * FROM memory_candidates"
    if runtime:
        sql += " WHERE id IN (SELECT value FROM capture_events, json_each(capture_events.candidate_ids_json) WHERE runtime=?)"
        params = [runtime] + params
        if clauses:
            sql += " AND " + " AND ".join(clauses)
    elif clauses:
        sql += " WHERE " + " AND ".join(clauses)
    sql += " ORDER BY id DESC LIMIT ?"; params.append(limit)
    return {"items": rows(db(request).execute(sql, tuple(params)))}


@router.get("/candidates/{candidate_id}")
def get_candidate(request: Request, candidate_id: int) -> dict[str, Any]:
    return decode_row(require(db(request).execute("SELECT * FROM memory_candidates WHERE id=?", (candidate_id,)).fetchone(), "candidate"))


@router.patch("/candidates/{candidate_id}/status")
def update_candidate_status(request: Request, candidate_id: int, payload: dict[str, Any]) -> dict[str, Any]:
    conn = db(request)
    before = get_candidate(request, candidate_id)
    execute_update(conn, "UPDATE memory_candidates SET status=?, updated_by=?, updated_at=datetime('now') WHERE id=?", (payload["status"], payload.get("actor_identity", "api"), candidate_id))
    after = get_candidate(request, candidate_id)
    action = "claim" if payload["status"] == "claimed" else "reject" if payload["status"] == "rejected" else "supersede" if payload["status"] == "superseded" else "relabel"
    log_curation(conn, action=action, actor=payload.get("actor_identity", "api"), reason=payload.get("reason", "candidate status updated"), candidate_id=candidate_id, before=before, after=after)
    conn.commit()
    return after


@router.post("/candidates/search")
def search_candidates(request: Request, payload: dict[str, Any]) -> dict[str, Any]:
    cur = db(request).execute("SELECT c.* FROM memory_candidates_fts f JOIN memory_candidates c ON c.id=f.rowid WHERE memory_candidates_fts MATCH ? LIMIT ?", (payload["query"], int(payload.get("limit", 10))))
    return {"items": rows(cur)}


@router.post("/topic-nodes")
def create_node(request: Request, payload: dict[str, Any]) -> dict[str, Any]:
    conn = db(request)
    nid = execute_insert(conn, """INSERT INTO topic_nodes(slug,title,node_type,content_ref_kind,content_ref_id,memory_entry_id,summary,layer,canonicality,importance,stability,scope_kind,scope_id,authority_scope_kind,authority_scope_id,discovery_scope,claim_strength,audience_json,default_unroll_policy_json,created_by,updated_by) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)""", (
        payload["slug"], payload["title"], payload["node_type"], payload.get("content_ref_kind"), payload.get("content_ref_id"), payload.get("memory_entry_id"), payload.get("summary", ""), payload.get("layer", "topic_scene"), payload.get("canonicality", "preferred"), payload.get("importance", "important"), payload.get("stability", "evolving"), payload.get("scope_kind", "global"), payload.get("scope_id"), payload.get("authority_scope_kind", "global"), payload.get("authority_scope_id"), payload.get("discovery_scope", "explicit_only"), payload.get("claim_strength", "observation"), j(payload.get("audience", [])), j(payload.get("default_unroll_policy"), None), payload.get("created_by", "api"), payload.get("updated_by", payload.get("created_by", "api"))))
    refresh_node_fts(conn, nid, payload)
    conn.commit()
    return decode_row(conn.execute("SELECT * FROM topic_nodes WHERE id=?", (nid,)).fetchone())


@router.get("/topic-nodes")
def list_nodes(request: Request, limit: int = Query(50, ge=1, le=500)) -> dict[str, Any]:
    return {"items": rows(db(request).execute("SELECT * FROM topic_nodes ORDER BY id DESC LIMIT ?", (limit,)))}


@router.get("/topic-nodes/{slug}")
def get_node(request: Request, slug: str) -> dict[str, Any]:
    return decode_row(require(db(request).execute("SELECT * FROM topic_nodes WHERE slug=?", (slug,)).fetchone(), "topic_node"))


@router.put("/topic-nodes/{slug}")
def update_node(request: Request, slug: str, payload: dict[str, Any]) -> dict[str, Any]:
    conn = db(request)
    before = get_node(request, slug)
    merged = {**before, **payload, "slug": slug}
    execute_update(conn, "UPDATE topic_nodes SET title=?,summary=?,claim_strength=?,discovery_scope=?,updated_by=?,updated_at=datetime('now') WHERE slug=?", (merged["title"], merged.get("summary", ""), merged.get("claim_strength", "observation"), merged.get("discovery_scope", "explicit_only"), payload.get("updated_by", "api"), slug))
    nid = conn.execute("SELECT id FROM topic_nodes WHERE slug=?", (slug,)).fetchone()["id"]
    refresh_node_fts(conn, nid, merged)
    log_curation(conn, action="relabel", actor=payload.get("updated_by", "api"), reason=payload.get("reason", "node updated"), node_id=nid, before=before, after=merged)
    conn.commit()
    return get_node(request, slug)


@router.post("/topic-nodes/search")
def search_nodes(request: Request, payload: dict[str, Any]) -> dict[str, Any]:
    cur = db(request).execute("SELECT n.* FROM topic_nodes_fts f JOIN topic_nodes n ON n.id=f.rowid WHERE topic_nodes_fts MATCH ? LIMIT ?", (payload["query"], int(payload.get("limit", 10))))
    return {"items": rows(cur)}


@router.post("/topic-edges")
def create_edge(request: Request, payload: dict[str, Any]) -> dict[str, Any]:
    conn = db(request)
    eid = execute_insert(conn, "INSERT INTO topic_edges(from_node_id,to_node_id,relation,weight,priority,condition_json,condition_hash,audience_json,status,notes,created_by) VALUES (?,?,?,?,?,?,?,?,?,?,?)", (payload["from_node_id"], payload["to_node_id"], payload["relation"], payload.get("weight", 1.0), payload.get("priority", 0), j(payload.get("condition"), None), payload.get("condition_hash", ""), j(payload.get("audience", [])), payload.get("status", "active"), payload.get("notes", ""), payload.get("created_by", "api")))
    item = decode_row(conn.execute("SELECT * FROM topic_edges WHERE id=?", (eid,)).fetchone())
    log_curation(conn, action="link", actor=payload.get("created_by", "api"), reason=payload.get("reason", "edge created"), edge_id=eid, after=item)
    conn.commit()
    return item


@router.delete("/topic-edges/{edge_id}")
def delete_edge(request: Request, edge_id: int, actor_identity: str = "api", reason: str = "edge deleted") -> dict[str, Any]:
    conn = db(request)
    before = decode_row(require(conn.execute("SELECT * FROM topic_edges WHERE id=?", (edge_id,)).fetchone(), "topic_edge"))
    log_curation(conn, action="unlink", actor=actor_identity, reason=reason, before=before)
    conn.execute("DELETE FROM topic_edges WHERE id=?", (edge_id,))
    conn.commit()
    return {"deleted": True, "edge_id": edge_id}


@router.get("/topic-nodes/{slug}/neighbors")
def neighbors(request: Request, slug: str) -> dict[str, Any]:
    node = get_node(request, slug)
    cur = db(request).execute("SELECT e.*, n.slug AS to_slug, n.title AS to_title FROM topic_edges e JOIN topic_nodes n ON n.id=e.to_node_id WHERE e.from_node_id=? ORDER BY e.priority DESC, e.id", (node["id"],))
    return {"items": rows(cur)}


@router.post("/topic-views")
def create_view(request: Request, payload: dict[str, Any]) -> dict[str, Any]:
    conn = db(request)
    vid = execute_insert(conn, "INSERT INTO topic_views(slug,title,root_node_id,view_type,audience_json,mode,include_relations_json,exclude_relations_json,max_depth,token_budget_hint,ordering_policy,status,created_by,updated_by) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)", (payload["slug"], payload["title"], payload["root_node_id"], payload["view_type"], j(payload.get("audience", [])), payload.get("mode", "general"), j(payload.get("include_relations", [])), j(payload.get("exclude_relations", [])), payload.get("max_depth", 2), payload.get("token_budget_hint", 3000), payload.get("ordering_policy", "core_first_then_risks"), payload.get("status", "active"), payload.get("created_by", "api"), payload.get("updated_by", payload.get("created_by", "api"))))
    return decode_row(conn.execute("SELECT * FROM topic_views WHERE id=?", (vid,)).fetchone())


@router.get("/topic-views")
def list_views(request: Request, limit: int = Query(50, ge=1, le=500)) -> dict[str, Any]:
    return {"items": rows(db(request).execute("SELECT * FROM topic_views ORDER BY id DESC LIMIT ?", (limit,)))}


@router.get("/topic-views/{slug}")
def get_view(request: Request, slug: str) -> dict[str, Any]:
    return decode_row(require(db(request).execute("SELECT * FROM topic_views WHERE slug=?", (slug,)).fetchone(), "topic_view"))


@router.put("/topic-views/{slug}")
def update_view(request: Request, slug: str, payload: dict[str, Any]) -> dict[str, Any]:
    current = get_view(request, slug)
    merged = {**current, **payload}
    execute_update(db(request), "UPDATE topic_views SET title=?,mode=?,max_depth=?,token_budget_hint=?,ordering_policy=?,status=?,updated_by=?,updated_at=datetime('now') WHERE slug=?", (merged["title"], merged.get("mode", "general"), merged.get("max_depth", 2), merged.get("token_budget_hint", 3000), merged.get("ordering_policy", "core_first_then_risks"), merged.get("status", "active"), payload.get("updated_by", "api"), slug))
    return get_view(request, slug)



@router.post("/curation/candidates/{candidate_id}/claim")
def claim_candidate(request: Request, candidate_id: int, payload: dict[str, Any]) -> dict[str, Any]:
    conn = db(request)
    actor, reason = require_actor_reason(payload)
    before = get_candidate(request, candidate_id)
    after = update_candidate_fields(conn, candidate_id, {"status": "claimed", "updated_by": actor})
    event_id = log_curation(conn, action="claim", actor=actor, reason=reason, candidate_id=candidate_id, before=before, after=after)
    conn.commit()
    return {"candidate": after, "curation_event_id": event_id}


@router.post("/curation/candidates/{candidate_id}/reject")
def reject_candidate(request: Request, candidate_id: int, payload: dict[str, Any]) -> dict[str, Any]:
    conn = db(request)
    actor, reason = require_actor_reason(payload)
    before = get_candidate(request, candidate_id)
    after = update_candidate_fields(conn, candidate_id, {"status": "rejected", "updated_by": actor})
    event_id = log_curation(conn, action="reject", actor=actor, reason=reason, candidate_id=candidate_id, before=before, after=after)
    conn.commit()
    return {"candidate": after, "curation_event_id": event_id}


@router.post("/curation/candidates/{candidate_id}/promote")
def promote_candidate(request: Request, candidate_id: int, payload: dict[str, Any]) -> dict[str, Any]:
    conn = db(request)
    actor, reason = require_actor_reason(payload)
    before = get_candidate(request, candidate_id)
    if before["status"] not in {"pending", "claimed"}:
        raise HTTPException(status_code=400, detail="candidate_not_promotable")
    entry_id = insert_memory_entry_from_candidate(conn, before, payload, actor)
    source_ref_ids = attach_candidate_source_refs(conn, candidate=before, entry_id=entry_id, actor=actor)
    after_candidate = update_candidate_fields(conn, candidate_id, {"status": "promoted", "updated_by": actor})
    entry = decode_row(conn.execute("SELECT * FROM memory_entries WHERE id=?", (entry_id,)).fetchone())
    after = {"candidate": after_candidate, "memory_entry": entry, "source_ref_ids": source_ref_ids}
    event_id = log_curation(conn, action="promote", actor=actor, reason=reason, candidate_id=candidate_id, memory_entry_id=entry_id, before=before, after=after)
    conn.commit()
    return {"candidate": after_candidate, "memory_entry": entry, "source_ref_ids": source_ref_ids, "curation_event_id": event_id}


@router.post("/curation/candidates/{candidate_id}/split")
def split_candidate(request: Request, candidate_id: int, payload: dict[str, Any]) -> dict[str, Any]:
    conn = db(request)
    actor, reason = require_actor_reason(payload)
    before = get_candidate(request, candidate_id)
    fragments = payload.get("fragments") or []
    if len(fragments) < 2:
        raise HTTPException(status_code=400, detail="at_least_two_fragments_required")
    new_ids: list[int] = []
    split_candidates: list[dict[str, Any]] = []
    for fragment in fragments:
        candidate = {**before, **fragment, "source_refs": fragment.get("source_refs", before.get("source_refs", [])), "created_by": actor, "updated_by": actor, "status": "pending", "layer": "candidate"}
        candidate.pop("id", None)
        new_id = insert_candidate(conn, candidate)
        new_ids.append(new_id)
        split_candidates.append(decode_row(conn.execute("SELECT * FROM memory_candidates WHERE id=?", (new_id,)).fetchone()))
    after_original = update_candidate_fields(conn, candidate_id, {"status": "needs_split", "updated_by": actor})
    after = {"original": after_original, "split_candidates": split_candidates}
    event_id = log_curation(conn, action="split", actor=actor, reason=reason, candidate_id=candidate_id, before=before, after=after)
    conn.commit()
    return {"candidate": after_original, "split_candidate_ids": new_ids, "curation_event_id": event_id}


@router.post("/curation/candidates/merge")
def merge_candidates(request: Request, payload: dict[str, Any]) -> dict[str, Any]:
    conn = db(request)
    actor, reason = require_actor_reason(payload)
    candidate_ids = payload.get("candidate_ids") or []
    if len(candidate_ids) < 2:
        raise HTTPException(status_code=400, detail="at_least_two_candidates_required")
    before = [get_candidate(request, int(cid)) for cid in candidate_ids]
    merged_sources: list[Any] = []
    for item in before:
        merged_sources.extend(item.get("source_refs", []) or [])
    merged_payload = {
        "title": payload.get("title") or " / ".join(item["title"] for item in before),
        "summary": payload.get("summary") or "\n".join(item.get("summary", "") for item in before if item.get("summary")),
        "body_md": payload.get("body_md") or "\n\n".join(item.get("body_md", "") for item in before if item.get("body_md")),
        "proposed_kind": payload.get("proposed_kind", before[0].get("proposed_kind", "fact")),
        "scope_kind": payload.get("scope_kind", before[0].get("scope_kind", "global")),
        "scope_id": payload.get("scope_id", before[0].get("scope_id")),
        "authority_scope_kind": payload.get("authority_scope_kind", before[0].get("authority_scope_kind", "global")),
        "authority_scope_id": payload.get("authority_scope_id", before[0].get("authority_scope_id")),
        "discovery_scope": payload.get("discovery_scope", before[0].get("discovery_scope", "explicit_only")),
        "claim_strength": payload.get("claim_strength", before[0].get("claim_strength", "observation")),
        "source_refs": merged_sources,
        "created_by": actor,
        "updated_by": actor,
    }
    merged_id = insert_candidate(conn, merged_payload)
    updated_sources = []
    for cid in candidate_ids:
        updated_sources.append(update_candidate_fields(conn, int(cid), {"status": "needs_merge", "updated_by": actor}))
    merged_candidate = get_candidate(request, merged_id)
    after = {"merged_candidate": merged_candidate, "source_candidates": updated_sources}
    event_id = log_curation(conn, action="merge", actor=actor, reason=reason, candidate_id=merged_id, before=before, after=after)
    conn.commit()
    return {"merged_candidate": merged_candidate, "curation_event_id": event_id}


@router.post("/curation/candidates/{candidate_id}/relabel")
def relabel_candidate(request: Request, candidate_id: int, payload: dict[str, Any]) -> dict[str, Any]:
    conn = db(request)
    actor, reason = require_actor_reason(payload)
    before = get_candidate(request, candidate_id)
    updates = {key: payload[key] for key in ("title", "summary", "body_md", "proposed_kind", "claim_strength", "audience") if key in payload}
    updates["updated_by"] = actor
    after = update_candidate_fields(conn, candidate_id, updates)
    event_id = log_curation(conn, action="relabel", actor=actor, reason=reason, candidate_id=candidate_id, before=before, after=after)
    conn.commit()
    return {"candidate": after, "curation_event_id": event_id}


@router.post("/curation/candidates/{candidate_id}/rescope")
def rescope_candidate(request: Request, candidate_id: int, payload: dict[str, Any]) -> dict[str, Any]:
    conn = db(request)
    actor, reason = require_actor_reason(payload)
    required = {"scope_kind", "authority_scope_kind", "discovery_scope", "claim_strength"}
    missing = sorted(required - set(payload))
    if missing:
        raise HTTPException(status_code=400, detail={"missing": missing})
    before = get_candidate(request, candidate_id)
    updates = {key: payload.get(key) for key in ("scope_kind", "scope_id", "authority_scope_kind", "authority_scope_id", "discovery_scope", "claim_strength") if key in payload}
    updates["updated_by"] = actor
    after = update_candidate_fields(conn, candidate_id, updates)
    event_id = log_curation(conn, action="rescope", actor=actor, reason=reason, candidate_id=candidate_id, before=before, after=after)
    conn.commit()
    return {"candidate": after, "curation_event_id": event_id}


@router.post("/curation/memory-entries/{slug}/supersede")
def curation_supersede_entry(request: Request, slug: str, payload: dict[str, Any]) -> dict[str, Any]:
    conn = db(request)
    actor, reason = require_actor_reason(payload)
    before = get_entry(request, slug)
    execute_update(conn, "UPDATE memory_entries SET status='superseded', updated_by=?, updated_at=datetime('now') WHERE slug=?", (actor, slug))
    after = get_entry(request, slug)
    event_id = log_curation(conn, action="supersede", actor=actor, reason=reason, memory_entry_id=after["id"], before=before, after=after)
    conn.commit()
    return {"memory_entry": after, "curation_event_id": event_id}


@router.post("/curation/topic-edges/link")
def curation_link_edge(request: Request, payload: dict[str, Any]) -> dict[str, Any]:
    actor, reason = require_actor_reason(payload)
    payload = {**payload, "created_by": actor, "reason": reason}
    return create_edge(request, payload)


@router.post("/curation/topic-edges/{edge_id}/unlink")
def curation_unlink_edge(request: Request, edge_id: int, payload: dict[str, Any]) -> dict[str, Any]:
    actor, reason = require_actor_reason(payload)
    return delete_edge(request, edge_id, actor_identity=actor, reason=reason)



def entry_source_statuses(conn: sqlite3.Connection, entry_id: int) -> list[str]:
    return [row["verification_status"] for row in conn.execute("SELECT verification_status FROM source_refs WHERE target_kind='memory_entry' AND target_id=?", (entry_id,))]


def entry_provenance(conn: sqlite3.Connection, entry_id: int) -> list[dict[str, Any]]:
    return rows(conn.execute("SELECT * FROM source_refs WHERE target_kind='memory_entry' AND target_id=? ORDER BY id", (entry_id,)))


def markdown_for_packet(packet: dict[str, Any]) -> str:
    lines = [f"# Recall packet {packet['packet_id']}", ""]
    lines.append("## Included")
    for item in packet["included_nodes"]:
        lines.append(f"- **{item['title']}** (`{item['slug']}`) score={item.get('score')} interpretation={item.get('interpretation')}")
        if item.get("summary"):
            lines.append(f"  - {item['summary']}")
    if not packet["included_nodes"]:
        lines.append("- none")
    lines.append("")
    lines.append("## Skipped")
    for item in packet["skipped"]:
        lines.append(f"- `{item['node_slug']}`: {item['reason']}")
    if not packet["skipped"]:
        lines.append("- none")
    return "\n".join(lines)


def authority_scope_string(item: dict[str, Any]) -> str:
    scope_id = item.get("authority_scope_id")
    return f"{item.get('authority_scope_kind', 'global')}:{scope_id}" if scope_id else str(item.get("authority_scope_kind", "global"))


def source_ref_for_contract(source: dict[str, Any]) -> dict[str, Any]:
    return {
        "source_kind": source.get("source_kind", "manual_note"),
        "source_project_id": source.get("source_project_id"),
        "source_id": str(source.get("source_id", "unknown")),
        "source_locator": source.get("source_locator", {}),
        "source_summary": source.get("source_summary", ""),
        "observed_at": source.get("observed_at"),
        "verified_at": source.get("verified_at"),
        "verification_status": source.get("verification_status", "unverified"),
    }


def match_kind(label: str) -> str:
    if label == "authoritative":
        return "authoritative"
    if label == "discovered_evidence":
        return "discoverable"
    return "fts_only"


def find_root_entries(conn: sqlite3.Connection, query: str, limit: int) -> list[dict[str, Any]]:
    return rows(conn.execute("""
        SELECT e.* FROM memory_entries_fts f
        JOIN memory_entries e ON e.id=f.rowid
        WHERE memory_entries_fts MATCH ?
        LIMIT ?
    """, (query, limit)))


def find_root_nodes(conn: sqlite3.Connection, query: str, limit: int) -> list[dict[str, Any]]:
    return rows(conn.execute("""
        SELECT n.* FROM topic_nodes_fts f
        JOIN topic_nodes n ON n.id=f.rowid
        WHERE topic_nodes_fts MATCH ?
        LIMIT ?
    """, (query, limit)))


def load_view(conn: sqlite3.Connection, slug: str | None) -> dict[str, Any] | None:
    if not slug:
        return None
    return decode_row(require(conn.execute("SELECT * FROM topic_views WHERE slug=?", (slug,)).fetchone(), "topic_view"))


def traverse_topic_edges(conn: sqlite3.Connection, roots: list[dict[str, Any]], view: dict[str, Any] | None, request_policy: dict[str, Any]) -> list[dict[str, Any]]:
    include_relations = set(request_policy.get("include_relations") or (view or {}).get("include_relations") or [])
    exclude_relations = set(request_policy.get("exclude_relations") or (view or {}).get("exclude_relations") or [])
    max_depth = int(request_policy.get("max_depth", (view or {}).get("max_depth", 1)))
    if max_depth <= 0:
        return []
    visited = {root["id"] for root in roots}
    frontier = [(root["id"], 0, None) for root in roots]
    found: list[dict[str, Any]] = []
    while frontier:
        node_id, depth, incoming = frontier.pop(0)
        if depth >= max_depth:
            continue
        for row in conn.execute("SELECT e.*, n.* FROM topic_edges e JOIN topic_nodes n ON n.id=e.to_node_id WHERE e.from_node_id=? AND e.status='active' ORDER BY e.priority DESC, e.id", (node_id,)):
            data = dict(row)
            relation = data["relation"]
            to_id = data["to_node_id"]
            if include_relations and relation not in include_relations:
                continue
            if relation in exclude_relations:
                continue
            if to_id in visited:
                continue
            visited.add(to_id)
            node = decode_row(conn.execute("SELECT * FROM topic_nodes WHERE id=?", (to_id,)).fetchone())
            node["edge_relation"] = relation
            node["edge_depth"] = depth + 1
            found.append(node)
            frontier.append((to_id, depth + 1, relation))
    return found


@router.post("/recall")
def recall(request: Request, payload: dict[str, Any]) -> dict[str, Any]:
    conn = db(request)
    query = payload["query"]
    limit = int(payload.get("limit", 10))
    context = RecallContext(scope_kind=payload.get("scope_kind", "global"), scope_id=payload.get("scope_id"))
    view = load_view(conn, payload.get("topic_view_slug"))
    root_entries = find_root_entries(conn, query, limit)
    root_nodes = find_root_nodes(conn, query, limit)
    if view and not any(node["id"] == view["root_node_id"] for node in root_nodes):
        root = decode_row(conn.execute("SELECT * FROM topic_nodes WHERE id=?", (view["root_node_id"],)).fetchone())
        if root:
            root_nodes.insert(0, root)
    edge_nodes = traverse_topic_edges(conn, root_nodes, view, payload)
    included_nodes: list[dict[str, Any]] = []
    included_edges: list[dict[str, Any]] = []
    root_matches: list[dict[str, Any]] = []
    skipped: list[dict[str, Any]] = []
    provenance: list[dict[str, Any]] = []
    warnings: list[str] = []
    for entry in root_entries:
        if entry.get("status") in EXCLUDED_ENTRY_STATUSES:
            skipped.append({"node_slug": entry["slug"], "reason": f"status:{entry['status']}"})
            continue
        entry_sources = entry_provenance(conn, entry["id"])
        score = score_item(entry, context, [source.get("verification_status", "unverified") for source in entry_sources])
        if score["authority_label"] == "out_of_scope":
            skipped.append({"node_slug": entry["slug"], "reason": "out_of_scope"})
            continue
        if score["authority_label"] == "discovered_evidence":
            warnings.append(f"{entry['slug']} is discovered cross-scope evidence, not authority for this scope")
        provenance.extend(source_ref_for_contract(source) for source in entry_sources)
        root_matches.append({"node_slug": entry["slug"], "match_kind": match_kind(score["authority_label"]), "score": score["score"], "why": f"FTS entry match; {score['authority_label']}"})
        included_nodes.append({"kind": "memory_entry", "id": entry["id"], "slug": entry["slug"], "title": entry["title"], "summary": entry.get("summary", ""), "score": score["score"], "authority_scope": authority_scope_string(entry), "discovery_scope": entry.get("discovery_scope"), "claim_strength": entry.get("claim_strength"), "interpretation": score["authority_label"], "scope_kind": entry.get("scope_kind"), "scope_id": entry.get("scope_id"), "authority_scope_kind": entry.get("authority_scope_kind"), "authority_scope_id": entry.get("authority_scope_id")})
    for node in root_nodes + edge_nodes:
        item = {**node, "status": node.get("status", "active"), "confidence": "source_backed"}
        score = score_item(item, context, [], edge_depth=int(node.get("edge_depth", 0)), relation=node.get("edge_relation"))
        if score["authority_label"] == "out_of_scope":
            skipped.append({"node_slug": node["slug"], "reason": "out_of_scope"})
            continue
        if not node.get("edge_relation"):
            root_matches.append({"node_slug": node["slug"], "match_kind": match_kind(score["authority_label"]), "score": score["score"], "why": f"FTS/topic-view root; {score['authority_label']}"})
        else:
            included_edges.append({"from_view": payload.get("topic_view_slug"), "to_node_slug": node["slug"], "relation": node.get("edge_relation"), "depth": node.get("edge_depth", 0)})
        included_nodes.append({"kind": "topic_node", "id": node["id"], "slug": node["slug"], "title": node["title"], "summary": node.get("summary", ""), "score": score["score"], "authority_scope": authority_scope_string(node), "discovery_scope": node.get("discovery_scope"), "claim_strength": node.get("claim_strength"), "interpretation": score["authority_label"], "edge_relation": node.get("edge_relation"), "edge_depth": node.get("edge_depth", 0), "scope_kind": node.get("scope_kind"), "scope_id": node.get("scope_id"), "authority_scope_kind": node.get("authority_scope_kind"), "authority_scope_id": node.get("authority_scope_id")})
    included_nodes.sort(key=lambda item: item["score"], reverse=True)
    packet_id = payload.get("packet_id") or f"recall-{conn.execute('SELECT COUNT(*) FROM recall_logs').fetchone()[0] + 1}"
    log_id = execute_insert(conn, "INSERT INTO recall_logs(packet_id,request_json,root_node_ids_json,included_node_ids_json,skipped_json,warnings_json,scoring_profile,token_budget,estimated_tokens,created_by) VALUES (?,?,?,?,?,?,?,?,?,?)", (
        packet_id, j(payload, {}), j([node["id"] for node in root_nodes]), j([item["id"] for item in included_nodes if item.get("kind") == "topic_node"]), j(skipped), j(warnings), scoring_defaults_readback()["profile"], payload.get("token_budget"), 0, payload.get("actor_identity"),
    ))
    packet = {"packet_id": packet_id, "packet_md": "", "root_matches": root_matches, "included_nodes": included_nodes[:limit], "included_edges": included_edges, "skipped": skipped, "warnings": warnings, "provenance": provenance, "audit": {"recall_log_id": log_id, "scoring_profile": scoring_defaults_readback()["profile"], "scoring_defaults_ref": "contracts/v0/scoring-defaults.json"}}
    packet["packet_md"] = markdown_for_packet(packet)
    conn.execute("UPDATE recall_logs SET estimated_tokens=?, packet_json=? WHERE id=?", (len(packet["packet_md"].split()), j(packet, {}), log_id))
    conn.commit()
    return packet


@router.get("/recall-logs/by-packet/{packet_id}")
def get_recall_log_by_packet(request: Request, packet_id: str) -> dict[str, Any]:
    row = require(db(request).execute("SELECT packet_json FROM recall_logs WHERE packet_id=?", (packet_id,)).fetchone(), "recall_log")
    return json.loads(row["packet_json"])


@router.get("/scoring-defaults/readback")
def scoring_readback() -> dict[str, Any]:
    return scoring_defaults_readback()


@router.post("/source-refs")
def attach_source_ref(request: Request, payload: dict[str, Any]) -> dict[str, Any]:
    sid = execute_insert(db(request), "INSERT INTO source_refs(target_kind,target_id,source_kind,source_project_id,source_id,source_locator_json,source_summary,observed_at,verified_at,verification_status,created_by) VALUES (?,?,?,?,?,?,?,?,?,?,?)", (payload["target_kind"], payload["target_id"], payload["source_kind"], payload.get("source_project_id"), payload["source_id"], j(payload.get("source_locator", {}), {}), payload.get("source_summary", ""), payload.get("observed_at"), payload.get("verified_at"), payload.get("verification_status", "unverified"), payload.get("created_by", "api")))
    return decode_row(db(request).execute("SELECT * FROM source_refs WHERE id=?", (sid,)).fetchone())


@router.get("/source-refs")
def list_source_refs(request: Request, target_kind: str | None = None, target_id: int | None = None, verification_status: str | None = None, limit: int = Query(50, ge=1, le=500)) -> dict[str, Any]:
    clauses=[]; params=[]
    if target_kind: clauses.append("target_kind=?"); params.append(target_kind)
    if target_id is not None: clauses.append("target_id=?"); params.append(target_id)
    if verification_status: clauses.append("verification_status=?"); params.append(verification_status)
    sql="SELECT * FROM source_refs" + (" WHERE "+" AND ".join(clauses) if clauses else "") + " ORDER BY id DESC LIMIT ?"
    params.append(limit)
    return {"items": rows(db(request).execute(sql, tuple(params)))}


def list_event_table(request: Request, table: str, limit: int) -> dict[str, Any]:
    return {"items": rows(db(request).execute(f"SELECT * FROM {table} ORDER BY id DESC LIMIT ?", (limit,)))}


def get_event_row(request: Request, table: str, event_id: int, label: str) -> dict[str, Any]:
    return decode_row(require(db(request).execute(f"SELECT * FROM {table} WHERE id=?", (event_id,)).fetchone(), label))


@router.get("/capture-events")
def list_capture_events(request: Request, limit: int = Query(50, ge=1, le=500)) -> dict[str, Any]:
    return list_event_table(request, "capture_events", limit)


@router.get("/capture-events/recent")
def recent_capture_events(request: Request, limit: int = Query(50, ge=1, le=500)) -> dict[str, Any]:
    return list_event_table(request, "capture_events", limit)


@router.get("/capture-events/{event_id}")
def get_capture_event(request: Request, event_id: int) -> dict[str, Any]:
    return get_event_row(request, "capture_events", event_id, "capture_event")


@router.get("/curation-events")
def list_curation_events(request: Request, action: str | None = None, actor_identity: str | None = None, reason_contains: str | None = None, limit: int = Query(50, ge=1, le=500)) -> dict[str, Any]:
    clauses = []
    params: list[Any] = []
    if action:
        clauses.append("action=?")
        params.append(action)
    if actor_identity:
        clauses.append("actor_identity=?")
        params.append(actor_identity)
    if reason_contains:
        clauses.append("reason LIKE ?")
        params.append(f"%{reason_contains}%")
    sql = "SELECT * FROM curation_events" + (" WHERE " + " AND ".join(clauses) if clauses else "") + " ORDER BY id DESC LIMIT ?"
    params.append(limit)
    return {"items": rows(db(request).execute(sql, tuple(params)))}


@router.get("/curation-events/{event_id}")
def get_curation_event(request: Request, event_id: int) -> dict[str, Any]:
    return get_event_row(request, "curation_events", event_id, "curation_event")




# Observability, doctor, and audit export surfaces. These are read-only and are safe for
# independent auditors because they expose pipeline artifacts directly without invoking recall.

def count_one(conn: sqlite3.Connection, sql: str, params: tuple[Any, ...] = ()) -> int:
    return int(conn.execute(sql, params).fetchone()[0])


def time_filter_clause(since: str | None = None, until: str | None = None, column: str = "created_at") -> tuple[list[str], list[Any]]:
    clauses: list[str] = []
    params: list[Any] = []
    if since:
        clauses.append(f"{column} >= ?")
        params.append(since)
    if until:
        clauses.append(f"{column} <= ?")
        params.append(until)
    return clauses, params


def select_rows(conn: sqlite3.Connection, table: str, clauses: list[str], params: list[Any], *, order: str = "id DESC", limit: int = 50) -> list[dict[str, Any]]:
    sql = f"SELECT * FROM {table}"
    if clauses:
        sql += " WHERE " + " AND ".join(clauses)
    sql += f" ORDER BY {order} LIMIT ?"
    return rows(conn.execute(sql, (*params, limit)))


@router.get("/observability/summary")
def observability_summary(request: Request) -> dict[str, Any]:
    conn = db(request)
    return {
        "counts": {
            "capture_events": count_one(conn, "SELECT COUNT(*) FROM capture_events"),
            "captured_events": count_one(conn, "SELECT COUNT(*) FROM capture_events WHERE decision='captured'"),
            "filtered_events": count_one(conn, "SELECT COUNT(*) FROM capture_events WHERE decision='filtered'"),
            "errored_events": count_one(conn, "SELECT COUNT(*) FROM capture_events WHERE decision='errored'"),
            "pending_candidates": count_one(conn, "SELECT COUNT(*) FROM memory_candidates WHERE status='pending'"),
            "curation_events": count_one(conn, "SELECT COUNT(*) FROM curation_events"),
            "recall_logs": count_one(conn, "SELECT COUNT(*) FROM recall_logs"),
            "broken_source_refs": count_one(conn, "SELECT COUNT(*) FROM source_refs WHERE verification_status='broken'"),
            "unverified_source_refs": count_one(conn, "SELECT COUNT(*) FROM source_refs WHERE verification_status='unverified'"),
        },
        "recent_ids": {
            "capture_event_ids": [row["id"] for row in conn.execute("SELECT id FROM capture_events ORDER BY id DESC LIMIT 10")],
            "pending_candidate_ids": [row["id"] for row in conn.execute("SELECT id FROM memory_candidates WHERE status='pending' ORDER BY id DESC LIMIT 10")],
            "curation_event_ids": [row["id"] for row in conn.execute("SELECT id FROM curation_events ORDER BY id DESC LIMIT 10")],
            "recall_log_ids": [row["id"] for row in conn.execute("SELECT id FROM recall_logs ORDER BY id DESC LIMIT 10")],
        },
    }


@router.get("/observability/pending-candidates")
def observability_pending_candidates(request: Request, scope_kind: str | None = None, scope_id: str | None = None, source_kind: str | None = None, limit: int = Query(50, ge=1, le=500)) -> dict[str, Any]:
    clauses = ["status='pending'"]
    params: list[Any] = []
    join = ""
    if scope_kind:
        clauses.append("scope_kind=?")
        params.append(scope_kind)
    if scope_id:
        clauses.append("scope_id=?")
        params.append(scope_id)
    if source_kind:
        join = " JOIN json_each(memory_candidates.source_refs_json) source_ref"
        clauses.append("json_extract(source_ref.value, '$.source_kind')=?")
        params.append(source_kind)
    sql = "SELECT DISTINCT memory_candidates.* FROM memory_candidates" + join + " WHERE " + " AND ".join(clauses) + " ORDER BY memory_candidates.id DESC LIMIT ?"
    items = rows(db(request).execute(sql, (*params, limit)))
    return {"count": len(items), "items": items}


@router.get("/observability/curation-timeline")
def observability_curation_timeline(request: Request, actor_identity: str | None = None, action: str | None = None, since: str | None = None, until: str | None = None, limit: int = Query(50, ge=1, le=500)) -> dict[str, Any]:
    clauses, params = time_filter_clause(since, until)
    if actor_identity:
        clauses.append("actor_identity=?")
        params.append(actor_identity)
    if action:
        clauses.append("action=?")
        params.append(action)
    items = select_rows(db(request), "curation_events", clauses, params, limit=limit)
    return {"count": len(items), "items": items}


@router.get("/observability/recall-logs")
def observability_recall_logs(request: Request, packet_id: str | None = None, actor: str | None = None, project_id: str | None = None, since: str | None = None, until: str | None = None, limit: int = Query(50, ge=1, le=500)) -> dict[str, Any]:
    clauses, params = time_filter_clause(since, until)
    if packet_id:
        clauses.append("packet_id=?")
        params.append(packet_id)
    if actor:
        clauses.append("created_by=?")
        params.append(actor)
    if project_id:
        clauses.append("(json_extract(request_json, '$.scope_id')=? OR json_extract(request_json, '$.project_id')=?)")
        params.extend([project_id, project_id])
    items = select_rows(db(request), "recall_logs", clauses, params, limit=limit)
    return {"count": len(items), "items": items}


def issue(kind: str, severity: str, summary: str, ids: list[int], details: dict[str, Any] | None = None) -> dict[str, Any]:
    return {"kind": kind, "severity": severity, "summary": summary, "count": len(ids), "ids": ids, "details": details or {}}


def doctor_issues(conn: sqlite3.Connection) -> list[dict[str, Any]]:
    issues: list[dict[str, Any]] = []
    missing_ref_candidate_ids = [row["id"] for row in conn.execute("SELECT id FROM memory_candidates WHERE source_refs_json='[]'")]
    missing_ref_entry_ids = [row["id"] for row in conn.execute("SELECT e.id FROM memory_entries e LEFT JOIN source_refs s ON s.target_kind='memory_entry' AND s.target_id=e.id WHERE s.id IS NULL")]
    if missing_ref_candidate_ids or missing_ref_entry_ids:
        issues.append(issue("missing_source_refs", "warning", "Candidates or entries without source refs", missing_ref_candidate_ids + missing_ref_entry_ids, {"candidate_ids": missing_ref_candidate_ids, "memory_entry_ids": missing_ref_entry_ids}))
    broken_ids = [row["id"] for row in conn.execute("SELECT id FROM source_refs WHERE verification_status IN ('broken','unverified')")]
    if broken_ids:
        issues.append(issue("broken_or_unverified_source_refs", "warning", "Source refs are broken or unverified", broken_ids))
    unscoped = [row["id"] for row in conn.execute("SELECT id FROM memory_candidates WHERE scope_kind='global' AND scope_id IS NULL AND authority_scope_kind='global' AND authority_scope_id IS NULL")]
    if unscoped:
        issues.append(issue("unscoped_candidates", "warning", "Candidates remain globally scoped without explicit review", unscoped))
    broad = [row["id"] for row in conn.execute("""
        SELECT e.id FROM memory_entries e
        JOIN source_refs s ON s.target_kind='memory_entry' AND s.target_id=e.id
        WHERE e.authority_scope_kind='global' AND s.source_project_id IS NOT NULL
        GROUP BY e.id HAVING COUNT(s.id)=1
    """)]
    if broad:
        issues.append(issue("broad_authority_from_narrow_source", "warning", "Global authority memory backed by a single project-scoped source", broad))
    duplicate_candidate_ids = [row["id"] for row in conn.execute("""
        SELECT c.id FROM memory_candidates c
        JOIN (SELECT lower(trim(body_md)) body_key FROM memory_candidates WHERE length(trim(body_md)) > 0 GROUP BY lower(trim(body_md)) HAVING COUNT(*) > 1) d
          ON lower(trim(c.body_md)) = d.body_key
    """)]
    if duplicate_candidate_ids:
        issues.append(issue("duplicate_candidate_bodies", "info", "Multiple candidates share the same normalized body", duplicate_candidate_ids))
    secret_ids: list[str] = []
    marker_sql = " OR ".join(["lower(title || ' ' || summary || ' ' || body_md) LIKE ?" for _ in SECRET_MARKERS])
    marker_params = tuple(f"%{marker.lower()}%" for marker in SECRET_MARKERS)
    for row in conn.execute(f"SELECT 'candidate:' || id AS ref FROM memory_candidates WHERE {marker_sql}", marker_params):
        secret_ids.append(row["ref"])
    for row in conn.execute(f"SELECT 'entry:' || id AS ref FROM memory_entries WHERE {marker_sql}", marker_params):
        secret_ids.append(row["ref"])
    if secret_ids:
        issues.append({"kind": "secret_like_strings", "severity": "critical", "summary": "Secret/token-like strings found in memory text", "count": len(secret_ids), "ids": secret_ids, "details": {"markers_checked": list(SECRET_MARKERS)}})
    return issues


@router.get("/doctor/report")
def doctor_report(request: Request) -> dict[str, Any]:
    conn = db(request)
    issues = doctor_issues(conn)
    return {
        "doctor_version": "v0-stub",
        "read_only": True,
        "counts": {
            "issues": len(issues),
            "memory_entries": count_one(conn, "SELECT COUNT(*) FROM memory_entries"),
            "memory_candidates": count_one(conn, "SELECT COUNT(*) FROM memory_candidates"),
            "source_refs": count_one(conn, "SELECT COUNT(*) FROM source_refs"),
            "capture_events": count_one(conn, "SELECT COUNT(*) FROM capture_events"),
            "curation_events": count_one(conn, "SELECT COUNT(*) FROM curation_events"),
            "recall_logs": count_one(conn, "SELECT COUNT(*) FROM recall_logs"),
        },
        "issues": issues,
    }


@router.post("/doctor/report")
def doctor_report_post(request: Request) -> dict[str, Any]:
    return doctor_report(request)


def audit_snapshot(conn: sqlite3.Connection, since: str | None = None, until: str | None = None, limit: int = 500) -> dict[str, Any]:
    clauses, params = time_filter_clause(since, until)
    return {
        "metadata": {"format": "den-memories-v0-audit-export", "recall_used": False, "since": since, "until": until},
        "counts": observability_summary_for_conn(conn)["counts"],
        "capture_events": select_rows(conn, "capture_events", clauses, params, limit=limit),
        "memory_candidates": select_rows(conn, "memory_candidates", clauses, params, limit=limit),
        "memory_entries": select_rows(conn, "memory_entries", clauses, params, limit=limit),
        "source_refs": select_rows(conn, "source_refs", clauses, params, limit=limit),
        "curation_events": select_rows(conn, "curation_events", clauses, params, limit=limit),
        "recall_logs": select_rows(conn, "recall_logs", clauses, params, limit=limit),
        "doctor": {"read_only": True, "issues": doctor_issues(conn)},
    }


def observability_summary_for_conn(conn: sqlite3.Connection) -> dict[str, Any]:
    return {
        "counts": {
            "capture_events": count_one(conn, "SELECT COUNT(*) FROM capture_events"),
            "memory_candidates": count_one(conn, "SELECT COUNT(*) FROM memory_candidates"),
            "memory_entries": count_one(conn, "SELECT COUNT(*) FROM memory_entries"),
            "source_refs": count_one(conn, "SELECT COUNT(*) FROM source_refs"),
            "curation_events": count_one(conn, "SELECT COUNT(*) FROM curation_events"),
            "recall_logs": count_one(conn, "SELECT COUNT(*) FROM recall_logs"),
        }
    }


def audit_markdown(snapshot: dict[str, Any]) -> str:
    lines = ["# Den Memories v0 audit export", "", "Recall used: `false`", "", "## Counts"]
    for key, value in snapshot["counts"].items():
        lines.append(f"- {key}: {value}")
    lines.append("")
    lines.append("## Doctor issues")
    for item in snapshot["doctor"]["issues"]:
        lines.append(f"- **{item['kind']}** ({item['severity']}): {item['summary']} ids={item['ids']}")
    if not snapshot["doctor"]["issues"]:
        lines.append("- none")
    lines.append("")
    lines.append("## Drill-down IDs")
    for table in ("capture_events", "memory_candidates", "memory_entries", "source_refs", "curation_events", "recall_logs"):
        lines.append(f"- {table}: {[item.get('id') for item in snapshot[table]]}")
    return "\n".join(lines) + "\n"


@router.get("/audit/export")
def audit_export(request: Request, format: str = "jsonl", since: str | None = None, until: str | None = None, limit: int = Query(500, ge=1, le=2000)) -> Response:
    snapshot = audit_snapshot(db(request), since, until, limit)
    if format == "json":
        return Response(json.dumps(snapshot, sort_keys=True), media_type="application/json")
    if format == "markdown":
        return Response(audit_markdown(snapshot), media_type="text/markdown")
    if format != "jsonl":
        raise HTTPException(status_code=400, detail="unsupported_audit_export_format")
    lines = [json.dumps({"record_type": "metadata", **snapshot["metadata"], "counts": snapshot["counts"]}, sort_keys=True)]
    for table in ("capture_events", "memory_candidates", "memory_entries", "source_refs", "curation_events", "recall_logs"):
        for item in snapshot[table]:
            lines.append(json.dumps({"record_type": table, **item}, sort_keys=True))
    lines.append(json.dumps({"record_type": "doctor", **snapshot["doctor"]}, sort_keys=True))
    return Response("\n".join(lines) + "\n", media_type="application/x-ndjson")


@router.get("/recall-logs")
def list_recall_logs(request: Request, packet_id: str | None = None, actor: str | None = None, project_id: str | None = None, since: str | None = None, until: str | None = None, limit: int = Query(50, ge=1, le=500)) -> dict[str, Any]:
    return observability_recall_logs(request, packet_id=packet_id, actor=actor, project_id=project_id, since=since, until=until, limit=limit)


@router.get("/recall-logs/{event_id}")
def get_recall_log(request: Request, event_id: int) -> dict[str, Any]:
    return get_event_row(request, "recall_logs", event_id, "recall_log")
