from __future__ import annotations

import json
from functools import lru_cache
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[1]
REGISTRY_PATH = ROOT / "contracts" / "v0" / "registry.json"
SCORING_PATH = ROOT / "contracts" / "v0" / "scoring-defaults.json"


@lru_cache(maxsize=1)
def load_registry() -> dict[str, Any]:
    return json.loads(REGISTRY_PATH.read_text(encoding="utf-8"))


@lru_cache(maxsize=1)
def load_scoring_defaults() -> dict[str, Any]:
    return json.loads(SCORING_PATH.read_text(encoding="utf-8"))


def require_registry_value(category: str, value: str) -> None:
    registry = load_registry()
    allowed = set(registry[category])
    if value not in allowed:
        raise ValueError(f"invalid {category} value {value!r}; expected one of {sorted(allowed)}")
