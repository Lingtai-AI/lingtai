"""Anthropic vision service — standalone image analysis via Claude's multimodal API."""
from __future__ import annotations

import base64

from . import VisionService, _read_image


class AnthropicVisionService(VisionService):
    """Image understanding via Anthropic's multimodal Messages API.

    Owns its own ``anthropic.Anthropic`` client and API key — fully
    independent of any LLM adapter or agent.
    """

    def __init__(
        self,
        *,
        api_key: str,
        model: str = "claude-sonnet-4-20250514",
        max_tokens: int = 1024,
    ) -> None:
        import anthropic as _anthropic

        self._client = _anthropic.Anthropic(api_key=api_key)
        self._model = model
        self._max_tokens = max_tokens

    def analyze_image(self, image_path: str, prompt: str | None = None) -> str:
        """Analyze an image using Claude's vision capabilities."""
        image_bytes, mime_type = _read_image(image_path)
        question = prompt or "Describe this image."

        b64 = base64.b64encode(image_bytes).decode("utf-8")
        messages = [
            {
                "role": "user",
                "content": [
                    {
                        "type": "image",
                        "source": {
                            "type": "base64",
                            "media_type": mime_type,
                            "data": b64,
                        },
                    },
                    {"type": "text", "text": question},
                ],
            }
        ]
        raw = self._client.messages.create(
            model=self._model,
            max_tokens=self._max_tokens,
            messages=messages,
        )
        # Extract text from response blocks
        text_parts = []
        for block in raw.content:
            if block.type == "text":
                text_parts.append(block.text)
        return "\n".join(text_parts)
