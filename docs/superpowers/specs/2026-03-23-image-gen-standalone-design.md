# Image Generation as Standalone ImageGenService

**Date:** 2026-03-23
**Scope:** lingtai (services, capabilities, adapters)
**Status:** Draft

## Problem

The draw capability is hardcoded to MiniMax MCP (`mcp_media_client.py`). It calls `text_to_image` on the MiniMax MCP server. There's no service abstraction.

Meanwhile, the Gemini adapter has a working `generate_image()` method that generates images natively via `response_modalities=["IMAGE"]`. This is unreachable from the draw capability.

## Package Structure

```
services/image_gen/
    __init__.py          # ABC, factory
    minimax.py           # MiniMaxImageGenService
    gemini.py            # GeminiImageGenService
```

## `__init__.py` — ABC + Factory

Contains:

- `ImageGenService` ABC:

```python
class ImageGenService(ABC):
    @abstractmethod
    def generate(self, prompt: str, *, aspect_ratio: str | None = None,
                 output_dir: Path | None = None, **kwargs) -> Path:
        """Generate an image from a text prompt. Returns path to image file."""
        ...
```

- Factory function: `create_image_gen_service(provider: str, **kwargs) -> ImageGenService`

## Provider Implementations

| Provider | Class | Needs `api_key` | Other params | How it works |
|----------|-------|-----------------|--------------|--------------|
| `minimax` | `MiniMaxImageGenService` | Yes | `api_host` | MCP `text_to_image` tool via `minimax-mcp` |
| `gemini` | `GeminiImageGenService` | Yes | `model` (default: `gemini-3.1-flash-image-preview`) | `google.genai` with `response_modalities=["IMAGE"]`, returns inline bytes |

### MiniMax specifics

`MiniMaxImageGenService` creates its own `MCPClient` for `minimax-mcp`. Replaces `_auto_create_mcp_client()` in `capabilities/draw.py`. Handles MCP response parsing, URL downloading, and file saving internally.

### Gemini specifics

Logic moves from `GeminiAdapter.generate_image()`. The service creates a `google.genai.Client`, calls `generate_content` with `response_modalities=["IMAGE"]`, extracts `inline_data` bytes from the response, saves to `output_dir`.

## Capability Changes (`capabilities/draw.py`)

### `setup()` signature

```python
def setup(
    agent: BaseAgent,
    provider: str | None = None,
    api_key: str | None = None,
    image_gen_service: ImageGenService | None = None,
    **kwargs,
) -> DrawManager:
```

Resolution order:
1. `image_gen_service` passed directly → use it
2. `provider` passed → `create_image_gen_service(provider, api_key=api_key, **kwargs)`
3. Neither → `ValueError` at setup time

### `DrawManager` simplification

The manager delegates to `self._image_gen_service.generate(prompt, aspect_ratio=..., output_dir=out_dir)`. All MCP client management, URL downloading, and file detection logic moves into service implementations.

## Adapter Cleanup

Remove from Gemini adapter:
- `generate_image()` method

No other adapter has an image generation method.

## Backward Compatibility

### Breaking behavioral change

`Agent(capabilities=["draw"])` currently auto-creates a MiniMax MCP client from env vars. After this change, it raises `ValueError`. Must specify `provider` and `api_key`.

## init.json Config

```json
"capabilities": {
    "draw": {"provider": "minimax", "api_key": "..."}
}
```

Or:

```json
"capabilities": {
    "draw": {"provider": "gemini", "api_key": "..."}
}
```

## Migration Checklist

1. Create `services/image_gen/` package with ABC, factory, and 2 providers
2. Update `capabilities/draw.py` to use `ImageGenService` exclusively
3. Remove `generate_image()` from Gemini adapter
4. Update tests
5. Smoke-test: `python -c "import lingtai"`
