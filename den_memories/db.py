from __future__ import annotations

import sqlite3
from pathlib import Path
from typing import Iterable

MIGRATIONS_DIR = Path(__file__).resolve().parent / "migrations"


def connect(database_path: Path | str) -> sqlite3.Connection:
    path = Path(database_path)
    if path != Path(":memory:"):
        path.parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(path, check_same_thread=False)
    conn.row_factory = sqlite3.Row
    conn.execute("PRAGMA foreign_keys = ON")
    return conn


def migration_files() -> list[Path]:
    return sorted(MIGRATIONS_DIR.glob("*.sql"))


def apply_migrations(conn: sqlite3.Connection, files: Iterable[Path] | None = None) -> list[str]:
    files = list(files or migration_files())
    conn.execute(
        "CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT (datetime('now')))"
    )
    applied: list[str] = []
    known = {row["version"] for row in conn.execute("SELECT version FROM schema_migrations")}
    for path in files:
        version = path.stem
        if version in known:
            continue
        conn.executescript(path.read_text(encoding="utf-8"))
        conn.execute("INSERT INTO schema_migrations(version) VALUES (?)", (version,))
        applied.append(version)
    conn.commit()
    return applied


def table_names(conn: sqlite3.Connection) -> set[str]:
    return {
        row["name"]
        for row in conn.execute("SELECT name FROM sqlite_master WHERE type IN ('table', 'view')")
    }
