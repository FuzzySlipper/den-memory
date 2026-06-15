from __future__ import annotations

import json
import unittest

from typing import Any

from integrations.hermes.den_memory_hermes_adapter import DEN_MEMORY_TOOL_NAMES, DenMemoryHermesProvider


class RecordingClient:
    def __init__(self) -> None:
        self.calls: list[tuple[str, str, dict[str, Any] | None, dict[str, Any] | None]] = []

    def request_json(self, method: str, path: str, *, payload: dict[str, Any] | None = None, params: dict[str, Any] | None = None) -> dict[str, Any]:
        self.calls.append((method, path, payload, params))
        return {"packet_id": "packet-1"}


class HermesAdapterContractTests(unittest.TestCase):
    def test_v0_deferred_curator_tools_are_not_advertised(self) -> None:
        provider = DenMemoryHermesProvider(client=object())  # type: ignore[arg-type]
        provider.initialize("session", agent_identity="runner", project_id="den-memory", role="runner")
        names = {schema["name"] for schema in provider.get_tool_schemas()}
        self.assertEqual(names, DEN_MEMORY_TOOL_NAMES)
        self.assertNotIn("den_memory_link", names)
        self.assertNotIn("den_memory_curate", names)

    def test_unknown_deferred_curator_tool_fails_non_fatally(self) -> None:
        provider = DenMemoryHermesProvider(client=object())  # type: ignore[arg-type]
        provider.initialize("session", agent_identity="runner", project_id="den-memory", role="runner")
        result = json.loads(provider.handle_tool_call("den_memory_curate", {"action": "promote"}))
        self.assertFalse(result["ok"])
        self.assertEqual(result["error"], "unknown_den_memory_tool:den_memory_curate")

    def test_independent_auditor_gets_no_memory_tools(self) -> None:
        provider = DenMemoryHermesProvider(client=object())  # type: ignore[arg-type]
        provider.initialize("audit-session", agent_identity="auditor", project_id="den-memory", role="auditor", session_kind="diagnostic")
        self.assertFalse(provider.is_available())
        self.assertEqual(provider.get_tool_schemas(), [])

    def test_recall_schema_and_payload_include_v0_required_budget_fields(self) -> None:
        client = RecordingClient()
        provider = DenMemoryHermesProvider(client=client)
        provider.initialize("session", agent_identity="runner", project_id="den-memory", role="runner")

        recall_schema = next(schema for schema in provider.get_tool_schemas() if schema["name"] == "den_memory_recall")
        self.assertIn("budget_tokens", recall_schema["parameters"]["properties"])
        self.assertIn("include_candidates", recall_schema["parameters"]["properties"])
        self.assertEqual(recall_schema["parameters"]["required"], ["query", "budget_tokens", "include_candidates"])

        result = json.loads(provider.handle_tool_call("den_memory_recall", {"query": "adapter contract"}))
        self.assertTrue(result["ok"])
        self.assertEqual(client.calls[0][0:2], ("POST", "/api/recall"))
        payload = client.calls[0][2]
        assert payload is not None
        self.assertEqual(payload["budget_tokens"], 3000)
        self.assertIs(payload["include_candidates"], False)
        self.assertIn("runtime_context", payload)


if __name__ == "__main__":
    unittest.main()
