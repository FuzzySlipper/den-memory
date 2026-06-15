from __future__ import annotations

from dataclasses import dataclass
import os
from pathlib import Path


@dataclass(frozen=True)
class Settings:
    database_path: Path
    service_name: str = "den-memories"
    contract_version: str = "v0"

    @classmethod
    def from_env(cls) -> "Settings":
        db_path = os.environ.get("DEN_MEMORIES_DB", "./runtime/den-memories.sqlite")
        return cls(database_path=Path(db_path))
