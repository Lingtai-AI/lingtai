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


def test_missing_recipe_json(tmp_path: Path) -> None:
    _make_valid_recipe(tmp_path)
    (tmp_path / "recipe.json").unlink()
    result = subprocess.run(
        [sys.executable, str(SCRIPT), str(tmp_path)],
        capture_output=True, text=True,
    )
    assert result.returncode == 1
    assert "recipe.json" in result.stdout
    assert "missing" in result.stdout


def test_recipe_json_invalid_json(tmp_path: Path) -> None:
    _make_valid_recipe(tmp_path)
    (tmp_path / "recipe.json").write_text("{not valid", encoding="utf-8")
    result = subprocess.run(
        [sys.executable, str(SCRIPT), str(tmp_path)],
        capture_output=True, text=True,
    )
    assert result.returncode == 1
    assert "invalid JSON" in result.stdout


def test_recipe_json_missing_fields(tmp_path: Path) -> None:
    _make_valid_recipe(tmp_path)
    (tmp_path / "recipe.json").write_text(json.dumps({"name": ""}), encoding="utf-8")
    result = subprocess.run(
        [sys.executable, str(SCRIPT), str(tmp_path)],
        capture_output=True, text=True,
    )
    assert result.returncode == 1
    assert "name" in result.stdout
    assert "description" in result.stdout


def test_missing_recipe_dir(tmp_path: Path) -> None:
    (tmp_path / "recipe.json").write_text(
        json.dumps({"name": "X", "description": "Y"}), encoding="utf-8"
    )
    result = subprocess.run(
        [sys.executable, str(SCRIPT), str(tmp_path)],
        capture_output=True, text=True,
    )
    assert result.returncode == 1
    assert ".lingtai-recipe" in result.stdout


def test_missing_greet_md(tmp_path: Path) -> None:
    _make_valid_recipe(tmp_path)
    (tmp_path / ".lingtai-recipe" / "en" / "greet.md").unlink()
    result = subprocess.run(
        [sys.executable, str(SCRIPT), str(tmp_path)],
        capture_output=True, text=True,
    )
    assert result.returncode == 1
    assert "greet.md" in result.stdout


def test_missing_comment_md(tmp_path: Path) -> None:
    _make_valid_recipe(tmp_path)
    (tmp_path / ".lingtai-recipe" / "en" / "comment.md").unlink()
    result = subprocess.run(
        [sys.executable, str(SCRIPT), str(tmp_path)],
        capture_output=True, text=True,
    )
    assert result.returncode == 1
    assert "comment.md" in result.stdout


def test_root_level_component_accepted(tmp_path: Path) -> None:
    """greet.md and comment.md at .lingtai-recipe/ root (no lang subdir) is valid.

    Regression guard: this layout must produce neither errors nor warnings.
    """
    (tmp_path / "recipe.json").write_text(
        json.dumps({"name": "X", "description": "Y"}), encoding="utf-8"
    )
    recipe_dir = tmp_path / ".lingtai-recipe"
    recipe_dir.mkdir()
    (recipe_dir / "greet.md").write_text("hi", encoding="utf-8")
    (recipe_dir / "comment.md").write_text("be kind", encoding="utf-8")
    result = subprocess.run(
        [sys.executable, str(SCRIPT), str(tmp_path)],
        capture_output=True, text=True,
    )
    assert result.returncode == 0, result.stdout
    assert "WARN" not in result.stdout, result.stdout


def test_forbidden_placeholder_in_comment(tmp_path: Path) -> None:
    _make_valid_recipe(tmp_path)
    (tmp_path / ".lingtai-recipe" / "en" / "comment.md").write_text(
        "Current time: {{time}}", encoding="utf-8"
    )
    result = subprocess.run(
        [sys.executable, str(SCRIPT), str(tmp_path)],
        capture_output=True, text=True,
    )
    assert result.returncode == 1
    assert "{{time}}" in result.stdout
    assert "comment.md" in result.stdout


def test_unknown_lang_is_warning(tmp_path: Path) -> None:
    _make_valid_recipe(tmp_path)
    odd = tmp_path / ".lingtai-recipe" / "fr"
    odd.mkdir()
    (odd / "greet.md").write_text("bonjour", encoding="utf-8")
    (odd / "comment.md").write_text("soyez bref", encoding="utf-8")
    result = subprocess.run(
        [sys.executable, str(SCRIPT), str(tmp_path)],
        capture_output=True, text=True,
    )
    assert result.returncode == 0, result.stdout
    assert "WARN" in result.stdout
    assert "fr" in result.stdout


def test_stray_file_is_warning(tmp_path: Path) -> None:
    _make_valid_recipe(tmp_path)
    (tmp_path / ".lingtai-recipe" / "random.txt").write_text("x", encoding="utf-8")
    result = subprocess.run(
        [sys.executable, str(SCRIPT), str(tmp_path)],
        capture_output=True, text=True,
    )
    assert result.returncode == 0
    assert "WARN" in result.stdout
    assert "random.txt" in result.stdout


def test_skill_missing_frontmatter_is_error(tmp_path: Path) -> None:
    _make_valid_recipe(tmp_path)
    skill_dir = tmp_path / ".lingtai-recipe" / "skills" / "broken"
    skill_dir.mkdir(parents=True)
    (skill_dir / "SKILL.md").write_text("no frontmatter here", encoding="utf-8")
    result = subprocess.run(
        [sys.executable, str(SCRIPT), str(tmp_path)],
        capture_output=True, text=True,
    )
    assert result.returncode == 1
    assert "frontmatter" in result.stdout


def test_skill_frontmatter_missing_version(tmp_path: Path) -> None:
    _make_valid_recipe(tmp_path)
    skill_dir = tmp_path / ".lingtai-recipe" / "skills" / "halfbaked"
    skill_dir.mkdir(parents=True)
    (skill_dir / "SKILL.md").write_text(
        "---\nname: halfbaked\ndescription: missing version field\n---\n\nbody",
        encoding="utf-8",
    )
    result = subprocess.run(
        [sys.executable, str(SCRIPT), str(tmp_path)],
        capture_output=True, text=True,
    )
    assert result.returncode == 1
    assert "version" in result.stdout


def test_skill_missing_skill_md(tmp_path: Path) -> None:
    _make_valid_recipe(tmp_path)
    skill_dir = tmp_path / ".lingtai-recipe" / "skills" / "empty"
    skill_dir.mkdir(parents=True)
    result = subprocess.run(
        [sys.executable, str(SCRIPT), str(tmp_path)],
        capture_output=True, text=True,
    )
    assert result.returncode == 1
    assert "SKILL.md" in result.stdout


def test_system_prefix_in_greet_is_warning(tmp_path: Path) -> None:
    _make_valid_recipe(tmp_path)
    (tmp_path / ".lingtai-recipe" / "en" / "greet.md").write_text(
        "[system] do not say this", encoding="utf-8"
    )
    result = subprocess.run(
        [sys.executable, str(SCRIPT), str(tmp_path)],
        capture_output=True, text=True,
    )
    assert result.returncode == 0
    assert "WARN" in result.stdout
    assert "[system]" in result.stdout


def test_repo_root_not_a_directory(tmp_path: Path) -> None:
    missing = tmp_path / "does-not-exist"
    result = subprocess.run(
        [sys.executable, str(SCRIPT), str(missing)],
        capture_output=True, text=True,
    )
    assert result.returncode == 1
    assert "not a directory" in result.stdout
