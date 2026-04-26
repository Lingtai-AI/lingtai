#!/usr/bin/env python3
"""
mark_export_source.py — Stamp a staged network as an export snapshot.

Two effects, both designed so a recipient who clones the bundle realizes
they are running a snapshot rather than the original network:

1. Writes `.exported-from` at the staging root, recording bundle name,
   source URL (if known), and export timestamp. Survives `git add .` —
   that's intentional, it's proof of origin for downstream forks.

2. Prepends an "EXPORTED SNAPSHOT" banner to every agent's
   system/brief.md. brief.md is read by each agent on launch and forms
   the top of its system prompt, so the banner reaches the agent's
   awareness on first turn after rehydration.

Without these markers, a clone wakes up indistinguishable from the
original — same memories, same state, no signal that the world has
changed. With them, the very first thing each agent sees is "you are a
snapshot from <source>; the network you remember is elsewhere."

Usage:
    mark_export_source.py <staging_dir> --name <bundle-name> [--source-url URL]

Both arguments are recorded verbatim in the banner. --source-url is
optional; if omitted, the banner just names the bundle.

Exit codes:
    0  success
    1  staging_dir does not look like a lingtai project
    2  I/O error
"""

import argparse
import sys
from datetime import datetime, timezone
from pathlib import Path


BANNER_TEMPLATE_EN = """\
> **EXPORTED SNAPSHOT.** This network is an export of `{name}`, taken at {when}.
> {source_line}
> The original network you remember continues elsewhere. You are a clone.
> Your chat_history was intentionally stripped; the recipe's greet.md
> serves as 「前尘往事」 — read it for context on who you were.

"""

BANNER_TEMPLATE_ZH = """\
> **此网为导出快照。** 本源 `{name}`，导出时间 {when}。
> {source_line}
> 君所记之原网仍在他处运行，此网为其分身。
> chat_history 已剥离 —— 配方之 greet.md 即为「前尘往事」，可阅以知前事。

"""


def validate_staging_dir(staging_dir: Path) -> str | None:
    if not staging_dir.is_absolute():
        return f"refusing non-absolute path: {staging_dir}"
    if not (staging_dir / ".lingtai").is_dir():
        return f"{staging_dir} has no .lingtai/ directory — not a lingtai project"
    if len(staging_dir.parts) < 3:
        return f"refusing to operate on short path: {staging_dir}"
    return None


def find_real_agents(staging_dir: Path) -> list[Path]:
    """Real agents only — skip dot-dirs and the human pseudo-agent (no system/)."""
    lingtai = staging_dir / ".lingtai"
    agents = []
    for entry in sorted(lingtai.iterdir()):
        if not entry.is_dir() or entry.name.startswith("."):
            continue
        if not (entry / "system").is_dir():
            continue
        agents.append(entry)
    return agents


def write_root_marker(staging_dir: Path, name: str, source_url: str | None, when: str) -> None:
    marker = staging_dir / ".exported-from"
    lines = [
        f"name: {name}",
        f"exported_at: {when}",
    ]
    if source_url:
        lines.append(f"source: {source_url}")
    marker.write_text("\n".join(lines) + "\n", encoding="utf-8")
    print(f"wrote root marker: {marker}")


def banner_text(name: str, source_url: str | None, when: str) -> str:
    """Bilingual banner — short enough that overhead is minimal on every turn."""
    source_line_en = f"Source: {source_url}" if source_url else "Source: (unspecified)"
    source_line_zh = f"来源: {source_url}" if source_url else "来源: (未指定)"
    en = BANNER_TEMPLATE_EN.format(name=name, when=when, source_line=source_line_en)
    zh = BANNER_TEMPLATE_ZH.format(name=name, when=when, source_line=source_line_zh)
    return en + zh


def stamp_brief(agent_dir: Path, banner: str) -> bool:
    """Prepend banner to agent_dir/system/brief.md. Idempotent."""
    brief = agent_dir / "system" / "brief.md"
    if not brief.is_file():
        # Not all agents have brief.md; skip silently.
        return False
    existing = brief.read_text(encoding="utf-8")
    # Idempotency: if our banner sentinel is already there, leave the file alone.
    if "**EXPORTED SNAPSHOT.**" in existing or "**此网为导出快照。**" in existing:
        print(f"  skipped (already stamped): {brief}")
        return False
    brief.write_text(banner + existing, encoding="utf-8")
    print(f"  stamped: {brief}")
    return True


def main() -> int:
    ap = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter
    )
    ap.add_argument("staging_dir", type=Path)
    ap.add_argument("--name", required=True, help="Bundle name (recorded in marker + banner)")
    ap.add_argument("--source-url", default=None, help="Origin URL, e.g. GitHub repo (optional)")
    args = ap.parse_args()

    staging_dir = args.staging_dir.resolve()
    err = validate_staging_dir(staging_dir)
    if err:
        print(f"error: {err}", file=sys.stderr)
        return 1

    when = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M UTC")
    banner = banner_text(args.name, args.source_url, when)

    try:
        write_root_marker(staging_dir, args.name, args.source_url, when)
        stamped = 0
        for agent in find_real_agents(staging_dir):
            print(f"agent: {agent.name}")
            if stamp_brief(agent, banner):
                stamped += 1
        print(f"\nstamped {stamped} brief.md file(s)")
    except OSError as e:
        print(f"error: I/O failure: {e}", file=sys.stderr)
        return 2

    return 0


if __name__ == "__main__":
    sys.exit(main())
