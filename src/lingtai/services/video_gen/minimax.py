"""MiniMax video generation service — wraps the minimax-mcp media server."""
from __future__ import annotations

import hashlib
import re
import time
from pathlib import Path
from typing import Any

from lingtai_kernel.logging import get_logger

from . import VideoGenService

logger = get_logger()


class MiniMaxVideoGenService(VideoGenService):
    """Video generation via the MiniMax ``minimax-mcp`` media server.

    Creates its own ``MCPClient`` subprocess connected to the full
    ``minimax-mcp`` package.  Supports both text-to-video (T2V) and
    image-to-video (I2V) modes.

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

    # -- VideoGenService interface ---------------------------------------------

    def generate(
        self,
        prompt: str,
        *,
        first_frame_image: str | None = None,
        model: str | None = None,
        duration: int | None = None,
        resolution: str | None = None,
        output_dir: Path | None = None,
        **kwargs: Any,
    ) -> Path:
        """Generate a video via MiniMax MCP ``generate_video`` tool.

        The MCP server handles the full generation lifecycle (including
        polling for async tasks internally).  The result is either a file
        saved directly to *output_dir* or a URL that we download.

        Returns:
            Path to the generated video file.

        Raises:
            RuntimeError: On any failure.
        """
        if output_dir is None:
            output_dir = Path.cwd() / "videos"
        output_dir.mkdir(parents=True, exist_ok=True)

        mcp = self._ensure_mcp()

        mcp_args: dict[str, Any] = {
            "prompt": prompt,
            "output_directory": str(output_dir),
        }
        if first_frame_image is not None:
            mcp_args["first_frame_image"] = first_frame_image
            # Auto-select I2V model when an image is provided
            if model is None:
                model = "I2V-01"
        if model is not None:
            mcp_args["model"] = model
        if duration is not None:
            mcp_args["duration"] = duration
        if resolution is not None:
            mcp_args["resolution"] = resolution

        try:
            result = mcp.call_tool("generate_video", mcp_args)
        except Exception as exc:
            raise RuntimeError(f"MCP call failed: {exc}") from exc

        if isinstance(result, dict) and result.get("status") == "error":
            raise RuntimeError(result.get("message", "Unknown MCP error"))

        # Check if MCP saved a file to output_directory
        video_files = (
            sorted(output_dir.glob("*.mp4"))
            + sorted(output_dir.glob("*.webm"))
            + sorted(output_dir.glob("*.mov"))
        )
        if video_files:
            latest = video_files[-1]
            # Rename to a unique timestamped name to prevent overwrites
            ts = int(time.time())
            short_hash = hashlib.md5(prompt.encode()).hexdigest()[:4]
            unique_name = f"video_{ts}_{short_hash}{latest.suffix}"
            unique_path = output_dir / unique_name
            if latest.name != unique_name:
                latest.rename(unique_path)
                logger.debug("MiniMaxVideoGen: renamed to %s", unique_path)
                return unique_path
            logger.debug("MiniMaxVideoGen: found saved file %s", latest)
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
    """Download a video file from *url* into *output_dir*."""
    try:
        import requests

        resp = requests.get(url, timeout=300)
        resp.raise_for_status()
        ts = int(time.time())
        short_hash = hashlib.md5(prompt.encode()).hexdigest()[:4]
        filename = f"video_{ts}_{short_hash}.mp4"
        out_path = output_dir / filename
        out_path.write_bytes(resp.content)
        return out_path
    except Exception as exc:
        raise RuntimeError(f"Failed to download video from {url}: {exc}") from exc
