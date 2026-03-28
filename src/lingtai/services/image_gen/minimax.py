"""MiniMaxImageGenService — text-to-image via MiniMax MCP server.

Creates its own MCPClient for the full ``minimax-mcp`` media server.
Extracted from the draw capability's direct MCP usage.
"""
from __future__ import annotations

import hashlib
import re
import time
from pathlib import Path
from typing import Any

from lingtai_kernel.logging import get_logger

from . import ImageGenService

logger = get_logger()


class MiniMaxImageGenService(ImageGenService):
    """Image generation via MiniMax's text_to_image MCP tool.

    Creates and manages its own MCPClient connected to the full
    ``minimax-mcp`` server (requires ``MINIMAX_API_KEY`` env var or
    explicit ``api_key``).
    """

    def __init__(
        self,
        *,
        api_key: str | None = None,
        api_host: str | None = None,
        mcp_client: Any | None = None,
    ) -> None:
        self._owns_client = mcp_client is None
        if mcp_client is not None:
            self._mcp = mcp_client
        else:
            from ...llm.minimax.mcp_media_client import create_minimax_media_client
            self._mcp = create_minimax_media_client(
                api_key=api_key,
                api_host=api_host,
            )

    def generate(
        self,
        prompt: str,
        *,
        aspect_ratio: str | None = None,
        output_dir: Path | None = None,
        **kwargs: Any,
    ) -> Path:
        """Generate an image via MiniMax MCP text_to_image tool."""
        if output_dir is None:
            output_dir = Path.cwd() / "images"
        output_dir.mkdir(parents=True, exist_ok=True)

        mcp_args: dict[str, Any] = {"prompt": prompt}
        if aspect_ratio:
            mcp_args["aspect_ratio"] = aspect_ratio
        mcp_args["output_directory"] = str(output_dir)

        try:
            result = self._mcp.call_tool("text_to_image", mcp_args)
        except Exception as exc:
            raise RuntimeError(f"MCP call failed: {exc}") from exc

        if isinstance(result, dict) and result.get("status") == "error":
            raise RuntimeError(result.get("message", "Unknown MCP error"))

        # Check if MCP saved file to output_directory
        image_files = (
            sorted(output_dir.glob("*.jpeg"))
            + sorted(output_dir.glob("*.jpg"))
            + sorted(output_dir.glob("*.png"))
        )
        if image_files:
            latest = image_files[-1]
            # Rename to a unique timestamped name to prevent overwrites
            ts = int(time.time())
            short_hash = hashlib.md5(prompt.encode()).hexdigest()[:4]
            unique_name = f"draw_{ts}_{short_hash}{latest.suffix}"
            unique_path = output_dir / unique_name
            if latest.name != unique_name:
                latest.rename(unique_path)
                return unique_path
            return latest

        # Fallback: download from URL in result text
        result_text = self._extract_text(result)
        url = self._extract_url(result_text)
        if url:
            try:
                import requests
                resp = requests.get(url, timeout=60)
                resp.raise_for_status()
                ts = int(time.time())
                short_hash = hashlib.md5(prompt.encode()).hexdigest()[:4]
                filename = f"draw_{ts}_{short_hash}.jpeg"
                out_path = output_dir / filename
                out_path.write_bytes(resp.content)
                return out_path
            except Exception as exc:
                raise RuntimeError(f"Failed to download image: {exc}") from exc

        raise RuntimeError(f"Unexpected MCP response: {result_text}")

    def close(self) -> None:
        """Close the MCP client if we own it."""
        if self._owns_client and hasattr(self._mcp, "close"):
            self._mcp.close()

    @staticmethod
    def _extract_text(result: Any) -> str:
        """Extract text from an MCP call result."""
        if isinstance(result, dict):
            return result.get("text", str(result))
        return str(result)

    @staticmethod
    def _extract_url(text: str) -> str | None:
        """Extract the first HTTP(S) URL from text."""
        match = re.search(r"https?://\S+", text)
        return match.group(0).rstrip("']") if match else None
