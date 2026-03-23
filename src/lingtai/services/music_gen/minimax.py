"""MiniMax music generation service — wraps the minimax-mcp media server."""
from __future__ import annotations

import hashlib
import re
import time
from pathlib import Path
from typing import Any

from lingtai_kernel.logging import get_logger

from . import MusicGenService

logger = get_logger()


class MiniMaxMusicGenService(MusicGenService):
    """Music generation via the MiniMax ``minimax-mcp`` media server.

    Creates its own ``MCPClient`` subprocess connected to the full
    ``minimax-mcp`` package (the media server, *not* the coding-plan server).
    The client is started lazily on the first call to :meth:`generate` and
    cleaned up via :meth:`close` or ``atexit``.

    Args:
        api_key: MiniMax API key.  Falls back to ``MINIMAX_API_KEY`` env var.
        api_host: API host URL.  Falls back to ``MINIMAX_API_HOST`` env var.
        **kwargs: Forwarded to
            :func:`~lingtai.llm.minimax.mcp_media_client.create_minimax_media_client`.
    """

    def __init__(
        self,
        *,
        api_key: str | None = None,
        api_host: str | None = None,
        **kwargs: Any,
    ) -> None:
        self._api_key = api_key
        self._api_host = api_host
        self._extra_kwargs = kwargs
        self._mcp: Any | None = None  # MCPClient, created lazily

    # -- lazy MCP lifecycle ----------------------------------------------------

    def _ensure_mcp(self) -> Any:
        """Return (and lazily create) the underlying MCPClient."""
        if self._mcp is None:
            from lingtai.llm.minimax.mcp_media_client import (
                create_minimax_media_client,
            )

            self._mcp = create_minimax_media_client(
                api_key=self._api_key,
                api_host=self._api_host,
                **self._extra_kwargs,
            )
        return self._mcp

    def close(self) -> None:
        """Shut down the underlying MCP subprocess."""
        if self._mcp is not None:
            try:
                self._mcp.close()
            except Exception:
                pass
            self._mcp = None

    # -- MusicGenService interface ---------------------------------------------

    def generate(
        self,
        prompt: str,
        *,
        lyrics: str | None = None,
        output_dir: Path | None = None,
        **kwargs: Any,
    ) -> Path:
        """Generate music via MiniMax MCP ``music_generation`` tool.

        The MCP server may either save the file directly to *output_dir* or
        return a URL.  In the latter case the file is downloaded into
        *output_dir*.

        Returns:
            Path to the generated audio file.

        Raises:
            RuntimeError: On any failure (missing params, MCP error, download
                failure, unexpected response).
        """
        if output_dir is None:
            output_dir = Path.cwd() / "music"
        output_dir.mkdir(parents=True, exist_ok=True)

        mcp = self._ensure_mcp()

        mcp_args: dict[str, Any] = {
            "prompt": prompt,
            "output_directory": str(output_dir),
        }
        if lyrics is not None:
            mcp_args["lyrics"] = lyrics

        try:
            result = mcp.call_tool("music_generation", mcp_args)
        except Exception as exc:
            raise RuntimeError(f"MCP call failed: {exc}") from exc

        if isinstance(result, dict) and result.get("status") == "error":
            raise RuntimeError(result.get("message", "Unknown MCP error"))

        # Check if MCP saved a file to output_directory
        music_files = sorted(output_dir.glob("*.mp3")) + sorted(
            output_dir.glob("*.wav")
        )
        if music_files:
            latest = music_files[-1]
            logger.debug("MiniMaxMusicGen: found saved file %s", latest)
            return latest

        # Fallback: MCP may have returned a URL in text
        result_text = _extract_text(result)
        url = _extract_url(result_text)
        if url:
            return _download(url, prompt, output_dir)

        raise RuntimeError(f"Unexpected MCP response: {result_text}")


# ---------------------------------------------------------------------------
# Helpers (private)
# ---------------------------------------------------------------------------

def _extract_text(result: Any) -> str:
    """Extract text from an MCP call result."""
    if isinstance(result, dict):
        return result.get("text", str(result))
    return str(result)


def _extract_url(text: str) -> str | None:
    """Extract the first HTTP(S) URL from text."""
    match = re.search(r"https?://\S+", text)
    return match.group(0).rstrip("']") if match else None


def _download(url: str, prompt: str, output_dir: Path) -> Path:
    """Download an audio file from *url* into *output_dir*."""
    try:
        import requests

        resp = requests.get(url, timeout=120)
        resp.raise_for_status()
        ts = int(time.time())
        short_hash = hashlib.md5(prompt.encode()).hexdigest()[:4]
        filename = f"compose_{ts}_{short_hash}.mp3"
        out_path = output_dir / filename
        out_path.write_bytes(resp.content)
        return out_path
    except Exception as exc:
        raise RuntimeError(f"Failed to download music from {url}: {exc}") from exc
