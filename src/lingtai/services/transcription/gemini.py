"""GeminiTranscriptionService — speech-to-text via Gemini multimodal API.

Requires ``google-genai`` SDK (``pip install google-genai``).
"""
from __future__ import annotations

from pathlib import Path

from . import TranscriptionResult, TranscriptionService


class GeminiTranscriptionService(TranscriptionService):
    """Cloud transcription using Gemini's multimodal understanding.

    Creates its own ``google.genai.Client`` — independent of the LLM adapter.

    Args:
        api_key: Gemini API key. If not provided, reads from
            ``GEMINI_API_KEY`` environment variable.
        model: Model name (default ``"gemini-3-flash-preview"``).
    """

    def __init__(
        self,
        *,
        api_key: str | None = None,
        model: str = "gemini-3-flash-preview",
    ) -> None:
        self._api_key = api_key
        self._model = model
        self._client = None

    def transcribe(self, audio_path: Path, **kwargs) -> TranscriptionResult:
        """Transcribe audio using Gemini multimodal API.

        Args:
            audio_path: Path to the audio file.
            **kwargs: Additional options (``mime_type`` override, etc.).

        Returns:
            TranscriptionResult with text (no segment-level detail from Gemini).
        """
        client = self._ensure_client()

        mime_type = kwargs.pop("mime_type", None)
        if mime_type is None:
            mime_type = _guess_mime_type(audio_path)

        audio_bytes = Path(audio_path).read_bytes()

        from google.genai import types

        contents = [
            types.Part.from_bytes(data=audio_bytes, mime_type=mime_type),
            "Transcribe this audio verbatim. Return only the exact transcription text, nothing else.",
        ]
        raw = client.models.generate_content(
            model=self._model,
            contents=contents,
        )
        text = raw.text or ""

        return TranscriptionResult(text=text.strip())

    def _ensure_client(self):
        """Lazy-create the genai Client."""
        if self._client is None:
            import os

            from google import genai

            api_key = self._api_key or os.environ.get("GEMINI_API_KEY")
            if not api_key:
                raise ValueError(
                    "GeminiTranscriptionService requires a Gemini API key. "
                    "Pass api_key= or set GEMINI_API_KEY environment variable."
                )
            self._client = genai.Client(api_key=api_key)
        return self._client


def _guess_mime_type(path: Path) -> str:
    """Guess audio MIME type from file extension."""
    suffix = Path(path).suffix.lower()
    mime_map = {
        ".wav": "audio/wav",
        ".mp3": "audio/mpeg",
        ".flac": "audio/flac",
        ".ogg": "audio/ogg",
        ".m4a": "audio/mp4",
        ".aac": "audio/aac",
        ".webm": "audio/webm",
        ".opus": "audio/opus",
    }
    return mime_map.get(suffix, "audio/wav")
