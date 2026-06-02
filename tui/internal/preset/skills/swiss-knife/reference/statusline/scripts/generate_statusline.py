#!/usr/bin/env python3
"""Generate and optionally install a Claude Code statusLine script.

The installed script is intentionally self-contained and uses only Python's
standard library so Claude Code can run it without jq, node, bun, or pip
dependencies.
"""

from __future__ import annotations

import argparse
import json
import os
import stat
import subprocess
import sys
import textwrap
from pathlib import Path
from typing import Any, Optional


ELEMENTS = ("model", "context", "cost", "effort", "style", "git", "dir", "worktree", "vim")
PRESETS = {
    "essentials": ("model", "context", "git", "dir"),
    "default": ("model", "context", "effort", "style", "git", "dir"),
    "everything": ("model", "context", "cost", "effort", "style", "git", "dir", "worktree", "vim"),
}
THEMES = ("lingtai", "dracula", "gruvbox", "minimal")


THEME_DATA: dict[str, dict[str, Any]] = {
    "lingtai": {
        "sep": " | ",
        "labels": {
            "model": "model",
            "context": "ctx",
            "cost": "cost",
            "effort": "think",
            "style": "style",
            "git": "git",
            "dir": "dir",
            "worktree": "wt",
            "vim": "vim",
        },
        "colors": {
            "model": "\033[38;5;81m",
            "context_good": "\033[38;5;78m",
            "context_warn": "\033[38;5;221m",
            "context_bad": "\033[1;38;5;203m",
            "cost": "\033[38;5;220m",
            "effort_low": "\033[38;5;78m",
            "effort_medium": "\033[38;5;221m",
            "effort_high": "\033[1;38;5;203m",
            "style": "\033[38;5;147m",
            "git_clean": "\033[38;5;214m",
            "git_dirty": "\033[1;38;5;221m",
            "dir": "\033[38;5;117m",
            "worktree": "\033[1;38;5;183m",
            "vim": "\033[38;5;159m",
            "dim": "\033[38;5;245m",
            "reset": "\033[0m",
        },
    },
    "dracula": {
        "sep": " | ",
        "labels": {
            "model": "md",
            "context": "ctx",
            "cost": "usd",
            "effort": "effort",
            "style": "style",
            "git": "git",
            "dir": "dir",
            "worktree": "wt",
            "vim": "vim",
        },
        "colors": {
            "model": "\033[38;5;117m",
            "context_good": "\033[38;5;84m",
            "context_warn": "\033[38;5;228m",
            "context_bad": "\033[1;38;5;212m",
            "cost": "\033[38;5;222m",
            "effort_low": "\033[38;5;84m",
            "effort_medium": "\033[38;5;228m",
            "effort_high": "\033[1;38;5;212m",
            "style": "\033[38;5;183m",
            "git_clean": "\033[38;5;215m",
            "git_dirty": "\033[1;38;5;228m",
            "dir": "\033[38;5;159m",
            "worktree": "\033[1;38;5;183m",
            "vim": "\033[38;5;84m",
            "dim": "\033[38;5;245m",
            "reset": "\033[0m",
        },
    },
    "gruvbox": {
        "sep": " | ",
        "labels": {
            "model": "model",
            "context": "ctx",
            "cost": "cost",
            "effort": "effort",
            "style": "style",
            "git": "git",
            "dir": "dir",
            "worktree": "wt",
            "vim": "vim",
        },
        "colors": {
            "model": "\033[38;5;109m",
            "context_good": "\033[38;5;142m",
            "context_warn": "\033[38;5;214m",
            "context_bad": "\033[1;38;5;167m",
            "cost": "\033[38;5;214m",
            "effort_low": "\033[38;5;142m",
            "effort_medium": "\033[38;5;214m",
            "effort_high": "\033[1;38;5;167m",
            "style": "\033[38;5;175m",
            "git_clean": "\033[38;5;208m",
            "git_dirty": "\033[1;38;5;214m",
            "dir": "\033[38;5;109m",
            "worktree": "\033[1;38;5;175m",
            "vim": "\033[38;5;142m",
            "dim": "\033[38;5;245m",
            "reset": "\033[0m",
        },
    },
    "minimal": {
        "sep": " | ",
        "labels": {},
        "colors": {},
    },
}


