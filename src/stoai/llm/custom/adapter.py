"""Custom adapter — generic OpenAI or Anthropic-compatible provider.

For OpenRouter, SiliconFlow, Ollama, vLLM, or any OpenAI/Anthropic-compatible service.
Configuration: base_url, api_key, api_compat ("openai" or "anthropic").
"""
from ..base import LLMAdapter

from .defaults import DEFAULTS  # noqa: F401 — re-exported for consumers


def create_custom_adapter(
    api_key: str | None = None,
    api_compat: str = "openai",
    base_url: str | None = None,
    **kwargs,
) -> LLMAdapter:
    """Factory: creates an OpenAI or Anthropic-based adapter."""
    if not base_url:
        raise ValueError("Custom provider requires a base_url")

    if api_compat == "anthropic":
        from ..anthropic.adapter import AnthropicAdapter
        return AnthropicAdapter(api_key=api_key, base_url=base_url, **kwargs)
    else:
        from ..openai.adapter import OpenAIAdapter
        return OpenAIAdapter(api_key=api_key, base_url=base_url, **kwargs)
