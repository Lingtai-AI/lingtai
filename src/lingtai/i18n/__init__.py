"""Capability i18n — language-aware string tables for lingtai capabilities.

Usage: t(lang, key, **kwargs)
  lang: language code ("en", "zh", "wen")
  key: dotted string ID ("read.description")
  kwargs: template substitutions

The kernel ships en.json + zh.json + wen.json (kernel-level strings).
Lingtai ships en.json + zh.json + wen.json covering capability strings.
On first load, any kernel-level keys found in lingtai's tables are
injected into the kernel's i18n cache via register_strings() so that
kernel-level t() calls resolve correctly (additive, not destructive).
"""
from __future__ import annotations

import json
from collections import defaultdict
from pathlib import Path

_DIR = Path(__file__).parent
_CACHE: dict[str, dict[str, str]] = {}

# Keys that belong to the kernel's i18n namespace.
# When we load a language that the kernel doesn't ship (e.g. wen),
# we extract these keys and inject them into the kernel's cache.
_KERNEL_PREFIXES = (
    "system.", "soul.", "mail.", "eigen.", "system_tool.", "tool.",
)


def _load(lang: str) -> dict[str, str]:
    if lang not in _CACHE:
        path = _DIR / f"{lang}.json"
        if path.is_file():
            _CACHE[lang] = json.loads(path.read_text(encoding="utf-8"))
        else:
            _CACHE[lang] = {}
        # Inject kernel-level keys into the kernel's i18n cache
        # for languages the kernel doesn't carry natively.
        _sync_to_kernel(lang)
    return _CACHE[lang]


def _sync_to_kernel(lang: str) -> None:
    """Push kernel-level keys from our table into kernel's i18n cache."""
    from lingtai_kernel.i18n import register_strings

    table = _CACHE.get(lang, {})
    kernel_keys = {
        k: v for k, v in table.items()
        if k.startswith(_KERNEL_PREFIXES)
    }
    if kernel_keys:
        register_strings(lang, kernel_keys)


def t(lang: str, key: str, **kwargs) -> str:
    table = _load(lang)
    value = table.get(key)
    if value is None and lang != "en":
        value = _load("en").get(key)
    if value is None:
        return key
    if kwargs:
        return value.format_map(defaultdict(str, kwargs))
    return value
