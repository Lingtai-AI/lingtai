# Vision as Standalone VisionService

**Date:** 2026-03-23
**Scope:** lingtai (services, capabilities, adapters)
**Status:** Draft

## Problem

The vision capability uses the same two-tier pattern as web_search: `VisionService` first (if provided), then `adapter.generate_vision()` fallback. The `VisionService` ABC exists but its only implementation (`LLMVisionService`) is a stub that raises `NotImplementedError`. In practice, vision always falls back to the adapter.

Four adapters implement `generate_vision()`: Anthropic (base64 + messages API), OpenAI (base64 + data URL), Gemini (multimodal parts), MiniMax (MCP `understand_image` tool). Each needs its own API key, but the current design resolves keys through `LLMService.get_adapter()`.

## Design Principles

Same as web_search spec: adapters are for LLM calls only, explicit configuration, one interface one routing path.

## Package Structure

```
services/vision/
    __init__.py          # ABC, factory
    anthropic.py         # AnthropicVisionService
    openai.py            # OpenAIVisionService
    gemini.py            # GeminiVisionService
    minimax.py           # MiniMaxVisionService
```

Old `services/vision.py` is deleted. Everything moves to `services/vision/`.

## `__init__.py` — ABC + Factory

Contains:

- `VisionService` ABC (moved from `services/vision.py`): `analyze_image(image_path, prompt=None) -> str`
- Provider registry mapping provider name to module path
- Factory function:

```python
def create_vision_service(provider: str, **kwargs) -> VisionService:
    """Create a VisionService by provider name.

    Lazy-imports the implementation class. Passes api_key and
    any other kwargs to the constructor.

    Raises ValueError for unknown provider.
    Raises RuntimeError if provider needs api_key and it's missing.
    """
```

Public API: `from lingtai.services.vision import VisionService, create_vision_service`

## Provider Implementations

Each file exports a single class. Constructor takes `api_key: str | None = None` plus provider-specific kwargs. Validates key requirement at `__init__` time. Implements `analyze_image(image_path, prompt=None) -> str`.

| Provider | Class | Needs `api_key` | Other params | How it works |
|----------|-------|-----------------|--------------|--------------|
| `anthropic` | `AnthropicVisionService` | Yes | `model` (default: current anthropic model) | base64 image + messages API |
| `openai` | `OpenAIVisionService` | Yes | `model` (default: current openai model) | base64 data URL + chat completions |
| `gemini` | `GeminiVisionService` | Yes | `model` (default from `capability_models.vision`) | multimodal `Part.from_bytes` |
| `minimax` | `MiniMaxVisionService` | Yes | — | MCP `understand_image` tool via coding-plan server |

The logic currently inside each adapter's `generate_vision()` moves into these classes. Each service handles its own file reading (path → bytes), base64 encoding, and SDK calls.

### MiniMax specifics

`MiniMaxVisionService` manages its own MCP client lifecycle (same as `MiniMaxSearchService` in the web_search spec). Creates its own `MCPClient` for `minimax-coding-plan-mcp` with the provided `api_key`.

## Capability Changes (`capabilities/vision.py`)

### `setup()` signature

```python
def setup(
    agent: BaseAgent,
    provider: str | None = None,
    api_key: str | None = None,
    vision_service: VisionService | None = None,
    **kwargs,
) -> VisionManager:
```

Resolution order:
1. `vision_service` passed directly → use it (programmatic API for custom implementations)
2. `provider` passed → `create_vision_service(provider, api_key=api_key, **kwargs)`
3. Neither → `ValueError` at setup time

### `VisionManager` simplification

No more adapter fallback path. The manager delegates to `self._vision_service.analyze_image(path, prompt)`. File validation (exists, extension → mime type) stays in the manager — the service receives a path.

## Adapter Cleanup

Remove from all 4 adapters (Anthropic, OpenAI, Gemini, MiniMax):
- `generate_vision()` method

The Custom adapter inherits from its delegate — no changes needed.

Remove from Gemini adapter:
- `generate_multimodal()` — only used by `generate_vision()`. If needed later, it can be re-added as a utility.

## Backward Compatibility

### Import paths

`from lingtai.services.vision import VisionService, LLMVisionService` → broken. New path: `from lingtai.services.vision import VisionService, create_vision_service`.

Affected files (exhaustive):
- `src/lingtai/__init__.py` — re-exports `VisionService`, `LLMVisionService`. Update path, remove `LLMVisionService`, add `create_vision_service`.
- `src/lingtai/capabilities/vision.py` — currently uses `Any` for vision_service type. Will import `VisionService` and `create_vision_service`.

### Breaking behavioral change

`Agent(capabilities=["vision"])` without any config currently falls back to the agent's LLM provider. After this change, it raises `ValueError` at setup time. Explicit configuration required.

## init.json Config

```json
"capabilities": {
    "vision": {"provider": "gemini", "api_key": "..."}
}
```

Validation happens at `setup()` time (same pattern as web_search).

## Migration Checklist

1. Create `services/vision/` package with ABC, factory, and all 4 providers
2. Update `capabilities/vision.py` to use `VisionService` exclusively
3. Remove `generate_vision()` from all 4 adapters
4. Remove `generate_multimodal()` from Gemini adapter
5. Delete `services/vision.py`
6. Update `__init__.py` public API (remove `LLMVisionService`, add `create_vision_service`)
7. Update tests
8. Smoke-test: `python -c "import lingtai"`
