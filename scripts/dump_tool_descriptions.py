#!/usr/bin/env python3
"""Dump all tool descriptions from lingtai-kernel i18n files.

Usage:
    python scripts/dump_tool_descriptions.py
    python scripts/dump_tool_descriptions.py --kernel-repo /path/to/lingtai-kernel

By default, reads both kernel intrinsic and capability i18n JSONs from a
``lingtai-kernel`` checkout next to this repo. Override the checkout path with
``--kernel-repo`` or the ``LINGTAI_KERNEL_REPO`` environment variable.
Outputs one markdown with en / zh / wen columns per tool.
"""
from __future__ import annotations

import argparse
import json
import os
import sys
from pathlib import Path

LANGS = ["en", "zh", "wen"]
LANG_LABELS = {"en": "English", "zh": "中文", "wen": "文言"}

BASE = Path(__file__).resolve().parent.parent
DEFAULT_KERNEL_REPO = BASE.parent / "lingtai-kernel"
OVERRIDE_HELP = "Use --kernel-repo or LINGTAI_KERNEL_REPO to set the lingtai-kernel checkout."


def load_descriptions(path: Path) -> dict[str, str]:
    """Load i18n JSON and return only keys ending with '.description'."""
    if not path.is_file():
        sys.exit(f"error: missing locale file: {path.resolve()}")
    data = json.loads(path.read_text(encoding="utf-8"))
    return {k: v for k, v in data.items() if k.endswith(".description")}


def tool_name(key: str) -> str:
    return key.rsplit(".description", 1)[0]


def collect(i18n_dir: Path) -> dict[str, dict[str, str]]:
    """Return {tool_name: {lang: description}} for all langs."""
    tools: dict[str, dict[str, str]] = {}
    for lang in LANGS:
        descs = load_descriptions(i18n_dir / f"{lang}.json")
        for key, value in descs.items():
            name = tool_name(key)
            tools.setdefault(name, {})[lang] = value
    return tools


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Dump tool descriptions from a lingtai-kernel checkout."
    )
    parser.add_argument(
        "--kernel-repo",
        help="Path to the lingtai-kernel checkout. Overrides LINGTAI_KERNEL_REPO.",
    )
    return parser.parse_args()


def resolve_kernel_repo(args: argparse.Namespace) -> Path:
    configured = args.kernel_repo or os.environ.get("LINGTAI_KERNEL_REPO")
    if configured:
        return Path(configured).expanduser().resolve()
    return DEFAULT_KERNEL_REPO.resolve()


def validate_dir(path: Path) -> None:
    if not path.is_dir():
        sys.exit(f"error: missing i18n directory: {path.resolve()}. {OVERRIDE_HELP}")


def validate_kernel_repo(kernel_repo: Path) -> None:
    if not kernel_repo.is_dir():
        sys.exit(f"error: missing kernel repo: {kernel_repo.resolve()}. {OVERRIDE_HELP}")


def validate_section(title: str, i18n_dir: Path, tools: dict[str, dict[str, str]]) -> None:
    if not tools:
        sys.exit(
            f"error: no .description entries found for {title} in {i18n_dir.resolve()}"
        )


def print_section(title: str, tools: dict[str, dict[str, str]]) -> None:
    print(f"## {title}\n")
    for name, langs in tools.items():
        print(f"### {name}\n")
        for lang in LANGS:
            desc = langs.get(lang, "—")
            print(f"**{LANG_LABELS[lang]}:**\n{desc}\n")


def main() -> None:
    args = parse_args()
    kernel_repo = resolve_kernel_repo(args)
    kernel_i18n = kernel_repo / "src" / "lingtai_kernel" / "i18n"
    lingtai_i18n = kernel_repo / "src" / "lingtai" / "i18n"

    validate_kernel_repo(kernel_repo)
    validate_dir(kernel_i18n)
    validate_dir(lingtai_i18n)

    kernel = collect(kernel_i18n)
    lingtai = collect(lingtai_i18n)
    validate_section("Kernel Intrinsics", kernel_i18n, kernel)
    validate_section("Capabilities", lingtai_i18n, lingtai)

    print("# Tool Descriptions\n")
    print_section("Kernel Intrinsics", kernel)
    print_section("Capabilities", lingtai)


if __name__ == "__main__":
    main()
