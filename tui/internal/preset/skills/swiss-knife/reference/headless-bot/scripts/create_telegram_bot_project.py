#!/usr/bin/env python3
"""Create a fresh LingTai bot project with Telegram MCP wiring.

The token is read from TELEGRAM_BOT_TOKEN so it does not appear in argv.
All status output avoids printing the token.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
from pathlib import Path
from typing import Any


TOKENISH = re.compile(r"\b\d{6,}:[A-Za-z0-9_-]{20,}\b")


def redact(text: str) -> str:
    return TOKENISH.sub("<redacted-telegram-bot-token>", text)


def parse_allowed_users(raw: str) -> list[int]:
    users: list[int] = []
    for item in raw.split(","):
        item = item.strip()
        if not item:
            continue
        try:
            users.append(int(item))
        except ValueError as exc:
            raise SystemExit(f"allowed user IDs must be integers: {item!r}") from exc
    if not users:
        raise SystemExit("--allowed-users must contain at least one Telegram numeric user ID")
    return users


def load_json(path: Path) -> dict[str, Any]:
    with path.open("r", encoding="utf-8") as f:
        data = json.load(f)
    if not isinstance(data, dict):
        raise SystemExit(f"{path} must contain a JSON object")
    return data


def write_json(path: Path, data: dict[str, Any], mode: int | None = None) -> None:
    path.write_text(json.dumps(data, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    if mode is not None:
        path.chmod(mode)


def write_secret_json(path: Path, data: dict[str, Any]) -> None:
    flags = os.O_CREAT | os.O_TRUNC | os.O_WRONLY
    flags |= getattr(os, "O_NOFOLLOW", 0)
    fd = os.open(path, flags, 0o600)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as f:
            fd = -1
            json.dump(data, f, indent=2, ensure_ascii=False)
            f.write("\n")
    finally:
        if fd >= 0:
            os.close(fd)
    path.chmod(0o600)


def runtime_python() -> str:
    suffix = "Scripts/python.exe" if os.name == "nt" else "bin/python"
    return str(Path.home() / ".lingtai-tui" / "runtime" / "venv" / suffix)


def ensure_telegram_init(init_path: Path) -> None:
    init = load_json(init_path)

    addons = init.get("addons")
    if addons is None:
        addons = []
    if not isinstance(addons, list):
        raise SystemExit(f"{init_path}: top-level addons exists but is not a list")
    if "telegram" not in addons:
        addons.append("telegram")
    init["addons"] = addons

    mcp = init.get("mcp")
    if mcp is None:
        mcp = {}
    if not isinstance(mcp, dict):
        raise SystemExit(f"{init_path}: top-level mcp exists but is not an object")
    mcp.setdefault(
        "telegram",
        {
            "type": "stdio",
            "command": runtime_python(),
            "args": ["-m", "lingtai_telegram"],
            "env": {"LINGTAI_TELEGRAM_CONFIG": ".secrets/telegram.json"},
        },
    )
    init["mcp"] = mcp

    write_json(init_path, init)


def apply_preset_policy(init_path: Path, source_init_path: Path) -> None:
    source = load_json(source_init_path)
    source_manifest = source.get("manifest")
    if not isinstance(source_manifest, dict):
        raise SystemExit(f"{source_init_path}: missing manifest object")
    preset_policy = source_manifest.get("preset")
    if not isinstance(preset_policy, dict):
        raise SystemExit(f"{source_init_path}: missing manifest.preset policy object")

    target = load_json(init_path)
    target_manifest = target.get("manifest")
    if target_manifest is None:
        target_manifest = {}
    if not isinstance(target_manifest, dict):
        raise SystemExit(f"{init_path}: top-level manifest exists but is not an object")
    target_manifest["preset"] = preset_policy
    target["manifest"] = target_manifest
    write_json(init_path, target)


def run_spawn(args: argparse.Namespace) -> dict[str, Any]:
    cmd = [
        "lingtai-tui",
        "spawn",
        str(args.project_dir),
        "--preset",
        args.preset,
        "--agent-name",
        args.agent_name,
        "--language",
        args.language,
    ]
    proc = subprocess.run(cmd, text=True, capture_output=True, check=False)
    if proc.returncode != 0:
        sys.stderr.write(redact(proc.stderr or proc.stdout))
        raise SystemExit(proc.returncode)
    try:
        data = json.loads(proc.stdout)
    except json.JSONDecodeError as exc:
        raise SystemExit("lingtai-tui spawn did not return JSON:\n" + redact(proc.stdout)) from exc
    if not isinstance(data, dict) or not data.get("agent_dir"):
        raise SystemExit("lingtai-tui spawn JSON did not include agent_dir")
    return data


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--project-dir", required=True, type=Path)
    parser.add_argument("--preset", required=True)
    parser.add_argument("--agent-name", required=True)
    parser.add_argument("--language", default="en", choices=("en", "zh", "wen"))
    parser.add_argument("--allowed-users", required=True, help="Comma-separated Telegram numeric user IDs")
    parser.add_argument("--alias", default="main")
    parser.add_argument("--poll-interval", default=1.0, type=float)
    parser.add_argument(
        "--preset-policy-from",
        type=Path,
        help="Optional source agent init.json whose manifest.preset policy should be copied by reference",
    )
    args = parser.parse_args()

    token = os.environ.get("TELEGRAM_BOT_TOKEN")
    if not token:
        raise SystemExit("TELEGRAM_BOT_TOKEN is required and must not be passed on argv")

    allowed_users = parse_allowed_users(args.allowed_users)
    spawn = run_spawn(args)
    agent_dir = Path(spawn["agent_dir"])

    secrets_dir = agent_dir / ".secrets"
    secrets_dir.mkdir(mode=0o700, exist_ok=True)
    secrets_dir.chmod(0o700)

    telegram_config = {
        "accounts": [
            {
                "alias": args.alias,
                "bot_token": token,
                "allowed_users": allowed_users,
            }
        ],
        "poll_interval": args.poll_interval,
    }
    write_secret_json(secrets_dir / "telegram.json", telegram_config)

    init_path = agent_dir / "init.json"
    ensure_telegram_init(init_path)
    if args.preset_policy_from:
        apply_preset_policy(init_path, args.preset_policy_from)

    refresh_path = agent_dir / ".refresh"
    refresh_path.touch()

    result = {
        "status": "configured",
        "project_dir": spawn.get("project_dir"),
        "agent_name": spawn.get("agent_name"),
        "agent_dir": str(agent_dir),
        "telegram_config": str(secrets_dir / "telegram.json"),
        "init_json": str(init_path),
        "refresh_signal": str(refresh_path),
        "preset_policy_from": str(args.preset_policy_from) if args.preset_policy_from else None,
        "next_checks": [
            "lingtai-tui list <project_dir>",
            "Bot API getMe with token redacted",
            "agent log shows Telegram listener/MCP startup",
            "allowed user sends /start",
        ],
    }
    print(redact(json.dumps(result, indent=2)))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
