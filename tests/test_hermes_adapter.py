from __future__ import annotations

import hashlib
import json
from typing import Any

from fastapi.testclient import TestClient

from den_memories.app import create_app
from den_memories.config import Settings
from den_memories.hermes_adapter import DEN_MEMORY_TOOL_NAMES, DenMemoryHermesProvider, create_provider_from_config


class ServiceClientAdapter:
    def __init__(self, client: TestClient):
        self.client = client

    def request_json(self, method: str, path: str, *, payload: dict[str, Any] | None = None, params: dict[str, Any] | None = None) -> Any:
        response = self.client.request(method, path, json=payload, params=params)
        response.raise_for_status()
        return response.json()


class FailingClient:
    def request_json(self, method: str, path: str, *, payload: dict[str, Any] | None = None, params: dict[str, Any] | None = None) -> Any:
        raise RuntimeError("service down")


def client(tmp_path):
    app = create_app(Settings(database_path=tmp_path / "hermes-adapter.sqlite"))
    return TestClient(app)


def provider(c: TestClient) -> DenMemoryHermesProvider:
    p = create_provider_from_config({"enabled": True}, client=ServiceClientAdapter(c))
    p.initialize(
        "session-1",
        platform="den_channels",
        agent_identity="den-mcp-runner",
        profile_id="den-mcp-runner",
        project_id="den-memory",
        task_id=2475,
        role="runner",
        mode="implementation",
        agent_context="primary",
    )
    return p


def seed_memory(c: TestClient) -> None:
    capture = c.post("/api/capture", json={
        "runtime": "hermes",
        "actor_identity": "fixture",
        "raw_text": "Hermes prompt cache invariant means mid-session captures cannot mutate system prompt.",
        "title": "Hermes prompt cache invariant",
        "summary": "Prompt cache invariant",
        "proposed_kind": "fact",
        "scope_kind": "project",
        "scope_id": "den-memory",
        "authority_scope_kind": "project",
        "authority_scope_id": "den-memory",
        "discovery_scope": "same_project",
        "claim_strength": "policy",
    })
    # policy-strength capture is intentionally filtered by service; use a direct entry for recall.
    assert capture.status_code == 200
    entry = c.post("/api/memory-entries", json={
        "slug": "hermes-prompt-cache-invariant",
        "title": "Hermes prompt cache invariant",
        "kind": "policy",
        "body_md": "Hermes prompt cache invariant means mid-session captures cannot mutate system prompt.",
        "summary": "Prompt cache invariant",
        "scope_kind": "project",
        "scope_id": "den-memory",
        "authority_scope_kind": "project",
        "authority_scope_id": "den-memory",
        "discovery_scope": "same_project",
        "claim_strength": "policy",
        "status": "active",
        "created_by": "fixture",
    })
    assert entry.status_code == 200, entry.text


def test_tool_schemas_are_shared_names_and_no_collisions(tmp_path):
    with client(tmp_path) as c:
        p = provider(c)
        schemas = p.get_tool_schemas()
        names = [schema["name"] for schema in schemas]
        assert set(names) == DEN_MEMORY_TOOL_NAMES
        assert len(names) == len(set(names))
        for schema in schemas:
            assert schema["parameters"]["additionalProperties"] is False


def test_manual_recall_returns_service_packet_and_prompt_block_stays_static(tmp_path):
    with client(tmp_path) as c:
        seed_memory(c)
        p = provider(c)
        before = p.system_prompt_block()
        before_hash = hashlib.sha256(before.encode()).hexdigest()

        recall = json.loads(p.handle_tool_call("den_memory_recall", {"query": "prompt cache", "scope_kind": "project", "scope_id": "den-memory"}))
        assert recall["ok"] is True
        assert recall["data"]["packet_id"]
        assert recall["data"]["included_nodes"][0]["slug"] == "hermes-prompt-cache-invariant"
        assert p.prefetch("prompt cache") == ""

        capture = json.loads(p.handle_tool_call("den_memory_capture_event", {"raw_text": "Adapter capture should create a pending candidate", "title": "Adapter capture", "summary": "Adapter capture", "event_kind": "manual_note"}))
        assert capture["ok"] is True
        assert capture["data"]["decision"] == "captured"
        assert capture["data"]["candidate"]["status"] == "pending"
        assert capture["data"]["capture_event_id"]

        after = p.system_prompt_block()
        after_hash = hashlib.sha256(after.encode()).hexdigest()
        assert after == before
        assert after_hash == before_hash


def test_store_candidate_search_read_and_doctor_tools(tmp_path):
    with client(tmp_path) as c:
        seed_memory(c)
        p = provider(c)
        stored = json.loads(p.handle_tool_call("den_memory_store_candidate", {"title": "Candidate from Hermes", "body_md": "candidate body", "proposed_kind": "fact"}))
        assert stored["ok"] is True
        assert stored["data"]["status"] == "pending"
        assert stored["data"]["scope_id"] == "den-memory"
        assert stored["data"]["created_by"] == "den-mcp-runner"

        search = json.loads(p.handle_tool_call("den_memory_search", {"query": "candidate", "include_candidates": True}))
        assert search["ok"] is True
        assert search["candidates"]["ok"] is True
        assert search["candidates"]["data"]["items"]

        read = json.loads(p.handle_tool_call("den_memory_read", {"slug": "hermes-prompt-cache-invariant"}))
        assert read["ok"] is True
        assert read["data"]["slug"] == "hermes-prompt-cache-invariant"

        doctor = json.loads(p.handle_tool_call("den_memory_doctor", {}))
        assert doctor["ok"] is True
        assert doctor["data"]["read_only"] is True


def test_worker_context_capture_is_metadata_only_and_auditor_provider_disabled(tmp_path):
    with client(tmp_path) as c:
        worker = create_provider_from_config({"enabled": True}, client=ServiceClientAdapter(c))
        worker.initialize("worker-session", agent_identity="spawned-coder", project_id="den-memory", task_id=2475, session_kind="worker_assignment", role="worker")
        capture = json.loads(worker.handle_tool_call("den_memory_capture_event", {"raw_text": "worker metadata only", "title": "Worker metadata"}))
        assert capture["ok"] is True
        assert capture["data"]["decision"] == "ignored"
        assert capture["data"]["reason"] == "metadata_only"

        auditor = create_provider_from_config({"enabled": True}, client=ServiceClientAdapter(c))
        auditor.initialize("audit-session", agent_identity="independent-auditor", project_id="den-memory", session_kind="diagnostic", role="auditor")
        assert auditor.is_available() is False
        assert auditor.system_prompt_block() == ""
        assert auditor.get_tool_schemas() == []
        disabled_result = json.loads(auditor.handle_tool_call("den_memory_recall", {"query": "anything"}))
        assert disabled_result["ok"] is False
        assert disabled_result["error"] == "den_memory_provider_disabled"


def test_provider_failures_are_non_fatal_and_return_json_errors():
    p = create_provider_from_config({"enabled": True}, client=FailingClient())
    p.initialize("session-1", agent_identity="den-mcp-runner", project_id="den-memory")
    result = json.loads(p.handle_tool_call("den_memory_doctor", {}))
    assert result["ok"] is False
    assert "service down" in result["error"]
    assert p.prefetch("query") == ""
