#!/usr/bin/env python3
"""Validate Den Memories v0 contract examples."""
from __future__ import annotations

import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
if __name__ == "__main__":
    raise SystemExit(subprocess.call([sys.executable, "-m", "pytest", "tests/test_contracts.py", "-q"], cwd=ROOT))
