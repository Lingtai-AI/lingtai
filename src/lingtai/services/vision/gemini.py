"""Gemini vision service — standalone image analysis via Google's genai SDK."""
from __future__ import annotations

from . import VisionService, _read_image


class GeminiVisionService(VisionService):
    """Image understanding via Gemini's multimodal content generation.

    Owns its own ``genai.Client`` and API key — fully independent of
    any LLM adapter or agent.
    """

    def __init__(
        self,
        *,
        api_key: str,
        model: str = "gemini-2.5-flash",
    ) -> None:
        from google import genai
        from google.genai import types

        self._client = genai.Client(
            api_key=api_key,
            http_options=types.HttpOptions(timeout=300_000),
        )
        self._model = model

    def analyze_image(self, image_path: str, prompt: str | None = None) -> str:
        """Analyze an image using Gemini's vision capabilities."""
        from google.genai import types

        image_bytes, mime_type = _read_image(image_path)
        question = prompt or "Describe this image."

        contents = [
            types.Part.from_bytes(data=image_bytes, mime_type=mime_type),
            types.Part.from_text(text=question),
        ]
        raw = self._client.models.generate_content(
            model=self._model,
            contents=contents,
        )
        # Extract text from response
        candidates = getattr(raw, "candidates", None) or []
        text_parts = []
        if candidates:
            content = candidates[0].content
            if content and content.parts:
                for part in content.parts:
                    if hasattr(part, "text") and part.text and not getattr(part, "thought", False):
                        text_parts.append(part.text)
        return "\n".join(text_parts)
