"""TTSService — abstract text-to-speech service backing the talk capability.

Implementations:
- MiniMaxTTSService — TTS via MiniMax MCP (minimax-mcp server).
- GeminiTTSService — TTS via Gemini's native speech generation models.
"""
from __future__ import annotations

from abc import ABC, abstractmethod
from pathlib import Path


class TTSService(ABC):
    """Abstract text-to-speech service.

    Backs the talk capability.  Implementations synthesize speech from text
    and save the result to an audio file.
    """

    @abstractmethod
    def synthesize(
        self,
        text: str,
        *,
        voice: str | None = None,
        output_dir: Path | None = None,
        **kwargs: object,
    ) -> Path:
        """Synthesize speech from text.

        Args:
            text: The text to convert to speech.
            voice: Optional voice identifier (provider-specific).
            output_dir: Directory to save the audio file.  If ``None``,
                the implementation chooses a default location.
            **kwargs: Provider-specific parameters (e.g. ``emotion``,
                ``speed`` for MiniMax).

        Returns:
            Path to the saved audio file.
        """
        ...


def create_tts_service(provider: str, **kwargs: object) -> TTSService:
    """Factory — create a TTSService for the given provider.

    Args:
        provider: ``"minimax"`` or ``"gemini"``.
        **kwargs: Forwarded to the provider constructor.

    Returns:
        A ready-to-use TTSService instance.

    Raises:
        ValueError: If the provider is unknown.
    """
    if provider == "minimax":
        from .minimax import MiniMaxTTSService

        return MiniMaxTTSService(**kwargs)  # type: ignore[arg-type]
    if provider == "gemini":
        from .gemini import GeminiTTSService

        return GeminiTTSService(**kwargs)  # type: ignore[arg-type]
    raise ValueError(
        f"Unknown TTS provider: {provider!r}. Supported: 'minimax', 'gemini'."
    )
