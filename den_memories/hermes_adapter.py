from __future__ import annotations

import json
import os
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass, field
from typing import Any, Protocol


STATIC_PROMPT_BLOCK = (
    "Den Memories is available only through explicit manual tools. "
    "Automatic recall/prefetch is disabled by default; candidate capture does not "
    "promote truth or mutate the system prompt during a session."
)

DEN_MEMORY_TOOL_NAMES = {
    "den_memory_recall",
    "den_memory_read",
    "den_memory_search",
    "den_memory_store_candidate",
    "den_memory_capture_event",
    "den_memory_doctor",
}


class DenMemoryServiceClient(Protocol):
    def request_json(self, method: str, path: str, *, payload: dict[str, Any] | None = None, params: dict[str, Any] | None = None) -> Any:
        ...


@dataclass
class UrllibDenMemoryClient:
    """Small stdlib HTTP client for Den Memories service APIs.

    Kept dependency-free so it can run inside a Hermes plugin/overlay without
    forcing a new runtime package dependency.
    """

    base_url: str
    timeout: float = 5.0

    def request_json(self, method: str, path: str, *, payload: dict[str, Any] | None = None, params: dict[str, Any] | None = None) -> Any:
        base = self.base_url.rstrip("/")
        url = f"{base}{path}"
        if params:
            clean = {key: value for key, value in params.items() if value is not None}
            if clean:
                url += "?" + urllib.parse.urlencode(clean)
        data = None
        headers = {"Accept": "application/json"}
        if payload is not None:
            data = json.dumps(payload).encode("utf-8")
            headers["Content-Type"] = "application/json"
        request = urllib.request.Request(url, data=data, headers=headers, method=method.upper())
        try:
            with urllib.request.urlopen(request, timeout=self.timeout) as response:
                raw = response.read().decode("utf-8")
                if not raw:
                    return {}
                return json.loads(raw)
        except urllib.error.HTTPError as exc:
            body = exc.read().decode("utf-8", errors="replace")
            raise RuntimeError(f"Den Memories HTTP {exc.code}: {body}") from exc
        except (urllib.error.URLError, TimeoutError, json.JSONDecodeError) as exc:
            raise RuntimeError(f"Den Memories request failed: {exc}") from exc


