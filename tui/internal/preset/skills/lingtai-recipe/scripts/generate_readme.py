#!/usr/bin/env python3
"""
generate_readme.py — Draft README.md for an exported network bundle.

Reads:
    .recipe/recipe.json          (name, description, library_name)
    .exported-from               (origin, when — written by mark_export_source.py)
    .lingtai/<agent>/.agent.json (per-agent identity + capabilities)
    <library>/<skill>/SKILL.md   (skill names + descriptions, if a library is present)

Writes:
    README.md at the bundle root.

The output is a *first draft* meant to be edited by the human. It captures
the structural facts a recipient needs (what is this, who is in it, how to
use it) and leaves the "why this network exists" part as a placeholder for
the human to fill in.

By default the script refuses to overwrite an existing README.md — pass
--force to replace. This is so that re-running the generator after the
human has hand-edited doesn't silently nuke their changes.

Usage:
    generate_readme.py <bundle_root> [--force]

Exit codes:
    0  README.md written or already present (without --force)
    1  bundle_root invalid / required files missing
    2  I/O error
"""

import argparse
import json
import re
import sys
from pathlib import Path


def validate_bundle_root(bundle_root: Path) -> str | None:
    if not bundle_root.is_absolute():
        return f"refusing non-absolute path: {bundle_root}"
    if not bundle_root.is_dir():
        return f"{bundle_root}: not a directory"
    if not (bundle_root / ".recipe" / "recipe.json").is_file():
        return f"{bundle_root}: missing .recipe/recipe.json (not a recipe bundle)"
    return None


def load_recipe(bundle_root: Path) -> dict:
    return json.loads((bundle_root / ".recipe" / "recipe.json").read_text(encoding="utf-8"))


def load_exported_from(bundle_root: Path) -> dict | None:
    """Parse the simple key:value file written by mark_export_source.py."""
    p = bundle_root / ".exported-from"
    if not p.is_file():
        return None
    out: dict = {}
    for line in p.read_text(encoding="utf-8").splitlines():
        if ":" in line:
            k, _, v = line.partition(":")
            out[k.strip()] = v.strip()
    return out


def list_agents(bundle_root: Path) -> list[dict]:
    """Return [{name, address, admin, nickname, capabilities, brief?}, ...]."""
    lingtai = bundle_root / ".lingtai"
    if not lingtai.is_dir():
        return []
    agents: list[dict] = []
    for entry in sorted(lingtai.iterdir()):
        if not entry.is_dir() or entry.name.startswith("."):
            continue
        if entry.name == "human":
            continue
        manifest = entry / ".agent.json"
        if not manifest.is_file():
            continue
        try:
            data = json.loads(manifest.read_text(encoding="utf-8"))
        except json.JSONDecodeError:
            continue
        # Pull the first paragraph of brief.md as a short description, if present.
        brief_path = entry / "system" / "brief.md"
        brief_first_para = ""
        if brief_path.is_file():
            text = brief_path.read_text(encoding="utf-8")
            # Skip our own EXPORTED SNAPSHOT banner lines (start with "> "),
            # then take the first non-blank paragraph.
            paras: list[str] = []
            current: list[str] = []
            for line in text.splitlines():
                if line.startswith(">") or line.strip().startswith("# "):
                    if current:
                        paras.append("\n".join(current).strip())
                        current = []
                    continue
                if not line.strip():
                    if current:
                        paras.append("\n".join(current).strip())
                        current = []
                    continue
                current.append(line)
            if current:
                paras.append("\n".join(current).strip())
            if paras:
                brief_first_para = paras[0]
        agents.append({
            "name": entry.name,
            "address": data.get("address") or entry.name,
            "admin": data.get("admin"),
            "nickname": data.get("nickname"),
            "capabilities": data.get("capabilities") or {},
            "brief": brief_first_para,
        })
    return agents


def list_skills(bundle_root: Path, library_name: str | None) -> list[tuple[str, str]]:
    """Return [(skill_name, one_line_description)]; empty if no library or empty library."""
    if not library_name:
        return []
    lib_dir = bundle_root / library_name
    if not lib_dir.is_dir():
        return []
    skills: list[tuple[str, str]] = []
    for child in sorted(lib_dir.iterdir()):
        if not child.is_dir() or child.name.startswith("."):
            continue
        skill_md = child / "SKILL.md"
        if not skill_md.is_file():
            continue
        # Pull `description:` from frontmatter if present, else first prose line.
        text = skill_md.read_text(encoding="utf-8")
        desc = ""
        m = re.search(r"^description:\s*(.+)$", text, re.MULTILINE)
        if m:
            desc = m.group(1).strip().strip('"').strip("'")
        skills.append((child.name, desc))
    return skills


def is_orchestrator(agent: dict) -> bool:
    """Mirror the lingtai skill's check: admin is a dict with at least one truthy boolean."""
    admin = agent.get("admin")
    if not isinstance(admin, dict):
        return False
    return any(bool(v) for v in admin.values())


