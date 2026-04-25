"""Tests for validate_recipe.py — the canonical recipe-bundle validator."""
import json
import subprocess
import sys
from pathlib import Path

import pytest

SCRIPT = Path(__file__).parent / "validate_recipe.py"


# --- helpers ---------------------------------------------------------------


def _make_valid_bundle(root: Path, *, library_name: str | None = None) -> None:
    """Populate `root` with the minimum valid bundle payload.

    Minimum = .recipe/recipe.json + .recipe/greet/greet.md. All other
    behavioral layers are optional.
    """
    recipe_dir = root / ".recipe"
    greet_dir = recipe_dir / "greet"
    greet_dir.mkdir(parents=True)
    manifest = {
        "id": "test-recipe",
        "name": "Test Recipe",
        "description": "A test recipe",
        "version": "1.0.0",
        "library_name": library_name,
    }
    (recipe_dir / "recipe.json").write_text(
        json.dumps(manifest), encoding="utf-8"
    )
    (greet_dir / "greet.md").write_text("Hello from the test recipe.", encoding="utf-8")
    # Create library sibling when declared. Canonical layout places each
    # skill in its own subdirectory: <library>/<skill>/SKILL.md. A SKILL.md
    # at the library root is ignored by the runtime scanner.
    if library_name:
        lib = root / library_name
        skill_dir = lib / "sample-skill"
        skill_dir.mkdir(parents=True)
        (skill_dir / "SKILL.md").write_text(
            "---\nname: sample-skill\ndescription: d\nversion: 1.0.0\n---\n",
            encoding="utf-8",
        )


def _run(root: Path) -> subprocess.CompletedProcess:
    return subprocess.run(
        [sys.executable, str(SCRIPT), str(root)],
        capture_output=True,
        text=True,
    )


def _assert_error(result, *needles: str) -> None:
    assert result.returncode == 1, result.stdout + result.stderr
    for n in needles:
        assert n in result.stdout, f"expected {n!r} in:\n{result.stdout}"


def _assert_ok(result) -> None:
    assert result.returncode == 0, result.stdout + result.stderr


# --- happy-path tests ------------------------------------------------------


def test_minimum_valid_bundle_passes(tmp_path: Path) -> None:
    _make_valid_bundle(tmp_path)
    _assert_ok(_run(tmp_path))


def test_valid_bundle_with_library_passes(tmp_path: Path) -> None:
    _make_valid_bundle(tmp_path, library_name="my-lib")
    _assert_ok(_run(tmp_path))


def test_bundle_without_any_behavioral_layer_passes(tmp_path: Path) -> None:
    """All four layers are optional — recipe.json alone is enough."""
    recipe_dir = tmp_path / ".recipe"
    recipe_dir.mkdir()
    (recipe_dir / "recipe.json").write_text(
        json.dumps({"id": "t", "name": "T", "description": "d"}),
        encoding="utf-8",
    )
    _assert_ok(_run(tmp_path))


def test_all_four_layers_populated_passes(tmp_path: Path) -> None:
    _make_valid_bundle(tmp_path)
    for layer in ("comment", "covenant", "procedures"):
        (tmp_path / ".recipe" / layer).mkdir()
        (tmp_path / ".recipe" / layer / f"{layer}.md").write_text(
            f"static {layer} content", encoding="utf-8"
        )
    _assert_ok(_run(tmp_path))


def test_locale_variants_pass(tmp_path: Path) -> None:
    _make_valid_bundle(tmp_path)
    # locale recipe.json at .recipe/zh/recipe.json
    (tmp_path / ".recipe" / "zh").mkdir()
    (tmp_path / ".recipe" / "zh" / "recipe.json").write_text(
        json.dumps({"name": "测试", "description": "测试配方"}),
        encoding="utf-8",
    )
    # locale greet at .recipe/greet/zh/greet.md
    (tmp_path / ".recipe" / "greet" / "zh").mkdir()
    (tmp_path / ".recipe" / "greet" / "zh" / "greet.md").write_text(
        "你好。", encoding="utf-8"
    )
    _assert_ok(_run(tmp_path))


# --- recipe.json errors ----------------------------------------------------


def test_missing_recipe_dot_dir(tmp_path: Path) -> None:
    _assert_error(_run(tmp_path), ".recipe", "missing")


