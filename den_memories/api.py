from __future__ import annotations

import json
import sqlite3
from typing import Any

from fastapi import APIRouter, HTTPException, Query, Request

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
    cid = execute_insert(conn, """INSERT INTO memory_candidates(proposed_slug,title,body_md,summary,proposed_kind,layer,scope_kind,scope_id,authority_scope_kind,authority_scope_id,discovery_scope,claim_strength,audience_json,source_refs_json,extraction_confidence,status,created_by,updated_by) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)""", (
        payload.get("proposed_slug"), payload["title"], payload.get("body_md", ""), payload.get("summary", ""), payload["proposed_kind"], payload.get("layer", "candidate"), payload.get("scope_kind", "global"), payload.get("scope_id"), payload.get("authority_scope_kind", "global"), payload.get("authority_scope_id"), payload.get("discovery_scope", "explicit_only"), payload.get("claim_strength", "observation"), j(payload.get("audience", [])), j(payload.get("source_refs", [])), payload.get("extraction_confidence"), payload.get("status", "pending"), payload.get("created_by", "api"), payload.get("updated_by", payload.get("created_by", "api"))))
    refresh_candidate_fts(conn, cid, payload)
    capture_id = execute_insert(conn, """INSERT INTO capture_events(event_kind,source_refs_json,actor_identity,runtime,proposed_scope_kind,proposed_scope_id,capture_policy_id,decision,reason,candidate_ids_json,raw_size,extracted_size) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)""", (
        payload.get("event_kind", "manual_note"), j(payload.get("source_refs", [])), payload.get("created_by", "api"), payload.get("runtime", "manual"), payload.get("scope_kind", "global"), payload.get("scope_id"), payload.get("capture_policy_id", "api-candidate-create"), "captured", payload.get("reason", "candidate created"), j([cid]), len(payload.get("body_md", "")), len(payload.get("summary", ""))))
    conn.commit()
    item = decode_row(conn.execute("SELECT * FROM memory_candidates WHERE id=?", (cid,)).fetchone())
    item["capture_event_id"] = capture_id
    return item


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
def delete_edge(request: Request, edge_id: int, actor_identity: str = "api") -> dict[str, Any]:
    conn = db(request)
    before = decode_row(require(conn.execute("SELECT * FROM topic_edges WHERE id=?", (edge_id,)).fetchone(), "topic_edge"))
    log_curation(conn, action="unlink", actor=actor_identity, reason="edge deleted", before=before)
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


@router.get("/capture-events/{event_id}")
def get_capture_event(request: Request, event_id: int) -> dict[str, Any]:
    return get_event_row(request, "capture_events", event_id, "capture_event")


@router.get("/curation-events")
def list_curation_events(request: Request, limit: int = Query(50, ge=1, le=500)) -> dict[str, Any]:
    return list_event_table(request, "curation_events", limit)


@router.get("/curation-events/{event_id}")
def get_curation_event(request: Request, event_id: int) -> dict[str, Any]:
    return get_event_row(request, "curation_events", event_id, "curation_event")


@router.get("/recall-logs")
def list_recall_logs(request: Request, limit: int = Query(50, ge=1, le=500)) -> dict[str, Any]:
    return list_event_table(request, "recall_logs", limit)


@router.get("/recall-logs/{event_id}")
def get_recall_log(request: Request, event_id: int) -> dict[str, Any]:
    return get_event_row(request, "recall_logs", event_id, "recall_log")