def render_readme(
    recipe: dict,
    exported_from: dict | None,
    agents: list[dict],
    skills: list[tuple[str, str]],
) -> str:
    lines: list[str] = []
    lines.append(f"# {recipe.get('name', 'Untitled Network')}")
    lines.append("")
    desc = recipe.get("description", "").strip()
    if desc:
        lines.append(f"> {desc}")
        lines.append("")

    if exported_from:
        lines.append("## Origin")
        lines.append("")
        if "name" in exported_from:
            lines.append(f"- **Bundle name**: `{exported_from['name']}`")
        if "source" in exported_from:
            lines.append(f"- **Source**: {exported_from['source']}")
        if "exported_at" in exported_from:
            lines.append(f"- **Exported at**: {exported_from['exported_at']}")
        lines.append("")
        lines.append(
            "This bundle is a network *snapshot*. The original network it was "
            "exported from continues to evolve elsewhere — anything you change "
            "in this clone stays in this clone. Per-agent `init.json` files "
            "have been stripped, so the recipient's TUI will run a rehydration "
            "step on first open and let you pick your own LLM preset."
        )
        lines.append("")

    if agents:
        lines.append("## Agents")
        lines.append("")
        orch = [a for a in agents if is_orchestrator(a)]
        workers = [a for a in agents if not is_orchestrator(a)]
        if orch:
            lines.append("### Orchestrator")
            lines.append("")
            for a in orch:
                lines.append(f"- **`{a['name']}`** — admin agent (network coordinator)")
                if a.get("brief"):
                    lines.append(f"  - {a['brief'].splitlines()[0][:300]}")
            lines.append("")
        if workers:
            lines.append("### Workers")
            lines.append("")
            for a in workers:
                lines.append(f"- **`{a['name']}`**")
                if a.get("brief"):
                    lines.append(f"  - {a['brief'].splitlines()[0][:300]}")
            lines.append("")

    library_name = recipe.get("library_name")
    if library_name and skills:
        lines.append(f"## Library: `{library_name}`")
        lines.append("")
        lines.append(
            f"This network ships a sibling skill library at `{library_name}/`. "
            "Each skill is framework-agnostic — readable by any agent runtime "
            "that consumes `SKILL.md` files."
        )
        lines.append("")
        for name, sdesc in skills:
            if sdesc:
                lines.append(f"- **`{name}`** — {sdesc}")
            else:
                lines.append(f"- **`{name}`**")
        lines.append("")

    lines.append("## Getting Started")
    lines.append("")
    lines.append("1. Install the LingTai TUI: `brew install huangzesen/lingtai/lingtai-tui`")
    lines.append("2. Clone this repo and `cd` into it.")
    lines.append("3. Run `lingtai-tui` in the project root.")
    lines.append("4. The TUI detects stripped `init.json` files and runs the rehydration "
                 "wizard — pick your preferred LLM preset.")
    lines.append("5. After rehydration, the orchestrator launches automatically. Run "
                 "`/cpr all` to wake the workers.")
    lines.append("")

    lines.append("## What's Here")
    lines.append("")
    lines.append("- `.recipe/` — behavioral layer applied on first open (greet, comment, etc.)")
    if library_name:
        lines.append(f"- `{library_name}/` — sibling skill library, registered into every agent")
    lines.append("- `.lingtai/` — full network snapshot: per-agent system files, codex, mailbox archive")
    if exported_from:
        lines.append("- `.exported-from` — origin marker (name, source URL, timestamp)")
    lines.append("")

    lines.append("---")
    lines.append("")
    lines.append("_This README was drafted by `generate_readme.py`. "
                 "Edit it before publishing — add a short story about why the "
                 "network exists, what it figured out, what's worth knowing._")
    return "\n".join(lines) + "\n"


def main() -> int:
    ap = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter
    )
    ap.add_argument("bundle_root", type=Path)
    ap.add_argument("--force", action="store_true",
                    help="Overwrite an existing README.md (default: refuse)")
    args = ap.parse_args()

    bundle_root = args.bundle_root.resolve()
    err = validate_bundle_root(bundle_root)
    if err:
        print(f"error: {err}", file=sys.stderr)
        return 1

    readme_path = bundle_root / "README.md"
    if readme_path.is_file() and not args.force:
        print(f"{readme_path} already exists — pass --force to overwrite. "
              f"Leaving it alone.")
        return 0

    try:
        recipe = load_recipe(bundle_root)
        exported_from = load_exported_from(bundle_root)
        agents = list_agents(bundle_root)
        skills = list_skills(bundle_root, recipe.get("library_name"))
        body = render_readme(recipe, exported_from, agents, skills)
        readme_path.write_text(body, encoding="utf-8")
    except OSError as e:
        print(f"error: I/O failure: {e}", file=sys.stderr)
        return 2
    except json.JSONDecodeError as e:
        print(f"error: malformed JSON: {e}", file=sys.stderr)
        return 1

    print(f"wrote {readme_path} ({readme_path.stat().st_size} bytes)")
    print(f"agents: {len(agents)}, skills: {len(skills)}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
