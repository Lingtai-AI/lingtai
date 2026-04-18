#!/usr/bin/env python3
"""
validate_recipe.py — Sanity-check a `.lingtai-recipe/` payload.

Both `/export recipe` and `/export network` invoke this script on their
staging directory before `git init`. Exits 0 if the payload is structurally
valid (warnings allowed); exits 1 if any error is found.

The canonical recipe format is documented in:
    tui/internal/preset/skills/lingtai-recipe/SKILL-en.md

Usage:
    validate_recipe.py <repo-root>

where <repo-root> contains both `recipe.json` and `.lingtai-recipe/`.
"""

import argparse
import json
import re
import sys
from pathlib import Path

KNOWN_LANGS = {"en", "zh", "wen"}
FORBIDDEN_PLACEHOLDERS = (
    "{{time}}",
    "{{addr}}",
    "{{lang}}",
    "{{location}}",
    "{{soul_delay}}",
)
RECIPE_ROOT_DIRNAME = ".lingtai-recipe"


def validate(repo_root: Path) -> tuple[list[str], list[str]]:
    """Return (errors, warnings) for the recipe payload at `repo_root`."""
    errors: list[str] = []
    warnings: list[str] = []

    if not repo_root.is_dir():
        errors.append(f"{repo_root}: not a directory")
        return errors, warnings

    _check_recipe_json(repo_root, errors)
    recipe_dir = repo_root / RECIPE_ROOT_DIRNAME
    if not recipe_dir.is_dir():
        errors.append(f"{recipe_dir}: directory missing")
        return errors, warnings

    _check_required_components(recipe_dir, errors)
    _check_lang_dirs(recipe_dir, warnings)
    _check_skills(recipe_dir, errors)
    _check_forbidden_placeholders(recipe_dir, errors)
    _check_greet_system_prefix(recipe_dir, warnings)
    _check_stray_files(recipe_dir, warnings)

    return errors, warnings


def _check_recipe_json(repo_root: Path, errors: list[str]) -> None:
    path = repo_root / "recipe.json"
    if not path.is_file():
        errors.append(f"{path}: missing (required at repo root)")
        return
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as e:
        errors.append(f"{path}: invalid JSON ({e})")
        return
    if not isinstance(data, dict):
        errors.append(f"{path}: must be a JSON object")
        return
    for field in ("name", "description"):
        value = data.get(field)
        if not isinstance(value, str) or not value.strip():
            errors.append(f"{path}: `{field}` must be a non-empty string")


def _has_component(recipe_dir: Path, name: str) -> bool:
    """True if `name` exists at root or under any lang subdir."""
    if (recipe_dir / name).is_file():
        return True
    for sub in recipe_dir.iterdir():
        if sub.is_dir() and (sub / name).is_file():
            return True
    return False


def _check_required_components(recipe_dir: Path, errors: list[str]) -> None:
    for name in ("greet.md", "comment.md"):
        if not _has_component(recipe_dir, name):
            errors.append(
                f"{recipe_dir}/{name}: missing (required at root or in a lang subdir)"
            )


def _check_lang_dirs(recipe_dir: Path, warnings: list[str]) -> None:
    for sub in recipe_dir.iterdir():
        if not sub.is_dir():
            continue
        if sub.name == "skills":
            continue
        if sub.name not in KNOWN_LANGS:
            warnings.append(
                f"{sub}: unknown lang code `{sub.name}` (known: {sorted(KNOWN_LANGS)})"
            )


def _check_skills(recipe_dir: Path, errors: list[str]) -> None:
    skills_dir = recipe_dir / "skills"
    if not skills_dir.is_dir():
        return
    for skill in skills_dir.iterdir():
        if not skill.is_dir():
            continue
        skill_md = skill / "SKILL.md"
        if not skill_md.is_file():
            errors.append(f"{skill}: missing SKILL.md")
            continue
        _check_skill_frontmatter(skill_md, errors)


def _check_skill_frontmatter(skill_md: Path, errors: list[str]) -> None:
    text = skill_md.read_text(encoding="utf-8")
    match = re.match(r"^---\n(.*?)\n---\n", text, re.DOTALL)
    if not match:
        errors.append(f"{skill_md}: missing YAML frontmatter (--- ... ---)")
        return
    body = match.group(1)
    for field in ("name", "description", "version"):
        if not re.search(rf"^{field}\s*:", body, re.MULTILINE):
            errors.append(f"{skill_md}: frontmatter missing `{field}`")


def _check_forbidden_placeholders(recipe_dir: Path, errors: list[str]) -> None:
    targets = ["comment.md", "covenant.md", "procedures.md"]
    for target in targets:
        for path in _all_component_paths(recipe_dir, target):
            text = path.read_text(encoding="utf-8")
            for placeholder in FORBIDDEN_PLACEHOLDERS:
                if placeholder in text:
                    errors.append(
                        f"{path}: contains forbidden placeholder `{placeholder}` "
                        "(only greet.md may use placeholders)"
                    )


def _all_component_paths(recipe_dir: Path, name: str) -> list[Path]:
    found: list[Path] = []
    root_file = recipe_dir / name
    if root_file.is_file():
        found.append(root_file)
    for sub in recipe_dir.iterdir():
        if sub.is_dir() and sub.name != "skills":
            candidate = sub / name
            if candidate.is_file():
                found.append(candidate)
    return found


def _check_greet_system_prefix(recipe_dir: Path, warnings: list[str]) -> None:
    for path in _all_component_paths(recipe_dir, "greet.md"):
        text = path.read_text(encoding="utf-8").lstrip()
        if text.startswith("[system]"):
            warnings.append(
                f"{path}: starts with `[system]` prefix — greet.md is the "
                "orchestrator's voice, not a system message"
            )


def _check_stray_files(recipe_dir: Path, warnings: list[str]) -> None:
    recognized_root_files = {"covenant.md", "procedures.md"}
    recognized_root_dirs = {"skills"} | KNOWN_LANGS
    for entry in recipe_dir.iterdir():
        if entry.is_file() and entry.name not in recognized_root_files:
            warnings.append(
                f"{entry}: unexpected file at .lingtai-recipe/ root "
                "(only covenant.md, procedures.md recognized here)"
            )
        elif entry.is_dir() and entry.name not in recognized_root_dirs:
            warnings.append(
                f"{entry}: unexpected directory at .lingtai-recipe/ root"
            )


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("repo_root", type=Path, help="Path to the repo root (contains recipe.json and .lingtai-recipe/)")
    args = parser.parse_args()

    errors, warnings = validate(args.repo_root.resolve())

    for w in warnings:
        print(f"WARN:  {w}")
    for e in errors:
        print(f"ERROR: {e}")

    print(f"\n{len(errors)} error(s), {len(warnings)} warning(s)")
    if errors:
        return 1
    if warnings:
        print("OK (with warnings)")
    else:
        print("OK")
    return 0


if __name__ == "__main__":
    sys.exit(main())
