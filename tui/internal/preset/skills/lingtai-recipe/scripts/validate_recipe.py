#!/usr/bin/env python3
"""
validate_recipe.py — Sanity-check a recipe bundle.

Both `/export recipe` and `/export network` invoke this script on their
staging directory before `git init`. Exits 0 if the bundle is structurally
valid (warnings allowed); exits 1 if any error is found.

The canonical recipe format is documented in:
    tui/internal/preset/skills/lingtai-recipe/references/recipe-format.md

A recipe **bundle** is a directory containing:

    <bundle-root>/
    ├── .recipe/                         (required — behavioral layer)
    │   ├── recipe.json                  (required manifest — SINGLE CANONICAL,
    │   │                                 NEVER localized; carries machine
    │   │                                 identity: id/version/library_name)
    │   ├── greet/greet.md               (optional — agent is silent if absent)
    │   ├── greet/{en,zh,wen}/greet.md   (optional locale variants)
    │   ├── comment/...                  (optional — no comment if absent)
    │   ├── covenant/...                 (optional — kernel default if absent)
    │   └── procedures/...               (optional — kernel default if absent)
    ├── <library_name>/                  (optional — shared skill library)
    │   └── (skills with SKILL.md files)
    └── .lingtai/                        (optional — full network snapshot)
        ├── <agent>/.agent.json          (identity blueprint)
        └── <agent>/                     (NO init.json — stripped on export)

Usage:
    validate_recipe.py <bundle-root>
"""

import argparse
import json
import re
import sys
from pathlib import Path

KNOWN_LANGS = {"en", "zh", "wen"}
GREET_PLACEHOLDERS = (
    "{{time}}",
    "{{addr}}",
    "{{lang}}",
    "{{location}}",
    "{{soul_delay}}",
    "{{commands}}",
)
# greet.md may contain placeholders; other layers must be static.
FORBIDDEN_LAYER_PLACEHOLDERS = {
    "comment": GREET_PLACEHOLDERS,
    "covenant": GREET_PLACEHOLDERS,
    "procedures": GREET_PLACEHOLDERS,
}
RECIPE_DOT_DIR = ".recipe"
BEHAVIORAL_LAYERS = ("greet", "comment", "covenant", "procedures")
REQUIRED_RECIPE_JSON_FIELDS = ("id", "name", "description")


def validate(bundle_root: Path) -> tuple[list[str], list[str]]:
    """Return (errors, warnings) for the bundle at `bundle_root`."""
    errors: list[str] = []
    warnings: list[str] = []

    if not bundle_root.is_dir():
        errors.append(f"{bundle_root}: not a directory")
        return errors, warnings

    recipe_dir = bundle_root / RECIPE_DOT_DIR
    if not recipe_dir.is_dir():
        errors.append(f"{recipe_dir}: directory missing (required at bundle root)")
        return errors, warnings

    manifest = _check_recipe_json(recipe_dir, errors, warnings)
    _check_locale_recipe_jsons(recipe_dir, errors, warnings)
    _check_behavioral_layers(recipe_dir, errors, warnings)
    _check_no_placeholders_in_static_layers(recipe_dir, errors)
    _check_greet_system_prefix(recipe_dir, warnings)
    _check_stray_files(recipe_dir, warnings)

    if manifest is not None:
        _check_library_sibling(bundle_root, manifest, errors, warnings)

    _check_network_snapshot(bundle_root, errors, warnings)

    return errors, warnings


def _check_recipe_json(
    recipe_dir: Path, errors: list[str], warnings: list[str]
) -> dict | None:
    """Validate <recipe_dir>/recipe.json and return its parsed content."""
    path = recipe_dir / "recipe.json"
    if not path.is_file():
        errors.append(f"{path}: missing (required at .recipe/ root)")
        return None
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as e:
        errors.append(f"{path}: invalid JSON ({e})")
        return None
    if not isinstance(data, dict):
        errors.append(f"{path}: must be a JSON object")
        return None

    for field in REQUIRED_RECIPE_JSON_FIELDS:
        value = data.get(field)
        if not isinstance(value, str) or not value.strip():
            errors.append(f"{path}: `{field}` must be a non-empty string")

    # version is optional; default "1.0.0" applied on load.
    version = data.get("version")
    if version is not None and (not isinstance(version, str) or not version.strip()):
        errors.append(f"{path}: `version` must be a non-empty string if present")

    # library_name must be either JSON null or a non-empty string.
    lib = data.get("library_name", None)
    if lib is not None and (not isinstance(lib, str) or not lib.strip()):
        errors.append(
            f"{path}: `library_name` must be null or a non-empty string"
        )
    if isinstance(lib, str) and "/" in lib:
        errors.append(
            f"{path}: `library_name` = {lib!r} must be a simple folder name, not a path"
        )

    return data


