from __future__ import annotations

from contextlib import asynccontextmanager
from pathlib import Path
from typing import Any

from fastapi import FastAPI

from . import __version__
from .config import Settings
from .db import apply_migrations, connect, table_names
from .registry import load_registry, load_scoring_defaults
from .api import router as api_router

REQUIRED_V0_TABLES = {
    "memory_entries",
    "memory_candidates",
    "topic_nodes",
    "topic_edges",
    "topic_views",
    "source_refs",
    "capture_events",
    "curation_events",
    "recall_logs",
    "schema_migrations",
}


def create_app(settings: Settings | None = None) -> FastAPI:
    settings = settings or Settings.from_env()

    @asynccontextmanager
    async def lifespan(app: FastAPI):
        conn = connect(settings.database_path)
        applied = apply_migrations(conn)
        app.state.db = conn
        app.state.applied_migrations = applied
        try:
            yield
        finally:
            conn.close()

    app = FastAPI(title="Den Memories", version=__version__, lifespan=lifespan)
    app.state.settings = settings
    app.include_router(api_router)

    @app.get("/health")
    def health() -> dict[str, Any]:
        conn = app.state.db
        existing = table_names(conn)
        missing = sorted(REQUIRED_V0_TABLES - existing)
        return {
            "ok": not missing,
            "service": settings.service_name,
            "version": __version__,
            "contract_version": settings.contract_version,
            "database_path": str(settings.database_path),
            "missing_tables": missing,
        }

    @app.get("/api/version")
    def version() -> dict[str, Any]:
        conn = app.state.db
        migrations = [row["version"] for row in conn.execute("SELECT version FROM schema_migrations ORDER BY version")]
        scoring = load_scoring_defaults()
        return {
            "service": settings.service_name,
            "version": __version__,
            "contract_version": settings.contract_version,
            "migrations": migrations,
            "registry_version": load_registry()["contract_version"],
            "scoring_profile": scoring["profile"],
        }

    @app.get("/api/registry")
    def registry() -> dict[str, Any]:
        return load_registry()

    @app.get("/api/scoring-defaults")
    def scoring_defaults() -> dict[str, Any]:
        return load_scoring_defaults()

    return app


app = create_app()
