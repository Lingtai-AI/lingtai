# Music Generation as Standalone MusicGenService

**Date:** 2026-03-23
**Scope:** lingtai (services, capabilities, adapters)
**Status:** Draft

## Problem

The compose capability is hardcoded to MiniMax MCP (`mcp_media_client.py`). It calls `music_generation` on the MiniMax MCP server. There's no service abstraction.

The Gemini adapter has a `generate_music()` method but it's a stub that raises `NotImplementedError` (Lyria requires WebSocket streaming, not supported). This is noted for future implementation but is not wired up now.

## Package Structure

```
services/music_gen/
    __init__.py          # ABC, factory
    minimax.py           # MiniMaxMusicGenService
```

Note: Only one provider for now (MiniMax). Gemini's Lyria is not implementable yet. The service abstraction exists so that when Lyria or other providers become available, they can be added without touching the capability.

## `__init__.py` — ABC + Factory

Contains:

- `MusicGenService` ABC:

```python
class MusicGenService(ABC):
    @abstractmethod
    def generate(self, prompt: str, *, lyrics: str | None = None,
                 output_dir: Path | None = None, **kwargs) -> Path:
        """Generate music from a text prompt. Returns path to audio file."""
        ...
```

- Factory function: `create_music_gen_service(provider: str, **kwargs) -> MusicGenService`

## Provider Implementations

| Provider | Class | Needs `api_key` | Other params | How it works |
|----------|-------|-----------------|--------------|--------------|
| `minimax` | `MiniMaxMusicGenService` | Yes | `api_host` | MCP `music_generation` tool via `minimax-mcp` |

### MiniMax specifics

`MiniMaxMusicGenService` creates its own `MCPClient` for `minimax-mcp`. Replaces `_auto_create_mcp_client()` in `capabilities/compose.py`. Handles MCP response parsing, URL downloading, and file saving internally.

## Capability Changes (`capabilities/compose.py`)

### `setup()` signature

```python
def setup(
    agent: BaseAgent,
    provider: str | None = None,
    api_key: str | None = None,
    music_gen_service: MusicGenService | None = None,
    **kwargs,
) -> ComposeManager:
```

Resolution order:
1. `music_gen_service` passed directly → use it
2. `provider` passed → `create_music_gen_service(provider, api_key=api_key, **kwargs)`
3. Neither → `ValueError` at setup time

### `ComposeManager` simplification

The manager delegates to `self._music_gen_service.generate(prompt, lyrics=lyrics, output_dir=out_dir)`. All MCP client management, URL downloading, and file detection logic moves into the service.

## Adapter Cleanup

Remove from Gemini adapter:
- `generate_music()` method (stub — raises NotImplementedError)

No other adapter has a music generation method.

## Backward Compatibility

### Breaking behavioral change

`Agent(capabilities=["compose"])` currently auto-creates a MiniMax MCP client from env vars. After this change, it raises `ValueError`. Must specify `provider` and `api_key`.

## init.json Config

```json
"capabilities": {
    "compose": {"provider": "minimax", "api_key": "..."}
}
```

## Migration Checklist

1. Create `services/music_gen/` package with ABC, factory, and 1 provider
2. Update `capabilities/compose.py` to use `MusicGenService` exclusively
3. Remove `generate_music()` from Gemini adapter
4. Update tests
5. Smoke-test: `python -c "import lingtai"`