STATUSLINE_TEMPLATE = r'''#!/usr/bin/env python3
"""LingTai-generated Claude Code status line."""

from __future__ import annotations

import json
import os
import subprocess
import sys
from pathlib import Path
from typing import Any, Optional

CONFIG = __CONFIG__
THEME = __THEME__


def dig(obj: Any, *keys: str) -> Any:
    cur = obj
    for key in keys:
        if not isinstance(cur, dict) or key not in cur:
            return None
        cur = cur[key]
    return cur


def first_value(*values: Any) -> Any:
    for value in values:
        if value not in (None, ""):
            return value
    return None


def to_float(value: Any) -> Optional[float]:
    if value is None:
        return None
    if isinstance(value, (int, float)):
        return float(value)
    if isinstance(value, str):
        text = value.strip().removesuffix("%")
        try:
            return float(text)
        except ValueError:
            return None
    return None


def normalize_percent(value: Any) -> Optional[float]:
    pct = to_float(value)
    if pct is None:
        return None
    if 0 <= pct <= 1:
        pct *= 100
    return max(0.0, min(100.0, pct))


def run_git(args: list[str], cwd: Optional[str]) -> str:
    if not cwd:
        return ""
    env = os.environ.copy()
    env["GIT_OPTIONAL_LOCKS"] = "0"
    try:
        proc = subprocess.run(
            ["git", *args],
            cwd=cwd,
            env=env,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            timeout=0.8,
            check=False,
        )
    except Exception:
        return ""
    if proc.returncode != 0:
        return ""
    return proc.stdout.strip()


def shorten(text: str, width: int) -> str:
    if len(text) <= width:
        return text
    if width <= 3:
        return text[:width]
    return text[: width - 3] + "..."


def color(text: str, name: str) -> str:
    colors = THEME.get("colors", {})
    if not colors or os.environ.get("NO_COLOR"):
        return text
    start = colors.get(name, "")
    reset = colors.get("reset", "\033[0m")
    if not start:
        return text
    return f"{start}{text}{reset}"


def label(name: str, value: str, color_name: str) -> str:
    labels = THEME.get("labels", {})
    prefix = labels.get(name, "")
    text = f"{prefix}:{value}" if prefix else value
    return color(text, color_name)


def workspace_dir(data: dict[str, Any]) -> str:
    value = first_value(
        dig(data, "workspace", "current_dir"),
        dig(data, "workspace", "cwd"),
        data.get("cwd"),
    )
    return str(value or "")


def render_model(data: dict[str, Any]) -> str:
    value = first_value(
        dig(data, "model", "display_name"),
        dig(data, "model", "name"),
        data.get("model"),
    )
    return label("model", shorten(str(value), 28), "model") if value else ""


def render_context(data: dict[str, Any]) -> str:
    remaining = first_value(
        dig(data, "context_window", "remaining_percentage"),
        dig(data, "contextWindow", "remaining_percentage"),
        dig(data, "context", "remaining_percentage"),
    )
    used = first_value(
        dig(data, "context_window", "used_percentage"),
        dig(data, "contextWindow", "used_percentage"),
        dig(data, "context", "used_percentage"),
    )
    remaining_pct = normalize_percent(remaining)
    used_pct = normalize_percent(used)
    if used_pct is None and remaining_pct is not None:
        used_pct = 100 - remaining_pct
    if remaining_pct is None and used_pct is not None:
        remaining_pct = 100 - used_pct
    if used_pct is None:
        return ""

    width = 12
    filled = int(round(width * used_pct / 100))
    bar = "[" + ("#" * filled) + ("-" * (width - filled)) + f"] {used_pct:.0f}%"
    if remaining_pct is None or remaining_pct > 50:
        color_name = "context_good"
    elif remaining_pct >= 20:
        color_name = "context_warn"
    else:
        color_name = "context_bad"
    return label("context", bar, color_name)


def render_cost(data: dict[str, Any]) -> str:
    value = to_float(first_value(dig(data, "cost", "total_cost_usd"), data.get("total_cost_usd")))
    if value is None or value < 0.005:
        return ""
    return label("cost", f"${value:.2f}", "cost")


def settings_effort(workspace: str) -> str:
    paths: list[Path] = []
    if workspace:
        paths.append(Path(workspace) / ".claude" / "settings.local.json")
        paths.append(Path(workspace) / ".claude" / "settings.json")
    paths.extend([
        Path.home() / ".claude" / "settings.local.json",
        Path.home() / ".claude" / "settings.json",
    ])
    for path in paths:
        try:
            data = json.loads(path.read_text())
        except Exception:
            continue
        value = first_value(
            data.get("effortLevel"),
            data.get("effort_level"),
            dig(data, "modelSettings", "effort"),
            dig(data, "model_settings", "effort"),
        )
        if value:
            return str(value)
    return ""


def render_effort(data: dict[str, Any]) -> str:
    value = first_value(data.get("effortLevel"), data.get("effort_level"))
    if not value:
        value = settings_effort(workspace_dir(data))
    if not value:
        return ""
    lowered = str(value).lower()
    if lowered in {"max", "xhigh", "high"}:
        color_name = "effort_high"
    elif lowered == "medium":
        color_name = "effort_medium"
    elif lowered in {"low", "xlow", "minimal"}:
        color_name = "effort_low"
    else:
        color_name = "dim"
    return label("effort", str(value), color_name)


def render_style(data: dict[str, Any]) -> str:
    value = first_value(dig(data, "output_style", "name"), dig(data, "outputStyle", "name"))
    if not value or str(value).lower() == "default":
        return ""
    return label("style", shorten(str(value), 18), "style")


def render_git(data: dict[str, Any]) -> str:
    cwd = workspace_dir(data)
    branch = first_value(
        dig(data, "worktree", "branch"),
        dig(data, "git", "branch"),
        run_git(["rev-parse", "--abbrev-ref", "HEAD"], cwd),
    )
    if not branch:
        return ""
    dirty = bool(run_git(["status", "--short", "--untracked-files=no"], cwd))
    suffix = "*" if dirty else ""
    color_name = "git_dirty" if dirty else "git_clean"
    return label("git", shorten(str(branch) + suffix, 32), color_name)


def render_dir(data: dict[str, Any]) -> str:
    raw = first_value(dig(data, "worktree", "original_repo_dir"), workspace_dir(data))
    if not raw:
        return ""
    name = Path(str(raw)).name or str(raw)
    return label("dir", shorten(name, 24), "dir")


def render_worktree(data: dict[str, Any]) -> str:
    cwd = workspace_dir(data)
    name = first_value(dig(data, "worktree", "name"), dig(data, "worktree", "id"))
    if not name:
        common = run_git(["rev-parse", "--git-common-dir"], cwd)
        parts = Path(common).parts if common else ()
        if "worktrees" in parts:
            idx = parts.index("worktrees")
            if idx + 1 < len(parts):
                name = parts[idx + 1]
    if not name:
        return ""
    return label("worktree", shorten(f"worktree:{name}", 24), "worktree")


def render_vim(data: dict[str, Any]) -> str:
    mode = first_value(dig(data, "vim", "mode"), data.get("vim_mode"))
    if not mode or str(mode).lower() in {"inactive", "none", "off"}:
        return ""
    return label("vim", str(mode), "vim")


RENDERERS = {
    "model": render_model,
    "context": render_context,
    "cost": render_cost,
    "effort": render_effort,
    "style": render_style,
    "git": render_git,
    "dir": render_dir,
    "worktree": render_worktree,
    "vim": render_vim,
}


def main() -> int:
    try:
        raw = sys.stdin.read()
        data = json.loads(raw) if raw.strip() else {}
    except Exception:
        data = {}

    parts: list[str] = []
    for element in CONFIG["elements"]:
        rendered = RENDERERS[element](data)
        if rendered:
            parts.append(rendered)
    print(THEME.get("sep", " | ").join(parts))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
'''