@dataclass
class DenMemoryHermesProvider:
    """Hermes-compatible Den Memories provider/adapter.

    The class mirrors Hermes' MemoryProvider lifecycle without importing Hermes,
    so this repo can ship and test the adapter boundary independently. A Den-owned
    plugin/overlay can subclass/register this class with Hermes' MemoryProvider.
    """

    service_url: str | None = None
    enabled: bool = True
    auto_recall: bool = False
    capture_on_sync: bool = False
    client: DenMemoryServiceClient | None = None
    runtime_context: dict[str, Any] = field(default_factory=dict)
    session_id: str = ""

    @property
    def name(self) -> str:
        return "den_memory"

    def is_available(self) -> bool:
        return bool(self.enabled and (self.client or self.service_url or os.getenv("DEN_MEMORY_URL")))

    def initialize(self, session_id: str, **kwargs: Any) -> None:
        self.session_id = session_id
        self.service_url = self.service_url or kwargs.get("service_url") or os.getenv("DEN_MEMORY_URL")
        if self.client is None and self.service_url:
            self.client = UrllibDenMemoryClient(self.service_url)
        self.auto_recall = bool(kwargs.get("auto_recall", self.auto_recall))
        self.capture_on_sync = bool(kwargs.get("capture_on_sync", self.capture_on_sync))
        self.runtime_context = self._runtime_context_from_kwargs(session_id, kwargs)
        if self.runtime_context.get("role") == "auditor" or self.runtime_context.get("session_kind") == "diagnostic":
            # Independent auditors may use explicit audit exports, not recall/search provider tools.
            self.enabled = False

    def _runtime_context_from_kwargs(self, session_id: str, kwargs: dict[str, Any]) -> dict[str, Any]:
        agent_identity = kwargs.get("agent_identity") or kwargs.get("profile_id") or kwargs.get("profile")
        session_kind = kwargs.get("session_kind") or {
            "subagent": "assistant_delegate",
            "cron": "cron",
            "primary": "durable_agent",
            "flush": "diagnostic",
        }.get(str(kwargs.get("agent_context", "primary")), "durable_agent")
        role = kwargs.get("role") or ("worker" if session_kind in {"worker_assignment", "assistant_delegate"} else "assistant")
        return {
            "runtime": "hermes",
            "agent_identity": agent_identity,
            "profile_id": kwargs.get("profile_id") or agent_identity,
            "agent_instance_id": kwargs.get("agent_instance_id"),
            "session_id": session_id,
            "session_key": kwargs.get("session_key") or session_id,
            "session_kind": session_kind,
            "project_id": kwargs.get("project_id"),
            "task_id": kwargs.get("task_id"),
            "assignment_id": kwargs.get("assignment_id"),
            "run_id": kwargs.get("run_id"),
            "role": role,
            "audience": kwargs.get("audience") or [role],
            "mode": kwargs.get("mode", "general"),
            "source_surface": kwargs.get("platform", "cli"),
            "user_id": kwargs.get("user_id"),
        }

    def system_prompt_block(self) -> str:
        # Static by construction: never embeds service data, candidates, packets, or session writes.
        return STATIC_PROMPT_BLOCK if self.enabled else ""

    def prefetch(self, query: str, *, session_id: str = "") -> str:
        if not (self.enabled and self.auto_recall):
            return ""
        result = self._safe_call("POST", "/api/recall", {"query": query, "runtime_context": self.runtime_context})
        if not result.get("ok"):
            return ""
        packet = result["data"]
        return "<memory-context>\n" + str(packet.get("packet_md", ""))[:4000] + "\n</memory-context>"

    def sync_turn(self, user_content: str, assistant_content: str, *, session_id: str = "") -> None:
        if not (self.enabled and self.capture_on_sync):
            return
        self._safe_call("POST", "/api/capture", self._capture_payload({"raw_text": user_content + "\n\n" + assistant_content, "event_kind": "turn"}))

    def get_tool_schemas(self) -> list[dict[str, Any]]:
        if not self.enabled:
            return []
        return [
            self._schema("den_memory_recall", "Read-only guided recall packet from Den Memories.", {"query": {"type": "string"}, "scope_kind": {"type": "string"}, "scope_id": {"type": "string"}, "topic_view_slug": {"type": "string"}, "limit": {"type": "integer", "minimum": 1, "maximum": 50}}, ["query"]),
            self._schema("den_memory_read", "Read a Den Memories entry by slug.", {"slug": {"type": "string"}}, ["slug"]),
            self._schema("den_memory_search", "Search Den Memories entries and candidates.", {"query": {"type": "string"}, "include_candidates": {"type": "boolean"}, "limit": {"type": "integer", "minimum": 1, "maximum": 50}}, ["query"]),
            self._schema("den_memory_store_candidate", "Create a pending Den Memories candidate only; never promotes curated memory.", {"title": {"type": "string"}, "body_md": {"type": "string"}, "summary": {"type": "string"}, "proposed_kind": {"type": "string"}, "scope_kind": {"type": "string"}, "scope_id": {"type": "string"}, "authority_scope_kind": {"type": "string"}, "authority_scope_id": {"type": "string"}, "discovery_scope": {"type": "string"}, "claim_strength": {"type": "string"}, "source_refs": {"type": "array", "items": {"type": "object"}}}, ["title", "body_md", "proposed_kind"]),
            self._schema("den_memory_capture_event", "Send a runtime capture attempt through Den Memories capture policy.", {"raw_text": {"type": "string"}, "event_kind": {"type": "string"}, "title": {"type": "string"}, "summary": {"type": "string"}, "scope_kind": {"type": "string"}, "scope_id": {"type": "string"}, "source_refs": {"type": "array", "items": {"type": "object"}}}, ["raw_text"]),
            self._schema("den_memory_doctor", "Read-only Den Memories doctor report.", {}, []),
        ]

    def _schema(self, name: str, description: str, properties: dict[str, Any], required: list[str]) -> dict[str, Any]:
        return {"name": name, "description": description, "parameters": {"type": "object", "properties": properties, "required": required, "additionalProperties": False}}

    def handle_tool_call(self, tool_name: str, args: dict[str, Any], **kwargs: Any) -> str:
        if tool_name not in DEN_MEMORY_TOOL_NAMES:
            return json.dumps({"ok": False, "error": f"unknown_den_memory_tool:{tool_name}"})
        if not self.enabled:
            return json.dumps({"ok": False, "error": "den_memory_provider_disabled"})
        if tool_name == "den_memory_recall":
            payload = {**args, "runtime_context": self.runtime_context, "scope_kind": args.get("scope_kind") or "project", "scope_id": args.get("scope_id") or self.runtime_context.get("project_id")}
            return json.dumps(self._safe_call("POST", "/api/recall", payload), sort_keys=True)
        if tool_name == "den_memory_read":
            return json.dumps(self._safe_call("GET", f"/api/memory-entries/{urllib.parse.quote(str(args['slug']))}"), sort_keys=True)
        if tool_name == "den_memory_search":
            entry_result = self._safe_call("POST", "/api/memory-entries/search", {"query": args["query"], "limit": args.get("limit", 10)})
            if args.get("include_candidates"):
                cand_result = self._safe_call("POST", "/api/candidates/search", {"query": args["query"], "limit": args.get("limit", 10)})
                return json.dumps({"ok": entry_result.get("ok") and cand_result.get("ok"), "entries": entry_result, "candidates": cand_result}, sort_keys=True)
            return json.dumps(entry_result, sort_keys=True)
        if tool_name == "den_memory_store_candidate":
            return json.dumps(self._safe_call("POST", "/api/candidates", self._candidate_payload(args)), sort_keys=True)
        if tool_name == "den_memory_capture_event":
            return json.dumps(self._safe_call("POST", "/api/capture", self._capture_payload(args)), sort_keys=True)
        if tool_name == "den_memory_doctor":
            return json.dumps(self._safe_call("GET", "/api/doctor/report"), sort_keys=True)
        return json.dumps({"ok": False, "error": f"unhandled_den_memory_tool:{tool_name}"})

    def _candidate_payload(self, args: dict[str, Any]) -> dict[str, Any]:
        ctx = self.runtime_context
        return {
            **args,
            "created_by": ctx.get("agent_identity") or "hermes",
            "scope_kind": args.get("scope_kind") or ("project" if ctx.get("project_id") else "global"),
            "scope_id": args.get("scope_id") or ctx.get("project_id"),
            "authority_scope_kind": args.get("authority_scope_kind") or ("project" if ctx.get("project_id") else "global"),
            "authority_scope_id": args.get("authority_scope_id") or ctx.get("project_id"),
            "discovery_scope": args.get("discovery_scope", "explicit_only"),
            "claim_strength": args.get("claim_strength", "observation"),
            "runtime_context": ctx,
        }

    def _capture_payload(self, args: dict[str, Any]) -> dict[str, Any]:
        ctx = self.runtime_context
        session_kind = ctx.get("session_kind")
        role = ctx.get("role")
        actor_role = "worker" if session_kind in {"worker_assignment", "assistant_delegate"} or role == "worker" else role
        return {
            **args,
            "runtime": "hermes",
            "actor_identity": ctx.get("agent_identity") or "hermes",
            "actor_role": actor_role,
            "scope_kind": args.get("scope_kind") or ("project" if ctx.get("project_id") else "global"),
            "scope_id": args.get("scope_id") or ctx.get("project_id"),
            "source_refs": args.get("source_refs") or self._default_source_refs(),
            "runtime_context": ctx,
        }

    def _default_source_refs(self) -> list[dict[str, Any]]:
        ctx = self.runtime_context
        if ctx.get("task_id"):
            return [{"source_kind": "den_task", "source_project_id": ctx.get("project_id"), "source_id": str(ctx["task_id"]), "source_locator": {"session_id": self.session_id}, "verification_status": "unverified"}]
        return [{"source_kind": "hermes_session", "source_project_id": ctx.get("project_id"), "source_id": self.session_id or "unknown", "source_locator": {"profile_id": ctx.get("profile_id")}, "verification_status": "unverified"}]

    def _safe_call(self, method: str, path: str, payload: dict[str, Any] | None = None, params: dict[str, Any] | None = None) -> dict[str, Any]:
        if not self.client:
            return {"ok": False, "error": "den_memory_service_unavailable"}
        try:
            return {"ok": True, "data": self.client.request_json(method, path, payload=payload, params=params)}
        except Exception as exc:  # Non-fatal provider behavior is intentional.
            return {"ok": False, "error": str(exc), "path": path}


def create_provider_from_config(config: dict[str, Any] | None = None, *, client: DenMemoryServiceClient | None = None) -> DenMemoryHermesProvider:
    config = config or {}
    return DenMemoryHermesProvider(
        service_url=config.get("service_url") or os.getenv("DEN_MEMORY_URL"),
        enabled=bool(config.get("enabled", True)),
        auto_recall=bool(config.get("auto_recall", False)),
        capture_on_sync=bool(config.get("capture_on_sync", False)),
        client=client,
    )
