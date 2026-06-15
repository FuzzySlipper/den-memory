from __future__ import annotations

from pathlib import Path

from fastapi.testclient import TestClient

from den_memories.app import create_app
from den_memories.config import Settings


def client(tmp_path):
    app = create_app(Settings(database_path=tmp_path / "crud.sqlite"))
    return TestClient(app)


def test_entry_crud_search_source_refs_and_events(tmp_path):
    with client(tmp_path) as c:
        created = c.post("/api/memory-entries", json={
            "slug": "core-boundary", "title": "Den Core boundary", "kind": "policy", "body_md": "Den Core remains workflow truth.",
            "scope_kind": "project", "scope_id": "den-memory", "authority_scope_kind": "global", "discovery_scope": "global_discoverable", "claim_strength": "policy", "created_by": "test"
        })
        assert created.status_code == 200, created.text
        body = created.json()
        assert body["authority_scope_kind"] == "global"
        assert body["discovery_scope"] == "global_discoverable"
        assert body["claim_strength"] == "policy"

        assert c.get("/api/memory-entries/core-boundary").json()["title"] == "Den Core boundary"
        assert c.post("/api/memory-entries/search", json={"query": "workflow", "limit": 5}).json()["items"][0]["slug"] == "core-boundary"

        ref = c.post("/api/source-refs", json={"target_kind": "memory_entry", "target_id": body["id"], "source_kind": "den_task", "source_project_id": "den-memory", "source_id": "2469", "source_locator": {"task_id": 2469}, "verification_status": "verified", "created_by": "test"})
        assert ref.status_code == 200, ref.text
        refs = c.get(f"/api/source-refs?target_kind=memory_entry&target_id={body['id']}").json()["items"]
        assert refs[0]["verification_status"] == "verified"

        archived = c.post("/api/memory-entries/core-boundary/archive", json={"actor_identity": "test", "reason": "archive smoke"})
        assert archived.json()["status"] == "archived"
        assert c.get("/api/curation-events").json()["items"]


def test_candidate_create_filter_search_status_and_capture_event(tmp_path):
    with client(tmp_path) as c:
        res = c.post("/api/candidates", json={
            "title": "Noisy capture about plugin X", "body_md": "Plugin X failed in project A", "summary": "Plugin X failure", "proposed_kind": "warning",
            "scope_kind": "project", "scope_id": "project-a", "authority_scope_kind": "project", "authority_scope_id": "project-a", "discovery_scope": "global_discoverable", "claim_strength": "assessment", "runtime": "hermes", "created_by": "agent-a"
        })
        assert res.status_code == 200, res.text
        item = res.json()
        assert item["status"] == "pending"
        assert item["capture_event_id"]
        assert c.get("/api/candidates?status=pending&scope_kind=project&scope_id=project-a&runtime=hermes&actor=agent-a").json()["items"][0]["id"] == item["id"]
        assert c.post("/api/candidates/search", json={"query": "Plugin", "limit": 5}).json()["items"][0]["id"] == item["id"]
        updated = c.patch(f"/api/candidates/{item['id']}/status", json={"status": "rejected", "actor_identity": "curator", "reason": "noise"})
        assert updated.json()["status"] == "rejected"
        events = c.get("/api/curation-events").json()["items"]
        assert events[0]["action"] == "reject"
        capture = c.get(f"/api/capture-events/{item['capture_event_id']}").json()
        assert capture["decision"] == "captured"


def test_nodes_edges_views_and_neighbor_readback(tmp_path):
    with client(tmp_path) as c:
        a = c.post("/api/topic-nodes", json={"slug": "root", "title": "Root", "node_type": "concept", "summary": "implementation root", "scope_kind": "project", "authority_scope_kind": "project", "discovery_scope": "same_project", "claim_strength": "policy", "created_by": "test"}).json()
        b = c.post("/api/topic-nodes", json={"slug": "risk", "title": "Risk", "node_type": "warning", "summary": "review risk", "created_by": "test"}).json()
        assert c.post("/api/topic-nodes/search", json={"query": "implementation", "limit": 5}).json()["items"][0]["slug"] == "root"
        edge = c.post("/api/topic-edges", json={"from_node_id": a["id"], "to_node_id": b["id"], "relation": "warning", "condition_hash": "runner", "created_by": "test"})
        assert edge.status_code == 200, edge.text
        neighbors = c.get("/api/topic-nodes/root/neighbors").json()["items"]
        assert neighbors[0]["to_slug"] == "risk"
        view = c.post("/api/topic-views", json={"slug": "implementation-path", "title": "Implementation path", "root_node_id": a["id"], "view_type": "implementation_path", "mode": "implementation", "include_relations": ["warning"], "created_by": "test"})
        assert view.status_code == 200, view.text
        assert c.get("/api/topic-views/implementation-path").json()["mode"] == "implementation"
        assert c.get("/api/topic-views").json()["items"][0]["slug"] == "implementation-path"
        assert c.delete(f"/api/topic-edges/{edge.json()['id']}?actor_identity=test").json()["deleted"] is True



def test_upgrade_from_original_external_content_fts_schema(tmp_path):
    from den_memories.db import apply_migrations, connect
    from den_memories.config import Settings
    from den_memories.app import create_app

    db_path = tmp_path / "upgrade.sqlite"
    original_001 = tmp_path / "001_v0_schema.sql"
    current_001 = Path(__file__).resolve().parents[1] / "den_memories" / "migrations" / "001_v0_schema.sql"
    sql = current_001.read_text(encoding="utf-8")
    sql = sql.replace(
        "CREATE VIRTUAL TABLE IF NOT EXISTS memory_entries_fts USING fts5(slug, title, summary, body_md, tags_json, content='memory_entries', content_rowid='id');",
        "CREATE VIRTUAL TABLE IF NOT EXISTS memory_entries_fts USING fts5(slug, title, summary, body_md, tags_json, content='memory_entries', content_rowid='id');",
    )
    sql = sql.replace(
        "CREATE VIRTUAL TABLE IF NOT EXISTS topic_nodes_fts USING fts5(slug, title, summary, content='topic_nodes', content_rowid='id');",
        "CREATE VIRTUAL TABLE IF NOT EXISTS topic_nodes_fts USING fts5(slug, title, summary, content='topic_nodes', content_rowid='id');",
    )
    original_001.write_text(sql, encoding="utf-8")
    conn = connect(db_path)
    try:
        assert apply_migrations(conn, [original_001]) == ["001_v0_schema"]
    finally:
        conn.close()

    with TestClient(create_app(Settings(database_path=db_path))) as c:
        assert c.get("/api/version").json()["migrations"] == ["001_v0_schema", "002_candidate_fts", "003_recall_packet_json"]
        created = c.post("/api/memory-entries", json={
            "slug": "upgrade-entry", "title": "Upgrade Entry", "kind": "fact", "body_md": "upgrade fts works", "created_by": "test"
        })
        assert created.status_code == 200, created.text
        assert c.post("/api/memory-entries/search", json={"query": "upgrade", "limit": 5}).json()["items"][0]["slug"] == "upgrade-entry"