def _check_locale_recipe_jsons(
    recipe_dir: Path, errors: list[str], warnings: list[str]
) -> None:
    """Reject any locale-variant recipe.json.

    recipe.json carries machine identity (id, version, library_name) and
    must be a single canonical file at .recipe/recipe.json — never localized.
    Locale variants silently drop critical fields like library_name in the
    non-default locale, breaking recipe-apply with no visible error. If you
    want localized display strings, put them only in greet.md / comment.md /
    covenant.md / procedures.md — those are the four behavioral layers that
    legitimately have locale variants.
    """
    for sub in sorted(recipe_dir.iterdir()):
        if not sub.is_dir():
            continue
        if sub.name in BEHAVIORAL_LAYERS:
            continue  # behavioral-layer subdir, not a locale folder
        loc_json = sub / "recipe.json"
        if loc_json.is_file():
            errors.append(
                f"{loc_json}: locale-variant recipe.json is forbidden — "
                f"recipe.json must be a single canonical file at .recipe/recipe.json. "
                f"Move localized name/description into greet/comment if needed, "
                f"and delete this file."
            )


def _check_behavioral_layers(
    recipe_dir: Path, errors: list[str], warnings: list[str]
) -> None:
    """Each present behavioral-layer dir must have the canonical file shape."""
    for layer in BEHAVIORAL_LAYERS:
        layer_dir = recipe_dir / layer
        if not layer_dir.is_dir():
            continue  # layer absent — OK, all layers are optional
        filename = f"{layer}.md"
        root_file = layer_dir / filename
        has_root = root_file.is_file()
        has_any_locale = False
        for sub in layer_dir.iterdir():
            if not sub.is_dir():
                continue
            if sub.name not in KNOWN_LANGS:
                warnings.append(
                    f"{sub}: unknown lang code `{sub.name}` "
                    f"(known: {sorted(KNOWN_LANGS)})"
                )
            if (sub / filename).is_file():
                has_any_locale = True
        if not has_root and not has_any_locale:
            errors.append(
                f"{layer_dir}: contains neither {filename} nor any "
                f"<lang>/{filename} — remove the empty dir or add content"
            )


def _check_no_placeholders_in_static_layers(
    recipe_dir: Path, errors: list[str]
) -> None:
    """comment/covenant/procedures are static; no {{placeholders}} allowed."""
    for layer, placeholders in FORBIDDEN_LAYER_PLACEHOLDERS.items():
        filename = f"{layer}.md"
        for path in _all_layer_files(recipe_dir, layer, filename):
            text = path.read_text(encoding="utf-8")
            for placeholder in placeholders:
                if placeholder in text:
                    errors.append(
                        f"{path}: contains forbidden placeholder "
                        f"`{placeholder}` (only greet.md may use placeholders)"
                    )


def _all_layer_files(recipe_dir: Path, layer: str, filename: str) -> list[Path]:
    """Return all existing layer files: root + locale variants."""
    layer_dir = recipe_dir / layer
    if not layer_dir.is_dir():
        return []
    found: list[Path] = []
    root_file = layer_dir / filename
    if root_file.is_file():
        found.append(root_file)
    for sub in layer_dir.iterdir():
        if sub.is_dir() and (sub / filename).is_file():
            found.append(sub / filename)
    return found


def _check_greet_system_prefix(recipe_dir: Path, warnings: list[str]) -> None:
    """greet.md starting with [system] is suspicious — it's first-contact prose."""
    for path in _all_layer_files(recipe_dir, "greet", "greet.md"):
        text = path.read_text(encoding="utf-8").lstrip()
        if text.startswith("[system]"):
            warnings.append(
                f"{path}: starts with `[system]` prefix — greet.md is the "
                "orchestrator's voice to the human, not a system message"
            )


def _check_stray_files(recipe_dir: Path, warnings: list[str]) -> None:
    """Warn on anything unrecognized at .recipe/ root."""
    recognized_files = {"recipe.json"}
    recognized_dirs = set(BEHAVIORAL_LAYERS) | KNOWN_LANGS
    for entry in recipe_dir.iterdir():
        if entry.is_file() and entry.name not in recognized_files:
            warnings.append(
                f"{entry}: unexpected file at .recipe/ root "
                f"(only recipe.json recognized here)"
            )
        elif entry.is_dir() and entry.name not in recognized_dirs:
            warnings.append(
                f"{entry}: unexpected directory at .recipe/ root "
                f"(known: {sorted(recognized_dirs)})"
            )


