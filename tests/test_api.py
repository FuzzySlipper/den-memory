from __future__ import annotations

from fastapi.testclient import TestClient

from den_memories.app import create_app
from den_memories.config import Settings


def test_health_and_version_endpoints_initialize_schema(tmp_path):
    app = create_app(Settings(database_path=tmp_path / "api.sqlite"))
    with TestClient(app) as client:
        health = client.get("/health")
        assert health.status_code == 200
        assert health.json()["ok"] is True
        assert health.json()["contract_version"] == "v0"
        assert health.json()["missing_tables"] == []

        version = client.get("/api/version")
        assert version.status_code == 200
        body = version.json()
        assert body["service"] == "den-memories"
        assert body["contract_version"] == "v0"
        assert body["migrations"] == ["001_v0_schema", "002_candidate_fts"]
        assert body["registry_version"] == "v0"
        assert body["scoring_profile"] == "v0-default"


def test_registry_and_scoring_readback(tmp_path):
    app = create_app(Settings(database_path=tmp_path / "api.sqlite"))
    with TestClient(app) as client:
        registry = client.get("/api/registry").json()
        assert "curated_fact" in registry["layers"]
        assert "permissive_candidates" in registry["capture_policy_modes"]

        scoring = client.get("/api/scoring-defaults").json()
        assert scoring["profile"] == "v0-default"
        assert scoring["root_match"]["authoritative_scope_match"] > scoring["root_match"]["discoverable_scope_match"]
