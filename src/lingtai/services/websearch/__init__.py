"""SearchService — abstract web search and provider implementations.

Providers:
- DuckDuckGoSearchService — zero-API-key search via ddgs package.
- AnthropicSearchService — Anthropic native web search tool.
- OpenAISearchService — OpenAI search-preview model.
- GeminiSearchService — Gemini Google Search grounding.
- MiniMaxSearchService — MiniMax MCP web_search tool.

Factory:
    create_search_service(provider, api_key) — instantiate by provider name.
"""
from __future__ import annotations

from abc import ABC, abstractmethod
from dataclasses import dataclass


@dataclass
class SearchResult:
    """A single search result."""
    title: str
    url: str
    snippet: str


class SearchService(ABC):
    """Abstract web search service.

    Backs the web_search capability. Implementations provide search
    via LLM grounding, dedicated search APIs, or other backends.
    """

    @abstractmethod
    def search(self, query: str, max_results: int = 5) -> list[SearchResult]:
        """Search the web and return results.

        Args:
            query: Search query string.
            max_results: Maximum number of results to return.

        Returns:
            List of search results.
        """
        ...


def create_search_service(
    provider: str,
    *,
    api_key: str | None = None,
    model: str | None = None,
    api_host: str | None = None,
) -> SearchService:
    """Factory — create a SearchService for the given provider.

    Args:
        provider: Provider name (``"duckduckgo"``, ``"anthropic"``,
                  ``"openai"``, ``"gemini"``, ``"minimax"``).
        api_key: API key for the provider (required for all except duckduckgo).
        model: Optional model override.

    Returns:
        A configured SearchService instance.

    Raises:
        ValueError: If *provider* is not recognized.
        RuntimeError: If *api_key* is required but missing or empty.
    """
    name = provider.lower()

    def _require_key() -> str:
        if not api_key:
            raise RuntimeError(
                f"Search provider {provider!r} requires an api_key."
            )
        return api_key

    if name == "duckduckgo":
        from .duckduckgo import DuckDuckGoSearchService
        return DuckDuckGoSearchService()

    if name == "anthropic":
        from .anthropic import AnthropicSearchService
        kwargs: dict = {"api_key": _require_key()}
        if model:
            kwargs["model"] = model
        return AnthropicSearchService(**kwargs)

    if name == "openai":
        from .openai import OpenAISearchService
        kwargs = {"api_key": _require_key()}
        if model:
            kwargs["model"] = model
        return OpenAISearchService(**kwargs)

    if name == "gemini":
        from .gemini import GeminiSearchService
        kwargs = {"api_key": _require_key()}
        if model:
            kwargs["model"] = model
        return GeminiSearchService(**kwargs)

    if name == "minimax":
        from .minimax import MiniMaxSearchService
        return MiniMaxSearchService(api_key=_require_key(), api_host=api_host)

    raise ValueError(
        f"Unknown web search provider: {provider!r}. "
        f"Supported: duckduckgo, anthropic, openai, gemini, minimax."
    )
