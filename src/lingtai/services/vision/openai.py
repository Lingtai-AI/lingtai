"""OpenAI vision service — standalone image analysis via OpenAI's multimodal API."""
from __future__ import annotations

import base64

from . import VisionService, _read_image


class OpenAIVisionService(VisionService):
    """Image understanding via OpenAI's chat completions with vision.

    Owns its own ``openai.OpenAI`` client and API key — fully
    independent of any LLM adapter or agent.
    """

    def __init__(
        self,
        *,
        api_key: str,
        model: str = "gpt-4o",
        base_url: str | None = None,
        max_tokens: int = 1024,
    ) -> None:
        import openai as _openai

        kwargs: dict = {"api_key": api_key}
        if base_url:
            kwargs["base_url"] = base_url
        self._client = _openai.OpenAI(**kwargs)
        self._model = model
        self._max_tokens = max_tokens

    def analyze_image(self, image_path: str, prompt: str | None = None) -> str:
        """Analyze an image using OpenAI's vision capabilities."""
        image_bytes, mime_type = _read_image(image_path)
        question = prompt or "Describe this image."

        b64 = base64.b64encode(image_bytes).decode("utf-8")
        data_url = f"data:{mime_type};base64,{b64}"
        messages = [
            {
                "role": "user",
                "content": [
                    {"type": "image_url", "image_url": {"url": data_url}},
                    {"type": "text", "text": question},
                ],
            }
        ]
        raw = self._client.chat.completions.create(
            model=self._model,
            messages=messages,
            max_tokens=self._max_tokens,
        )
        if raw.choices:
            return raw.choices[0].message.content or ""
        return ""
