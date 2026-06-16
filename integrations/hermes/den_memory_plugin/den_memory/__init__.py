"""Hermes memory-provider plugin for the Den Memories v0 service.

This selected-profile plugin wraps the dependency-light adapter in
``den_memory_hermes_adapter.py`` and registers it with Hermes' MemoryProvider
loader. It is intentionally manual-only by default: no automatic recall, no
turn sync, and a static prompt block.
"""
from __future__ import annotations

from typing import Any

import yaml

from agent.memory_provider import MemoryProvider
from hermes_constants import get_hermes_home

try:  # runtime plugin install copies the adapter beside this file
    from .den_memory_hermes_adapter import DenMemoryHermesProvider
except ModuleNotFoundError:  # source-tree tests import the adapter from integrations/hermes
    from integrations.hermes.den_memory_hermes_adapter import DenMemoryHermesProvider


class HermesDenMemoryServiceProvider(DenMemoryHermesProvider, MemoryProvider):
    """MemoryProvider adapter for selected-profile Den Memories rollout."""

    def _config(self) -> dict[str, Any]:
        config_path = get_hermes_home() / "config.yaml"
        if not config_path.exists():
            return {}
        data = yaml.safe_load(config_path.read_text(encoding="utf-8")) or {}
        if not isinstance(data, dict):
            return {}
        raw = data.get("den_memory") or {}
        return raw if isinstance(raw, dict) else {}

    def is_available(self) -> bool:
        cfg = self._config()
        return bool(self.enabled and cfg.get("enabled") is True and cfg.get("service_url"))

    def initialize(self, session_id: str, **kwargs: Any) -> None:
        cfg = self._config()
        if cfg.get("deny_auto_behavior") is not True:
            raise RuntimeError("den_memory.deny_auto_behavior must be true for selected-profile v0 rollout")
        if cfg.get("enabled") is not True:
            raise RuntimeError("den_memory.enabled must be true when memory.provider=den_memory")
        runtime_context = cfg.get("runtime_context") or {}
        if not isinstance(runtime_context, dict):
            runtime_context = {}
        init_kwargs = {**runtime_context, **kwargs}
        if init_kwargs.get("agent_context") == "flush" and "session_kind" not in kwargs:
            init_kwargs["session_kind"] = "diagnostic"
        init_kwargs.setdefault("agent_identity", cfg.get("profile"))
        init_kwargs.setdefault("profile_id", cfg.get("profile"))
        init_kwargs.setdefault("session_kind", "durable_agent")
        init_kwargs.setdefault("role", cfg.get("role", "assistant"))
        init_kwargs.setdefault("mode", cfg.get("mode", "general"))
        init_kwargs.setdefault("project_id", cfg.get("project_id", "den-memory"))
        init_kwargs.setdefault("source_surface", "hermes")
        super().initialize(
            session_id,
            service_url=str(cfg.get("service_url") or ""),
            auto_recall=bool(cfg.get("auto_recall", False)),
            capture_on_sync=bool(cfg.get("capture_on_sync", False)),
            **init_kwargs,
        )


def register(ctx: Any) -> None:
    ctx.register_memory_provider(HermesDenMemoryServiceProvider())
