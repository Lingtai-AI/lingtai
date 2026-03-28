"""Derive MiniMax media API host from the agent's LLM base_url."""
from __future__ import annotations

from typing import TYPE_CHECKING
from urllib.parse import urlparse, urlunparse

if TYPE_CHECKING:
    from lingtai_kernel.base_agent import BaseAgent


def resolve_media_host(agent: "BaseAgent") -> str | None:
    """Extract the media API host from the agent's LLM service base_url.

    The LLM ``base_url`` includes a path component (e.g.
    ``https://api.minimaxi.com/anthropic``).  The MCP media server needs
    just the origin (``https://api.minimaxi.com``).

    Returns ``None`` when the agent has no ``base_url`` configured, letting
    the downstream default take over.
    """
    base_url = getattr(agent.service, "_base_url", None)
    if not base_url:
        return None
    parsed = urlparse(base_url)
    return urlunparse((parsed.scheme, parsed.netloc, "", "", "", ""))
