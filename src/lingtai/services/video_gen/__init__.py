"""VideoGenService — abstract video generation backing the video capability.

First implementation: MiniMaxVideoGenService (wraps minimax-mcp media server).
"""
from __future__ import annotations

from abc import ABC, abstractmethod
from pathlib import Path
from typing import Any


class VideoGenService(ABC):
    """Abstract video generation service.

    Backs the video capability.  Implementations provide text-to-video
    and image-to-video generation via MCP servers, REST APIs, or other backends.
    """

    @abstractmethod
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
        """Generate a video from a text prompt.

        Args:
            prompt: Description of the video to generate.  May include camera
                movement instructions for Director models.
            first_frame_image: Path to an image to use as the first frame
                (image-to-video mode).  When provided the implementation should
                select an I2V model automatically.
            model: Explicit model override.  If *None* the implementation
                picks a sensible default.
            duration: Video duration in seconds (provider-specific).
            resolution: Output resolution (e.g. ``"1080P"``).
            output_dir: Directory where the output file should be saved.
                If *None*, the implementation chooses a default location.
            **kwargs: Provider-specific options.

        Returns:
            Path to the generated video file.

        Raises:
            RuntimeError: If generation fails.
        """
        ...

    def close(self) -> None:
        """Release any resources held by this service.

        Subclasses should override if they hold connections, subprocesses, etc.
        """


def create_video_gen_service(
    provider: str = "minimax",
    *,
    api_key: str | None = None,
    **kwargs: Any,
) -> VideoGenService:
    """Factory — create a VideoGenService for the given provider.

    Args:
        provider: Provider name.  Currently only ``"minimax"`` is supported.
        api_key: API key passed through to the provider implementation.
        **kwargs: Extra provider-specific options.

    Returns:
        A ready-to-use VideoGenService instance.

    Raises:
        ValueError: If the provider is not recognised.
    """
    if provider == "minimax":
        from .minimax import MiniMaxVideoGenService

        return MiniMaxVideoGenService(api_key=api_key, **kwargs)

    raise ValueError(
        f"Unknown video generation provider: {provider!r}.  "
        f"Supported: 'minimax'."
    )