SAMPLE_INPUT = {
    "model": {"display_name": "Claude Opus 4.7"},
    "context_window": {"remaining_percentage": 42},
    "cost": {"total_cost_usd": 0.37},
    "effortLevel": "high",
    "output_style": {"name": "Explanatory"},
    "workspace": {"current_dir": "/Users/example/Projects/lingtai"},
    "worktree": {
        "branch": "feature/statusline-customization",
        "name": "46a6",
        "original_repo_dir": "/Users/example/Projects/lingtai",
    },
    "vim": {"mode": "normal"},
}


def parse_elements(raw: str) -> tuple[str, ...]:
    seen: list[str] = []
    for item in raw.split(","):
        name = item.strip()
        if not name:
            continue
        if name not in ELEMENTS:
            raise argparse.ArgumentTypeError(f"unknown element {name!r}; choose from {', '.join(ELEMENTS)}")
        if name not in seen:
            seen.append(name)
    if not seen:
        raise argparse.ArgumentTypeError("at least one element is required")
    return tuple(seen)


def build_script(elements: tuple[str, ...], theme_name: str) -> str:
    config = {"elements": list(elements)}
    theme = THEME_DATA[theme_name]
    script = STATUSLINE_TEMPLATE.replace("__CONFIG__", json.dumps(config, indent=2))
    script = script.replace("__THEME__", json.dumps(theme, indent=2))
    return script


