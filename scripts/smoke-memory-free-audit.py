#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import sys
import urllib.request
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[1]
if str(REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(REPO_ROOT))

from den_memories.auditor import (
    DEFAULT_AUDITOR_PROFILE,
    audit_export_jsonl,
    report_markdown,
    validate_auditor_profile,
)


def read_export(args: argparse.Namespace) -> str:
    if args.export_file:
        return Path(args.export_file).read_text(encoding="utf-8")
    if args.service_url:
        base = args.service_url.rstrip("/")
        with urllib.request.urlopen(f"{base}/api/audit/export?format=jsonl", timeout=args.timeout) as response:
            return response.read().decode("utf-8")
    raise SystemExit("Provide --export-file or --service-url")


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Run a memory-free Den Memories audit over an audit export.")
    parser.add_argument("--export-file", help="Path to JSONL from /api/audit/export?format=jsonl")
    parser.add_argument("--service-url", help="Den Memories service URL; reads /api/audit/export only")
    parser.add_argument("--tool-list", help="Optional JSON file containing runtime tool names for profile allow-list validation")
    parser.add_argument("--profile", help="Optional JSON config readback for the auditor profile")
    parser.add_argument("--markdown-out", help="Optional path to write markdown report")
    parser.add_argument("--timeout", type=float, default=5.0)
    args = parser.parse_args(argv)

    profile = DEFAULT_AUDITOR_PROFILE
    if args.profile:
        profile = json.loads(Path(args.profile).read_text(encoding="utf-8"))
    tool_names = []
    if args.tool_list:
        loaded = json.loads(Path(args.tool_list).read_text(encoding="utf-8"))
        tool_names = loaded if isinstance(loaded, list) else loaded.get("tools", [])
    profile_report = validate_auditor_profile(profile, tool_names)
    if not profile_report["ok"]:
        print(json.dumps({"profile_ok": False, "profile_report": profile_report}, indent=2, sort_keys=True), file=sys.stderr)
        return 2

    export_text = read_export(args)
    report = audit_export_jsonl(export_text)
    markdown = report_markdown(report)
    if args.markdown_out:
        Path(args.markdown_out).write_text(markdown, encoding="utf-8")
    print(markdown)
    return 0 if report["ok"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
