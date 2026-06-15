from __future__ import annotations

import sqlite3

import pytest

from den_memories.app import REQUIRED_V0_TABLES
from den_memories.db import apply_migrations, connect, table_names


def test_migrations_create_required_v0_tables_and_are_idempotent(tmp_path):
    conn = connect(tmp_path / "memories.sqlite")
    try:
        first = apply_migrations(conn)
        second = apply_migrations(conn)
        assert first == ["001_v0_schema", "002_candidate_fts", "003_recall_packet_json"]
        assert second == []
        assert REQUIRED_V0_TABLES <= table_names(conn)
        assert conn.execute("SELECT COUNT(*) FROM schema_migrations").fetchone()[0] == 3
    finally:
        conn.close()


@pytest.mark.parametrize(
    ("table", "column", "bad_value", "insert_sql", "params"),
    [
        ("memory_entries", "layer", "captured_observation", "INSERT INTO memory_entries(slug,title,kind,layer,created_by,updated_by) VALUES (?,?,?,?,?,?)", ("bad-entry", "Bad", "fact", "captured_observation", "test", "test")),
        ("memory_candidates", "status", "approved", "INSERT INTO memory_candidates(title,proposed_kind,status,created_by) VALUES (?,?,?,?)", ("Bad", "fact", "approved", "test")),
        ("topic_nodes", "claim_strength", "law", "INSERT INTO topic_nodes(slug,title,node_type,claim_strength,created_by,updated_by) VALUES (?,?,?,?,?,?)", ("bad-node", "Bad", "fact", "law", "test", "test")),
        ("topic_edges", "relation", "causes", "INSERT INTO topic_nodes(slug,title,node_type,created_by,updated_by) VALUES ('a','A','fact','test','test'); INSERT INTO topic_nodes(slug,title,node_type,created_by,updated_by) VALUES ('b','B','fact','test','test'); INSERT INTO topic_edges(from_node_id,to_node_id,relation,created_by) VALUES (1,2,?,?)", ("causes", "test")),
        ("capture_events", "decision", "silently_dropped", "INSERT INTO capture_events(event_kind,actor_identity,runtime,proposed_scope_kind,capture_policy_id,decision) VALUES (?,?,?,?,?,?)", ("turn", "test", "manual", "project", "policy", "silently_dropped")),
        ("curation_events", "action", "approve", "INSERT INTO curation_events(action,actor_identity) VALUES (?,?)", ("approve", "test")),
    ],
)
def test_invalid_enum_values_are_rejected(tmp_path, table, column, bad_value, insert_sql, params):
    conn = connect(tmp_path / "memories.sqlite")
    try:
        apply_migrations(conn)
        with pytest.raises(sqlite3.IntegrityError):
            if ";" in insert_sql:
                statements = [part.strip() for part in insert_sql.split(";") if part.strip()]
                for stmt in statements[:-1]:
                    conn.execute(stmt)
                conn.execute(statements[-1], params)
            else:
                conn.execute(insert_sql, params)
    finally:
        conn.close()


def test_topic_edges_allow_conditional_duplicates_but_reject_exact_duplicate(tmp_path):
    conn = connect(tmp_path / "memories.sqlite")
    try:
        apply_migrations(conn)
        conn.execute("INSERT INTO topic_nodes(slug,title,node_type,created_by,updated_by) VALUES ('a','A','fact','test','test')")
        conn.execute("INSERT INTO topic_nodes(slug,title,node_type,created_by,updated_by) VALUES ('b','B','fact','test','test')")
        conn.execute("INSERT INTO topic_edges(from_node_id,to_node_id,relation,condition_hash,created_by) VALUES (1,2,'warning','runner','test')")
        conn.execute("INSERT INTO topic_edges(from_node_id,to_node_id,relation,condition_hash,created_by) VALUES (1,2,'warning','reviewer','test')")
        with pytest.raises(sqlite3.IntegrityError):
            conn.execute("INSERT INTO topic_edges(from_node_id,to_node_id,relation,condition_hash,created_by) VALUES (1,2,'warning','runner','test')")
    finally:
        conn.close()
