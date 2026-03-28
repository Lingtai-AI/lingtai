"""MiniMax web search — uses its own MCP client for the web_search tool."""
from __future__ import annotations

import atexit
import os
import shutil

from lingtai_kernel.logging import get_logger

from . import SearchResult, SearchService

logger = get_logger()


class MiniMaxSearchService(SearchService):
    """Web search via MiniMax's MCP web_search tool.

    Creates its own MCPClient for ``minimax-coding-plan-mcp``, isolated
    from the adapter's MCP singleton.

    Args:
        api_key: MiniMax API key.
    """

    # Class-level flag to ensure atexit is registered only once.
    _atexit_registered = False

    def __init__(self, api_key: str, api_host: str | None = None) -> None:
        self._api_key = api_key
        self._api_host = api_host
        self._client = None

    def _ensure_client(self):
        if self._client is not None and self._client.is_connected():
            return self._client
        from ...services.mcp import MCPClient

        uvx_path = shutil.which("uvx")
        if not uvx_path:
            raise RuntimeError(
                "uvx not found. Please install uv: "
                "https://docs.astral.sh/uv/getting-started/installation/"
            )
        if not self._api_host:
            raise RuntimeError(
                "api_host is required for MiniMax search service."
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
        if not MiniMaxSearchService._atexit_registered:
            atexit.register(self.close)
            MiniMaxSearchService._atexit_registered = True
        return self._client

    def close(self) -> None:
        """Shut down the MCP subprocess."""
        if self._client is not None:
            try:
                self._client.close()
            except Exception:
                pass
            self._client = None

    def search(self, query: str, max_results: int = 5) -> list[SearchResult]:
        try:
            client = self._ensure_client()
            result = client.call_tool("web_search", {"query": query})
        except Exception as e:
            logger.warning("MiniMax MCP web search failed: %s", e)
            return []

        if result.get("status") == "error":
            logger.warning("MiniMax MCP web search error: %s", result.get("message"))
            return []

        # Try to extract structured results
        text = result.get("text", "") or result.get("answer", "")
        if not text and "organic" in result:
            items: list[SearchResult] = []
            for item in result["organic"][:max_results]:
                items.append(
                    SearchResult(
                        title=item.get("title", ""),
                        url=item.get("link", ""),
                        snippet=item.get("snippet", ""),
                    )
                )
            return items

        if not text:
            text = str(result)

        return [
            SearchResult(
                title="MiniMax Web Search",
                url="",
                snippet=text,
            )
        ]