def test_missing_recipe_json(tmp_path: Path) -> None:
    _make_valid_bundle(tmp_path)
    (tmp_path / ".recipe" / "recipe.json").unlink()
    _assert_error(_run(tmp_path), "recipe.json", "missing")


def test_recipe_json_invalid_json(tmp_path: Path) -> None:
    _make_valid_bundle(tmp_path)
    (tmp_path / ".recipe" / "recipe.json").write_text("{not valid", encoding="utf-8")
    _assert_error(_run(tmp_path), "invalid JSON")


def test_recipe_json_not_object(tmp_path: Path) -> None:
    _make_valid_bundle(tmp_path)
    (tmp_path / ".recipe" / "recipe.json").write_text("[1,2,3]", encoding="utf-8")
    _assert_error(_run(tmp_path), "JSON object")


def test_recipe_json_missing_id(tmp_path: Path) -> None:
    _make_valid_bundle(tmp_path)
    (tmp_path / ".recipe" / "recipe.json").write_text(
        json.dumps({"name": "N", "description": "D"}), encoding="utf-8"
    )
    _assert_error(_run(tmp_path), "`id`", "non-empty")


def test_recipe_json_missing_name(tmp_path: Path) -> None:
    _make_valid_bundle(tmp_path)
    (tmp_path / ".recipe" / "recipe.json").write_text(
        json.dumps({"id": "t", "description": "D"}), encoding="utf-8"
    )
    _assert_error(_run(tmp_path), "`name`", "non-empty")


def test_recipe_json_empty_description(tmp_path: Path) -> None:
    _make_valid_bundle(tmp_path)
    (tmp_path / ".recipe" / "recipe.json").write_text(
        json.dumps({"id": "t", "name": "N", "description": "   "}),
        encoding="utf-8",
    )
    _assert_error(_run(tmp_path), "`description`", "non-empty")


def test_recipe_json_library_name_with_slash_rejected(tmp_path: Path) -> None:
    _make_valid_bundle(tmp_path)
    (tmp_path / ".recipe" / "recipe.json").write_text(
        json.dumps(
            {"id": "t", "name": "N", "description": "d", "library_name": "sub/dir"}
        ),
        encoding="utf-8",
    )
    _assert_error(_run(tmp_path), "simple folder name, not a path")


def test_recipe_json_library_name_null_ok(tmp_path: Path) -> None:
    _make_valid_bundle(tmp_path)
    # explicit null library_name is fine
    (tmp_path / ".recipe" / "recipe.json").write_text(
        json.dumps(
            {"id": "t", "name": "N", "description": "d", "library_name": None}
        ),
        encoding="utf-8",
    )
    _assert_ok(_run(tmp_path))


def test_recipe_json_version_invalid_rejected(tmp_path: Path) -> None:
    _make_valid_bundle(tmp_path)
    (tmp_path / ".recipe" / "recipe.json").write_text(
        json.dumps(
            {"id": "t", "name": "N", "description": "d", "version": ""}
        ),
        encoding="utf-8",
    )
    _assert_error(_run(tmp_path), "`version`")


# --- library sibling errors ------------------------------------------------


def test_library_sibling_missing_when_declared(tmp_path: Path) -> None:
    """library_name non-null but folder absent → error."""
    recipe_dir = tmp_path / ".recipe"
    (recipe_dir / "greet").mkdir(parents=True)
    (recipe_dir / "recipe.json").write_text(
        json.dumps(
            {
                "id": "t",
                "name": "N",
                "description": "d",
                "library_name": "ghost-lib",
            }
        ),
        encoding="utf-8",
    )
    (recipe_dir / "greet" / "greet.md").write_text("hi", encoding="utf-8")
    # NOTE: tmp_path / "ghost-lib" deliberately not created
    _assert_error(_run(tmp_path), "ghost-lib", "missing")


def test_library_sibling_empty_warns(tmp_path: Path) -> None:
    _make_valid_bundle(tmp_path, library_name="my-lib")
    # Remove the sample skill so the library is empty.
    import shutil
    shutil.rmtree(tmp_path / "my-lib" / "sample-skill")
    result = _run(tmp_path)
    assert result.returncode == 0, result.stdout  # warnings don't fail
    assert "no SKILL.md" in result.stdout


