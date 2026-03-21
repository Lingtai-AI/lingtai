"""SearchService — abstract web search backing the web_search capability.

Implementations:
- DuckDuckGoSearchService — zero-API-key search via ddgs package.
- LLMSearchService — wraps LLM grounding/search capabilities (stub).
"""
from __future__ import annotations

from abc import ABC, abstractmethod
from dataclasses import dataclass
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from lingtai_kernel.llm.service import LLMService


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


class DuckDuckGoSearchService(SearchService):
    """Zero-API-key web search via DuckDuckGo.

    Uses the ``ddgs`` package to scrape DuckDuckGo results.
    Install with ``pip install lingtai[duckduckgo]`` or
    ``pip install ddgs``.
    """

    def search(self, query: str, max_results: int = 5) -> list[SearchResult]:
        try:
            from ddgs import DDGS  # type: ignore[import-untyped]
        except ImportError:
            raise ImportError(
                "ddgs is required for DuckDuckGoSearchService. "
                "Install it with: pip install lingtai[duckduckgo]"
            )
        raw = DDGS().text(query, max_results=max_results)
        return [
            SearchResult(
                title=r.get("title", ""),
                url=r.get("href", ""),
                snippet=r.get("body", ""),
            )
            for r in raw
        ]


class LLMSearchService(SearchService):
    """Uses LLM grounding/search for web search.

    This is the first implementation — delegates to the LLMService's
    built-in search/grounding capabilities (e.g., Gemini's google_search tool).
    """

    def __init__(self, llm: LLMService):
        self._llm = llm

    def search(self, query: str, max_results: int = 5) -> list[SearchResult]:
        # TODO: implement using LLMService grounding API
        raise NotImplementedError(
            "LLMSearchService.search requires LLM grounding support — "
            "wire through ChatSession when ready"
        )
