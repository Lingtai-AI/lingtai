"""Tests for validate_recipe.py — the canonical .lingtai-recipe/ validator."""
import json
import subprocess
import sys
from pathlib import Path

import pytest

SCRIPT = Path(__file__).parent / "validate_recipe.py"


def _make_valid_recipe(root: Path) -> None:
    """Populate `root` with the minimum valid recipe payload."""
    (root / "recipe.json").write_text(
        json.dumps({"name": "Test Recipe", "description": "A test recipe"}),
        encoding="utf-8",
    )
    recipe_dir = root / ".lingtai-recipe" / "en"
    recipe_dir.mkdir(parents=True)
    (recipe_dir / "greet.md").write_text("Hello from the test recipe.", encoding="utf-8")
    (recipe_dir / "comment.md").write_text("Be concise.", encoding="utf-8")


def test_valid_recipe_passes(tmp_path: Path) -> None:
    _make_valid_recipe(tmp_path)
    result = subprocess.run(
        [sys.executable, str(SCRIPT), str(tmp_path)],
        capture_output=True, text=True,
    )
    assert result.returncode == 0, result.stdout + result.stderr