# --- behavioral-layer errors -----------------------------------------------


def test_empty_layer_dir_errors(tmp_path: Path) -> None:
    """greet/ dir exists but contains neither greet.md nor any lang variant."""
    _make_valid_bundle(tmp_path)
    # blow away greet.md, leaving the dir empty
    (tmp_path / ".recipe" / "greet" / "greet.md").unlink()
    _assert_error(_run(tmp_path), "greet.md", "neither")


def test_placeholder_in_comment_rejected(tmp_path: Path) -> None:
    _make_valid_bundle(tmp_path)
    (tmp_path / ".recipe" / "comment").mkdir()
    (tmp_path / ".recipe" / "comment" / "comment.md").write_text(
        "time is {{time}}", encoding="utf-8"
    )
    _assert_error(_run(tmp_path), "forbidden placeholder", "{{time}}")


def test_placeholder_in_covenant_rejected(tmp_path: Path) -> None:
    _make_valid_bundle(tmp_path)
    (tmp_path / ".recipe" / "covenant").mkdir()
    (tmp_path / ".recipe" / "covenant" / "covenant.md").write_text(
        "you are {{addr}}", encoding="utf-8"
    )
    _assert_error(_run(tmp_path), "forbidden placeholder", "{{addr}}")


def test_placeholder_in_greet_ok(tmp_path: Path) -> None:
    _make_valid_bundle(tmp_path)
    (tmp_path / ".recipe" / "greet" / "greet.md").write_text(
        "welcome, it is {{time}} at {{location}}. commands: {{commands}}",
        encoding="utf-8",
    )
    _assert_ok(_run(tmp_path))


def test_greet_system_prefix_warns(tmp_path: Path) -> None:
    _make_valid_bundle(tmp_path)
    (tmp_path / ".recipe" / "greet" / "greet.md").write_text(
        "[system] this is a system message",
        encoding="utf-8",
    )
    result = _run(tmp_path)
    assert result.returncode == 0  # warnings don't fail
    assert "[system]" in result.stdout or "system" in result.stdout


# --- stray-file warnings ---------------------------------------------------


def test_unknown_lang_subdir_warns(tmp_path: Path) -> None:
    _make_valid_bundle(tmp_path)
    # stray lang code at .recipe/fr/recipe.json
    fr = tmp_path / ".recipe" / "fr"
    fr.mkdir()
    (fr / "recipe.json").write_text(
        json.dumps({"name": "Bonjour", "description": "fr"}), encoding="utf-8"
    )
    result = _run(tmp_path)
    assert result.returncode == 0
    assert "fr" in result.stdout and "unknown lang" in result.stdout


def test_unknown_greet_locale_warns(tmp_path: Path) -> None:
    _make_valid_bundle(tmp_path)
    fr_greet = tmp_path / ".recipe" / "greet" / "fr"
    fr_greet.mkdir()
    (fr_greet / "greet.md").write_text("bonjour", encoding="utf-8")
    result = _run(tmp_path)
    assert result.returncode == 0
    assert "unknown lang" in result.stdout


def test_stray_file_at_recipe_root_warns(tmp_path: Path) -> None:
    _make_valid_bundle(tmp_path)
    (tmp_path / ".recipe" / "README.md").write_text("stray", encoding="utf-8")
    result = _run(tmp_path)
    assert result.returncode == 0
    assert "unexpected file" in result.stdout


# --- network snapshot ------------------------------------------------------


def test_network_snapshot_with_stripped_init_passes(tmp_path: Path) -> None:
    _make_valid_bundle(tmp_path)
    agent = tmp_path / ".lingtai" / "alpha"
    agent.mkdir(parents=True)
    (agent / ".agent.json").write_text("{}", encoding="utf-8")
    # No init.json — correct post-strip shape.
    _assert_ok(_run(tmp_path))


def test_network_snapshot_with_init_present_errors(tmp_path: Path) -> None:
    _make_valid_bundle(tmp_path)
    agent = tmp_path / ".lingtai" / "alpha"
    agent.mkdir(parents=True)
    (agent / ".agent.json").write_text("{}", encoding="utf-8")
    # init.json should have been stripped.
    (agent / "init.json").write_text(
        json.dumps({"manifest": {}}), encoding="utf-8"
    )
    _assert_error(_run(tmp_path), "init.json", "stripped")


