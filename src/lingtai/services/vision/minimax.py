"""MiniMax vision service — standalone image analysis via MiniMax MCP understand_image tool."""
from __future__ import annotations

import base64

from . import VisionService, _read_image

from lingtai_kernel.logging import get_logger

logger = get_logger()


class MiniMaxVisionService(VisionService):
    """Image understanding via MiniMax's ``understand_image`` MCP tool.

    Creates its own MCPClient for the ``minimax-coding-plan-mcp`` server.
    The ``api_key`` is passed as ``MINIMAX_API_KEY`` in the subprocess env.
    """

    def __init__(
        self,
        *,
        api_key: str,
        api_host: str | None = None,
    ) -> None:
        self._api_key = api_key
        if not api_host:
            raise RuntimeError(
                "api_host is required for MiniMax vision service."
            )
        self._api_host = api_host
        self._client = None  # lazy init

    def _ensure_client(self):
        """Lazily start the MCP client subprocess."""
        if self._client is not None:
            from ...services.mcp import MCPClient
            if self._client.is_connected():
                return
            # Try to close stale client
            try:
                self._client.close()
            except Exception:
                pass

        import os
        import shutil
        from ...services.mcp import MCPClient

        uvx_path = shutil.which("uvx")
        if not uvx_path:
            raise RuntimeError(
                "uvx not found. Please install uv: "
                "https://docs.astral.sh/uv/getting-started/installation/"
            )

        env = {
            **os.environ,
            "MINIMAX_API_KEY": self._api_key,
            "MINIMAX_API_HOST": self._api_host,
        }
        self._client = MCPClient(
            command=uvx_path,
            args=["minimax-coding-plan-mcp", "-y"],
            env=env,
        )
        self._client.start()

    def analyze_image(self, image_path: str, prompt: str | None = None) -> str:
        """Analyze an image using MiniMax's understand_image MCP tool."""
        image_bytes, mime_type = _read_image(image_path)
        question = prompt or "Describe this image."

        self._ensure_client()

        b64 = base64.b64encode(image_bytes).decode("ascii")
        result = self._client.call_tool("understand_image", {
            "image_source": f"data:{mime_type};base64,{b64}",
            "prompt": question,
        })
        if result.get("status") == "error":
            msg = result.get("message", "unknown error")
            logger.warning("MiniMax MCP vision error: %s", msg)
            return ""
        return result.get("text", "")

    def close(self) -> None:
        """Shut down the MCP client subprocess."""
        if self._client is not None:
            try:
                self._client.close()
            except Exception:
                pass
            self._client = None
