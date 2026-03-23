"""TranscriptionService — abstract speech-to-text backing the listen capability.

Implementations:
- WhisperTranscriptionService — local faster-whisper (free, no API key).
- GeminiTranscriptionService — Gemini multimodal API.
"""
from __future__ import annotations

from abc import ABC, abstractmethod
from dataclasses import dataclass, field
from pathlib import Path


@dataclass
class TranscriptionResult:
    """Result of a transcription operation."""

    text: str
    language: str | None = None
    language_probability: float | None = None
    duration: float | None = None
    segments: list[dict] | None = None


class TranscriptionService(ABC):
    """Abstract transcription service.

    Backs the listen capability's ``transcribe`` action. Implementations
    provide speech-to-text via local models, cloud APIs, or other backends.
    """

    @abstractmethod
    def transcribe(self, audio_path: Path, **kwargs) -> TranscriptionResult:
        """Transcribe an audio file to text.

        Args:
            audio_path: Path to the audio file.
            **kwargs: Provider-specific options.

        Returns:
            TranscriptionResult with text and optional metadata.
        """
        ...


def create_transcription_service(
    provider: str = "whisper",
    **kwargs,
) -> TranscriptionService:
    """Factory — create a TranscriptionService by provider name.

    Args:
        provider: ``"whisper"`` (default, local) or ``"gemini"`` (cloud).
        **kwargs: Forwarded to the chosen service constructor.

    Returns:
        A ready-to-use TranscriptionService instance.
    """
    if provider == "whisper":
        from .whisper import WhisperTranscriptionService
        return WhisperTranscriptionService(**kwargs)
    elif provider == "gemini":
        from .gemini import GeminiTranscriptionService
        return GeminiTranscriptionService(**kwargs)
    else:
        raise ValueError(
            f"Unknown transcription provider: {provider!r}. "
            f"Supported: 'whisper', 'gemini'."
        )