def test_network_snapshot_skips_human_dir(tmp_path: Path) -> None:
    """human/ pseudo-agent is not checked."""
    _make_valid_bundle(tmp_path)
    human = tmp_path / ".lingtai" / "human"
    human.mkdir(parents=True)
    (human / "init.json").write_text("{}", encoding="utf-8")
    _assert_ok(_run(tmp_path))


def test_network_snapshot_skips_dir_without_blueprint(tmp_path: Path) -> None:
    """A dir without .agent.json is not treated as an agent."""
    _make_valid_bundle(tmp_path)
    notagent = tmp_path / ".lingtai" / ".tui-asset"
    notagent.mkdir(parents=True)
    (notagent / "init.json").write_text("{}", encoding="utf-8")
    _assert_ok(_run(tmp_path))


# --- library layout tests --------------------------------------------------


def test_flat_library_layout_errors(tmp_path: Path) -> None:
    """Library with SKILL.md at its root but no skill subdirs is rejected.

    Runtime scanner only registers <library>/<skill>/SKILL.md. A flat
    layout silently produces zero skills, so the validator must catch it.
    """
    recipe_dir = tmp_path / ".recipe"
    recipe_dir.mkdir()
    manifest = {
        "id": "test-recipe",
        "name": "Test Recipe",
        "description": "A test recipe",
        "version": "1.0.0",
        "library_name": "flat-lib",
    }
    (recipe_dir / "recipe.json").write_text(json.dumps(manifest), encoding="utf-8")
    # Flat layout: SKILL.md at library root, no skill subdirs.
    lib = tmp_path / "flat-lib"
    lib.mkdir()
    (lib / "SKILL.md").write_text(
        "---\nname: flat-lib\ndescription: d\nversion: 1.0.0\n---\n",
        encoding="utf-8",
    )
    (lib / "extra.md").write_text("stray file", encoding="utf-8")
    _assert_error(_run(tmp_path), "root", "subdirectories")


def test_nested_library_with_root_skill_errors(tmp_path: Path) -> None:
    """Library with BOTH root SKILL.md AND skill subdirs is rejected.

    Strict layout: a root-level SKILL.md is never permitted. Even though the
    subdirs would register at runtime, the root SKILL.md creates ambiguity
    and is silently ignored — the validator surfaces this as an error.
    """
    recipe_dir = tmp_path / ".recipe"
    recipe_dir.mkdir()
    manifest = {
        "id": "test-recipe",
        "name": "Test Recipe",
        "description": "A test recipe",
        "version": "1.0.0",
        "library_name": "mixed-lib",
    }
    (recipe_dir / "recipe.json").write_text(json.dumps(manifest), encoding="utf-8")
    lib = tmp_path / "mixed-lib"
    (lib / "real-skill").mkdir(parents=True)
    (lib / "real-skill" / "SKILL.md").write_text(
        "---\nname: real-skill\ndescription: d\nversion: 1.0.0\n---\n",
        encoding="utf-8",
    )
    # Stray root SKILL.md is now an error, not a warning.
    (lib / "SKILL.md").write_text(
        "---\nname: stray\ndescription: d\nversion: 1.0.0\n---\n",
        encoding="utf-8",
    )
    _assert_error(_run(tmp_path), "root-level SKILL.md is not permitted")


def test_single_skill_nested_layout_passes(tmp_path: Path) -> None:
    """The canonical single-skill nested layout <library>/<library>/SKILL.md passes."""
    recipe_dir = tmp_path / ".recipe"
    recipe_dir.mkdir()
    manifest = {
        "id": "test-recipe",
        "name": "Test Recipe",
        "description": "A test recipe",
        "version": "1.0.0",
        "library_name": "my-skill",
    }
    (recipe_dir / "recipe.json").write_text(json.dumps(manifest), encoding="utf-8")
    skill = tmp_path / "my-skill" / "my-skill"
    skill.mkdir(parents=True)
    (skill / "SKILL.md").write_text(
        "---\nname: my-skill\ndescription: d\nversion: 1.0.0\n---\n",
        encoding="utf-8",
    )
    _assert_ok(_run(tmp_path))