def expand(path: str) -> Path:
    return Path(os.path.expandvars(path)).expanduser()


def install(script: str, script_path: Path, settings_path: Path, backup: bool) -> None:
    script_path.parent.mkdir(parents=True, exist_ok=True)
    settings_path.parent.mkdir(parents=True, exist_ok=True)

    script_path.write_text(script)
    mode = script_path.stat().st_mode
    script_path.chmod(mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)

    if settings_path.exists():
        try:
            settings = json.loads(settings_path.read_text())
        except json.JSONDecodeError as exc:
            raise SystemExit(f"{settings_path} is not valid JSON: {exc}") from exc
        if backup:
            backup_path = settings_path.with_suffix(settings_path.suffix + ".bak")
            if not backup_path.exists():
                backup_path.write_text(settings_path.read_text())
    else:
        settings = {}

    settings["statusLine"] = {
        "type": "command",
        "command": str(script_path),
    }
    settings_path.write_text(json.dumps(settings, indent=2) + "\n")


def sample_output(script: str) -> str:
    proc = subprocess.run(
        [sys.executable, "-c", script],
        input=json.dumps(SAMPLE_INPUT),
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=False,
    )
    if proc.returncode != 0:
        raise SystemExit(proc.stderr.strip() or "sample render failed")
    return proc.stdout.rstrip("\n")


def main(argv: Optional[list[str]] = None) -> int:
    parser = argparse.ArgumentParser(
        description="Generate and optionally install a LingTai Claude Code status line.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=textwrap.dedent(
            """\
            Examples:
              generate_statusline.py --preset default --theme lingtai --sample
              generate_statusline.py --preset everything --theme dracula --install
              generate_statusline.py --elements model,context,git,dir --print-script
            """
        ),
    )
    parser.add_argument("--preset", choices=sorted(PRESETS), default="default")
    parser.add_argument("--elements", type=parse_elements, help="comma-separated status line elements")
    parser.add_argument("--theme", choices=THEMES, default="lingtai")
    parser.add_argument("--install", action="store_true", help="write script and update ~/.claude/settings.json")
    parser.add_argument("--sample", action="store_true", help="render a sample status line")
    parser.add_argument("--print-script", action="store_true", help="print the generated runtime script")
    parser.add_argument("--script-path", default="~/.claude/scripts/lingtai_statusline.py")
    parser.add_argument("--settings-path", default="~/.claude/settings.json")
    parser.add_argument("--no-backup", action="store_true", help="do not create settings.json.bak during install")
    args = parser.parse_args(argv)

    elements = args.elements or PRESETS[args.preset]
    script = build_script(elements, args.theme)

    if args.print_script:
        print(script, end="")
    if args.sample:
        print(sample_output(script))
    if args.install:
        script_path = expand(args.script_path)
        settings_path = expand(args.settings_path)
        install(script, script_path, settings_path, backup=not args.no_backup)
        print(f"installed status line script: {script_path}")
        print(f"updated Claude settings: {settings_path}")
    if not (args.print_script or args.sample or args.install):
        parser.print_help()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