def _check_library_sibling(
    bundle_root: Path,
    manifest: dict,
    errors: list[str],
    warnings: list[str],
) -> None:
    """If recipe.json declares library_name, validate the sibling library folder.

    Enforces the canonical layout ``<lib>/<skill>/SKILL.md``. The scanner in
    ``lingtai.core.library`` only registers direct-child subdirectories of the
    library folder as skills — a ``SKILL.md`` at the library root is ignored.
    This validator catches the common mistake of flattening a single-skill
    library to ``<lib>/SKILL.md`` + sibling content files, which silently
    results in zero registered skills at runtime.
    """
    lib_name = manifest.get("library_name")
    if not isinstance(lib_name, str) or not lib_name.strip():
        return  # null or missing — no library, nothing to validate
    lib_dir = bundle_root / lib_name
    if not lib_dir.is_dir():
        errors.append(
            f"{lib_dir}: directory missing "
            f"(recipe.json declares `library_name` = {lib_name!r})"
        )
        return

    # Must contain at least one SKILL.md somewhere — empty library is
    # probably a mistake.
    if not any(lib_dir.rglob("SKILL.md")):
        warnings.append(
            f"{lib_dir}: contains no SKILL.md files — is this library populated?"
        )
        return

    # Detect the flat-layout mistake: SKILL.md sits at the library root
    # instead of in a skill subdirectory. Runtime scanner will not register
    # this as a skill.
    root_skill = lib_dir / "SKILL.md"
    has_root_skill = root_skill.is_file()

    # Also collect valid skill subdirs (direct children that contain SKILL.md).
    skill_subdirs = [
        child for child in lib_dir.iterdir()
        if child.is_dir()
        and not child.name.startswith(".")
        and (child / "SKILL.md").is_file()
    ]

    if has_root_skill and not skill_subdirs:
        errors.append(
            f"{lib_dir}/SKILL.md: library has SKILL.md at its root but no skill "
            f"subdirectories. The runtime scanner only registers "
            f"<library>/<skill>/SKILL.md — a root-level SKILL.md is ignored. "
            f"Wrap the skill files into a subdirectory: "
            f"mkdir {lib_dir}/{lib_name} && mv {lib_dir}/* {lib_dir}/{lib_name}/"
        )
        return

    # Strict layout: a root-level SKILL.md is never permitted, even when
    # valid skill subdirs also exist. The scanner ignores it and its
    # presence creates ambiguity for human readers.
    if has_root_skill and skill_subdirs:
        errors.append(
            f"{lib_dir}/SKILL.md: root-level SKILL.md is not permitted. "
            f"The runtime scanner ignores it (only <library>/<skill>/SKILL.md is "
            f"registered) and its presence is ambiguous. Remove the root SKILL.md "
            f"or move its content into a dedicated skill subdirectory."
        )

    if not skill_subdirs:
        errors.append(
            f"{lib_dir}: no skill subdirectories with SKILL.md found. "
            f"The required layout is <library>/<skill-name>/SKILL.md; content "
            f"placed elsewhere is not registered at runtime."
        )


def _check_network_snapshot(
    bundle_root: Path, errors: list[str], warnings: list[str]
) -> None:
    """For bundles shipping .lingtai/, confirm init.json is stripped per agent."""
    lingtai = bundle_root / ".lingtai"
    if not lingtai.is_dir():
        return  # recipe-only bundle, no network
    for agent_dir in sorted(lingtai.iterdir()):
        if not agent_dir.is_dir():
            continue
        name = agent_dir.name
        if not name or name.startswith(".") or name == "human":
            continue
        blueprint = agent_dir / ".agent.json"
        if not blueprint.is_file():
            continue  # not an agent dir
        init_json = agent_dir / "init.json"
        if init_json.is_file():
            errors.append(
                f"{init_json}: present in exported network — must be stripped "
                "so the recipient picks their own LLM provider and capabilities"
            )


def main() -> int:
    parser = argparse.ArgumentParser(
        description=__doc__,
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument(
        "bundle_root",
        type=Path,
        help="Path to the bundle root (contains .recipe/ and optionally "
        "<library_name>/ and .lingtai/)",
    )
    args = parser.parse_args()

    errors, warnings = validate(args.bundle_root.resolve())

    for e in errors:
        print(f"ERROR: {e}")
    for w in warnings:
        print(f"WARN:  {w}")

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
