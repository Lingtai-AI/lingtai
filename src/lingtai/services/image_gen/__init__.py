"""ImageGenService — abstract image generation backing the draw capability.

Implementations:
- MiniMaxImageGenService — text-to-image via MiniMax MCP server.
- GeminiImageGenService — text-to-image via Gemini's native generation.
"""
from __future__ import annotations

from abc import ABC, abstractmethod
from pathlib import Path
from typing import Any


class ImageGenService(ABC):
    """Abstract image generation service.

    Backs the draw capability. Implementations provide text-to-image
    generation via MiniMax MCP, Gemini native, or other backends.
    """

    @abstractmethod
    def generate(
        self,
        prompt: str,
        *,
        aspect_ratio: str | None = None,
        output_dir: Path | None = None,
        **kwargs: Any,
    ) -> Path:
        """Generate an image from a text prompt.

        Args:
            prompt: Text description of the image to generate.
            aspect_ratio: Optional aspect ratio (e.g. "16:9", "1:1").
            output_dir: Directory to save the generated image. If None,
                the implementation chooses a default location.
            **kwargs: Provider-specific options.

        Returns:
            Path to the generated image file.

        Raises:
            RuntimeError: If generation fails.
        """
        ...


def create_image_gen_service(
    provider: str,
    *,
    api_key: str | None = None,
    api_host: str | None = None,
    **kwargs: Any,
) -> ImageGenService:
    """Factory — create an ImageGenService by provider name.

    Args:
        provider: Provider name. Supported: "minimax", "gemini".
        api_key: API key for the provider. Falls back to env vars.
        api_host: API host URL (provider-specific).
        **kwargs: Additional provider-specific options.

    Returns:
        An ImageGenService instance.

    Raises:
        ValueError: If the provider is unknown.
    """
    if provider == "minimax":
        from .minimax import MiniMaxImageGenService
        return MiniMaxImageGenService(api_key=api_key, api_host=api_host, **kwargs)
    elif provider == "gemini":
        from .gemini import GeminiImageGenService
        return GeminiImageGenService(api_key=api_key, **kwargs)
    else:
        raise ValueError(
            f"Unknown image generation provider: {provider!r}. "
            f"Supported: 'minimax', 'gemini'"
        )
