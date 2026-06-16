#!/usr/bin/env python3
"""Install the Den Memories Hermes memory-provider plugin into selected profiles."""
from __future__ import annotations

import argparse
import json
import os
import shutil
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

import yaml

REPO_ROOT = Path(__file__).resolve().parents[1]
PLUGIN_SRC = REPO_ROOT / "integrations" / "hermes" / "den_memory_plugin" / "den_memory"
ADAPTER_SRC = REPO_ROOT / "integrations" / "hermes" / "den_memory_hermes_adapter.py"
DEFAULT_PROFILES = ["kate", "researcher", "house-admin"]
DEFAULT_SERVICE_URL = "http://192.168.1.10:8780"
DEFAULT_RUNTIME_ROOT = Path("/home/agents/runtime/den-memory-hermes-plugin/den_memory")
DEFAULT_PROFILES_ROOT = Path("/home/agents/profiles")


def load_yaml(path: Path) -> dict[str, Any]:
    if not path.exists():
        return {}
    data = yaml.safe_load(path.read_text(encoding="utf-8")) or {}
    return data if isinstance(data, dict) else {}


def dump_yaml(path: Path, data: dict[str, Any]) -> None:
    path.write_text(yaml.safe_dump(data, sort_keys=False, allow_unicode=True), encoding="utf-8")


def profile_project_and_role(profile: str, cfg: dict[str, Any]) -> tuple[str, str]:
    meta = cfg.get("profile_metadata")
    if not isinstance(meta, dict):
        meta = {}
    existing_den = cfg.get("den_memory")
    if not isinstance(existing_den, dict):
        existing_den = {}
    project = str(meta.get("project_id") or existing_den.get("project_id") or "den-memory")
    role = str(meta.get("role") or existing_den.get("role") or "assistant")
    return project, role


def install_runtime_plugin(runtime_root: Path) -> None:
    runtime_root.mkdir(parents=True, exist_ok=True)
    for item in runtime_root.iterdir():
        if item.is_dir():
            shutil.rmtree(item)
        else:
            item.unlink()
    for src in PLUGIN_SRC.iterdir():
        dst = runtime_root / src.name
        if src.is_dir():
            shutil.copytree(src, dst)
        else:
            shutil.copy2(src, dst)
    shutil.copy2(ADAPTER_SRC, runtime_root / "den_memory_hermes_adapter.py")


def configure_profile(profile: str, profiles_root: Path, runtime_root: Path, service_url: str, dry_run: bool) -> dict[str, Any]:
    home = profiles_root / profile
    config_path = home / "config.yaml"
    if not config_path.exists():
        raise FileNotFoundError(f"missing config: {config_path}")
    cfg = load_yaml(config_path)
    project_id, role = profile_project_and_role(profile, cfg)

    memory = cfg.setdefault("memory", {})
    if not isinstance(memory, dict):
        raise TypeError(f"{config_path}: memory section is not a mapping")
    memory["provider"] = "den_memory"
    memory.setdefault("memory_enabled", True)

    plugins = cfg.setdefault("plugins", {})
    if not isinstance(plugins, dict):
        raise TypeError(f"{config_path}: plugins section is not a mapping")
    enabled = plugins.setdefault("enabled", [])
    if not isinstance(enabled, list):
        raise TypeError(f"{config_path}: plugins.enabled is not a list")
    if "den_memory" not in enabled:
        enabled.append("den_memory")

    cfg["den_memory"] = {
        "enabled": True,
        "service_url": service_url,
        "deny_auto_behavior": True,
        "auto_recall": False,
        "capture_on_sync": False,
        "profile": profile,
        "project_id": project_id,
        "role": role,
        "mode": "general",
        "runtime_context": {
            "runtime": "hermes",
            "agent_identity": profile,
            "profile_id": profile,
            "session_kind": "durable_agent",
            "project_id": project_id,
            "role": role,
            "audience": [role],
            "mode": "general",
            "source_surface": "hermes",
        },
    }

    plugin_link = home / "plugins" / "den_memory"
    if not dry_run:
        timestamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
        backup = config_path.with_suffix(config_path.suffix + f".bak-den-memory-service-{timestamp}")
        shutil.copy2(config_path, backup)
        plugin_link.parent.mkdir(parents=True, exist_ok=True)
        if plugin_link.is_symlink() or plugin_link.exists():
            if plugin_link.is_symlink() or plugin_link.is_file():
                plugin_link.unlink()
            else:
                backup_dir = plugin_link.with_name(plugin_link.name + f".bak-{timestamp}")
                plugin_link.rename(backup_dir)
        plugin_link.symlink_to(runtime_root)
        dump_yaml(config_path, cfg)
    return {"profile": profile, "project_id": project_id, "role": role, "plugin_link": str(plugin_link), "provider": "den_memory"}


def smoke_profile(profile: str, profiles_root: Path) -> dict[str, Any]:
    env = os.environ.copy()
    env["HERMES_HOME"] = str(profiles_root / profile)
    code = f'''
import json
from plugins.memory import load_memory_provider
provider = load_memory_provider("den_memory")
if provider is None:
    raise SystemExit("provider not loaded")
print(json.dumps({{"available": provider.is_available(), "name": provider.name}}))
provider.initialize("task-2500-smoke", platform="cli", agent_identity={profile!r})
schemas = provider.get_tool_schemas()
recall = json.loads(provider.handle_tool_call("den_memory_recall", {{"query":"den memory", "budget_tokens": 3000, "include_candidates": False, "scope_kind":"project"}}))
prompt_before = provider.system_prompt_block()
provider.sync_turn("hello", "world", session_id="task-2500-smoke")
prompt_after = provider.system_prompt_block()
print(json.dumps({{
  "schema_names": [s["name"] for s in schemas],
  "recall_ok": recall.get("ok"),
  "packet_id": (recall.get("data") or {{}}).get("packet_id"),
  "prompt_hash_stable": hash(prompt_before) == hash(prompt_after),
  "prompt_block": prompt_before,
}}, sort_keys=True))
'''
    proc = subprocess.run([sys.executable, "-c", code], env=env, text=True, capture_output=True, check=False)
    return {"profile": profile, "returncode": proc.returncode, "stdout": proc.stdout, "stderr": proc.stderr}


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--profiles", default=",".join(DEFAULT_PROFILES), help="Comma-separated profile names")
    ap.add_argument("--profiles-root", type=Path, default=DEFAULT_PROFILES_ROOT)
    ap.add_argument("--runtime-root", type=Path, default=DEFAULT_RUNTIME_ROOT)
    ap.add_argument("--service-url", default=DEFAULT_SERVICE_URL)
    ap.add_argument("--dry-run", action="store_true")
    ap.add_argument("--skip-smoke", action="store_true")
    args = ap.parse_args()

    profiles = [p.strip() for p in args.profiles.split(",") if p.strip()]
    if not profiles:
        raise SystemExit("no profiles selected")

    if not args.dry_run:
        install_runtime_plugin(args.runtime_root)

    results = []
    for profile in profiles:
        results.append(configure_profile(profile, args.profiles_root, args.runtime_root, args.service_url, args.dry_run))

    smokes = []
    if not args.dry_run and not args.skip_smoke:
        for profile in profiles:
            smokes.append(smoke_profile(profile, args.profiles_root))

    print(json.dumps({"dry_run": args.dry_run, "runtime_root": str(args.runtime_root), "service_url": args.service_url, "profiles": results, "smokes": smokes}, indent=2, sort_keys=True))
    failed = [s for s in smokes if s["returncode"] != 0]
    return 1 if failed else 0


if __name__ == "__main__":
    raise SystemExit(main())
