# TTS as Standalone TTSService

**Date:** 2026-03-23
**Scope:** lingtai (services, capabilities, adapters)
**Status:** Draft

## Problem

The talk capability is hardcoded to MiniMax MCP (`mcp_media_client.py`). It creates a `MCPClient` subprocess for `minimax-mcp` and calls `text_to_audio`. There's no service abstraction — you can't swap in a different TTS provider without rewriting the capability.

Meanwhile, the Gemini adapter has a working `text_to_speech()` method that generates WAV audio natively via Gemini's speech models. This is unreachable from the talk capability.

## Design Principles

Same as web_search spec: adapters are for LLM calls only, explicit configuration, one interface one routing path.

## Package Structure

```
services/tts/
    __init__.py          # ABC, factory
    minimax.py           # MiniMaxTTSService
    gemini.py            # GeminiTTSService
```

## `__init__.py` — ABC + Factory

Contains:

- `TTSService` ABC:

```python
class TTSService(ABC):
    @abstractmethod
    def synthesize(self, text: str, *, voice: str | None = None,
                   output_dir: Path | None = None, **kwargs) -> Path:
        """Synthesize speech from text. Returns path to audio file."""
        ...
```

- Factory function: `create_tts_service(provider: str, **kwargs) -> TTSService`

The return type is `Path` (to the saved audio file) rather than raw bytes, because both current implementations (MiniMax MCP and Gemini) need to save files for the agent to reference. The service handles file creation.

## Provider Implementations

| Provider | Class | Needs `api_key` | Other params | How it works |
|----------|-------|-----------------|--------------|--------------|
| `minimax` | `MiniMaxTTSService` | Yes | `api_host`, `voice_id`, `emotion`, `speed` | MCP `text_to_audio` tool via `minimax-mcp` |
| `gemini` | `GeminiTTSService` | Yes | `model` (default: `gemini-2.5-pro-preview-tts`), `voice` (default: `Charon`) | `google.genai` speech generation, returns WAV |

### MiniMax specifics

`MiniMaxTTSService` creates its own `MCPClient` for `minimax-mcp` (the full media server, not coding-plan). This replaces the `_auto_create_mcp_client()` in `capabilities/talk.py`. The MCP client lifecycle is owned by the service.

### Gemini specifics

Logic moves from `GeminiAdapter.text_to_speech()`. The service creates a `google.genai.Client`, calls `generate_content` with `response_modalities=["AUDIO"]` and `SpeechConfig`, extracts PCM data, wraps in WAV header, writes to `output_dir`.

## Capability Changes (`capabilities/talk.py`)

### `setup()` signature

```python
def setup(
    agent: BaseAgent,
    provider: str | None = None,
    api_key: str | None = None,
    tts_service: TTSService | None = None,
    **kwargs,
) -> TalkManager:
```

Resolution order:
1. `tts_service` passed directly → use it
2. `provider` passed → `create_tts_service(provider, api_key=api_key, **kwargs)`
3. Neither → `ValueError` at setup time

### `TalkManager` simplification

The manager delegates to `self._tts_service.synthesize(text, voice=voice, output_dir=out_dir, ...)`. All MCP client management, URL downloading, and file detection logic moves into the service implementations. The manager just calls the service and returns the file path.

## Adapter Cleanup

Remove from Gemini adapter:
- `text_to_speech()` method

No other adapter has a TTS method.

## Backward Compatibility

### Breaking behavioral change

`Agent(capabilities=["talk"])` currently auto-creates a MiniMax MCP client from env vars. After this change, it raises `ValueError`. Must specify `provider` and `api_key`.

### Import paths

No existing public imports to break — `TTSService` is new.

## init.json Config

```json
"capabilities": {
    "talk": {"provider": "minimax", "api_key": "..."}
}
```

Or:

```json
"capabilities": {
    "talk": {"provider": "gemini", "api_key": "...", "voice": "Kore"}
}
```

## Migration Checklist

1. Create `services/tts/` package with ABC, factory, and 2 providers
2. Update `capabilities/talk.py` to use `TTSService` exclusively
3. Remove `text_to_speech()` from Gemini adapter
4. Update tests
5. Smoke-test: `python -c "import lingtai"`
