"""CRUD helpers for addon account templates at ~/.lingtai/addons/.

Each addon (imap, telegram) has a subfolder per account alias:
    ~/.lingtai/addons/{addon}/{alias}/config.json

These are reusable credential/config templates. Agents copy them
into their working directory when attaching an addon.
"""
from __future__ import annotations

import json
import shutil
from pathlib import Path


def _addons_dir() -> Path:
    return Path.home() / ".lingtai" / "addons"


def list_addon_accounts(addon_name: str) -> list[str]:
    """Return sorted list of alias names that have a config.json."""
    addon_dir = _addons_dir() / addon_name
    if not addon_dir.is_dir():
        return []
    return sorted(
        d.name
        for d in addon_dir.iterdir()
        if d.is_dir() and (d / "config.json").is_file()
    )


def load_addon_account(addon_name: str, alias: str) -> dict:
    """Load and return the config dict for an alias.

    Raises FileNotFoundError if the alias or config.json does not exist.
    """
    cfg_path = _addons_dir() / addon_name / alias / "config.json"
    if not cfg_path.is_file():
        raise FileNotFoundError(
            f"No account template: ~/.lingtai/addons/{addon_name}/{alias}/config.json"
        )
    return json.loads(cfg_path.read_text(encoding="utf-8"))


def save_addon_account(addon_name: str, alias: str, config: dict) -> Path:
    """Write config to ~/.lingtai/addons/{addon}/{alias}/config.json.

    Creates directories as needed. Overwrites existing config.
    Returns the path to the written file.
    """
    cfg_dir = _addons_dir() / addon_name / alias
    cfg_dir.mkdir(parents=True, exist_ok=True)
    cfg_path = cfg_dir / "config.json"
    cfg_path.write_text(
        json.dumps(config, indent=2, ensure_ascii=False) + "\n",
        encoding="utf-8",
    )
    return cfg_path


def delete_addon_account(addon_name: str, alias: str) -> None:
    """Remove an alias folder entirely.

    Raises FileNotFoundError if the alias does not exist.
    """
    alias_dir = _addons_dir() / addon_name / alias
    if not alias_dir.is_dir():
        raise FileNotFoundError(
            f"No account template: ~/.lingtai/addons/{addon_name}/{alias}/"
        )
    shutil.rmtree(alias_dir)
