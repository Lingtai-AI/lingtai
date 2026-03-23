"""MusicGenService — abstract music generation backing the compose capability.

First implementation: MiniMaxMusicGenService (wraps minimax-mcp media server).
"""
from __future__ import annotations

from abc import ABC, abstractmethod
from pathlib import Path
from typing import Any


class MusicGenService(ABC):
    """Abstract music generation service.

    Backs the compose capability.  Implementations provide music generation
    via MCP servers, REST APIs, or other backends.
    """

    @abstractmethod
    def generate(
        self,
        prompt: str,
        *,
        lyrics: str | None = None,
        output_dir: Path | None = None,
        **kwargs: Any,
    ) -> Path:
        """Generate music from a text prompt.

        Args:
            prompt: Description of the desired music (style, mood, instruments, etc.).
            lyrics: Optional lyrics for vocal tracks.
            output_dir: Directory where the output file should be saved.
                If *None*, the implementation chooses a default location.
            **kwargs: Provider-specific options.

        Returns:
            Path to the generated audio file.

        Raises:
            RuntimeError: If generation fails.
        """
        ...

    def close(self) -> None:
        """Release any resources held by this service.

        Subclasses should override if they hold connections, subprocesses, etc.
        """


def create_music_gen_service(
    provider: str = "minimax",
    *,
    api_key: str | None = None,
    **kwargs: Any,
) -> MusicGenService:
    """Factory — create a MusicGenService for the given provider.

    Args:
        provider: Provider name.  Currently only ``"minimax"`` is supported.
        api_key: API key passed through to the provider implementation.
        **kwargs: Extra provider-specific options.

    Returns:
        A ready-to-use MusicGenService instance.

    Raises:
        ValueError: If the provider is not recognised.
    """
    if provider == "minimax":
        from .minimax import MiniMaxMusicGenService

        return MiniMaxMusicGenService(api_key=api_key, **kwargs)

    raise ValueError(
        f"Unknown music generation provider: {provider!r}.  "
        f"Supported: 'minimax'."
    )
