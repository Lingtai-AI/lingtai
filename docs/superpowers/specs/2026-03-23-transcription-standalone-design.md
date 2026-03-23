# Transcription as Standalone TranscriptionService

**Date:** 2026-03-23
**Scope:** lingtai (services, capabilities, adapters)
**Status:** Draft

## Problem

The listen capability has two actions: `transcribe` (faster-whisper, local) and `appreciate` (librosa, local). The transcription path is hardcoded to local faster-whisper with no service abstraction.

Meanwhile, the Gemini adapter has a working `transcribe()` method that does speech-to-text via multimodal understanding. This is unreachable from the listen capability.

## Design Note

The listen capability has two distinct functions — transcription and music appreciation. These are fundamentally different services. This spec extracts **transcription** into a service. Music appreciation (librosa analysis) stays in the capability as a local-only feature — it's pure signal processing, not an API-backed service.

## Package Structure

```
services/transcription/
    __init__.py          # ABC, factory
    whisper.py           # WhisperTranscriptionService (local)
    gemini.py            # GeminiTranscriptionService
```

## `__init__.py` — ABC + Factory

Contains:

- `TranscriptionResult` dataclass:

```python
@dataclass
class TranscriptionResult:
    text: str
    language: str | None = None
    language_probability: float | None = None
    duration: float | None = None
    segments: list[dict] | None = None
```

- `TranscriptionService` ABC:

```python
class TranscriptionService(ABC):
    @abstractmethod
    def transcribe(self, audio_path: str | Path, **kwargs) -> TranscriptionResult:
        """Transcribe audio to text."""
        ...
```

- Factory function: `create_transcription_service(provider: str, **kwargs) -> TranscriptionService`

## Provider Implementations

| Provider | Class | Needs `api_key` | Other params | How it works |
|----------|-------|-----------------|--------------|--------------|
| `whisper` | `WhisperTranscriptionService` | No | `model_size` (default: `base`), `device` (default: `cpu`) | Local `faster_whisper` inference |
| `gemini` | `GeminiTranscriptionService` | Yes | `model` (default from `capability_models.transcribe`) | `google.genai` multimodal with audio bytes |

### Whisper specifics

Logic moves from `ListenManager._transcribe()` and `_get_whisper_model()`. The service owns the model lifecycle (lazy loading, caching). Returns full `TranscriptionResult` with segments, language, duration.

### Gemini specifics

Logic moves from `GeminiAdapter.transcribe()`. The service creates a `google.genai.Client`, sends audio bytes as `Part.from_bytes` with a transcription prompt. Returns `TranscriptionResult` with `text` populated (Gemini doesn't return segments or language metadata natively).

## Capability Changes (`capabilities/listen.py`)

### `setup()` signature

```python
def setup(
    agent: BaseAgent,
    provider: str | None = None,
    api_key: str | None = None,
    transcription_service: TranscriptionService | None = None,
    **kwargs,
) -> ListenManager:
```

Resolution order for transcription:
1. `transcription_service` passed directly → use it
2. `provider` passed → `create_transcription_service(provider, api_key=api_key, **kwargs)`
3. Neither → default to `whisper` (local, no API key needed — this is the one exception to the "no fallback" rule, since whisper is local and free)

### `ListenManager` changes

- `_transcribe()` delegates to `self._transcription_service.transcribe(path)`
- `_appreciate()` stays as-is (local librosa, no service needed)
- `_get_whisper_model()` removed from manager — moves into `WhisperTranscriptionService`

## Adapter Cleanup

Remove from Gemini adapter:
- `transcribe()` method

No other adapter has a transcription method.

## Backward Compatibility

### Non-breaking for default case

`Agent(capabilities=["listen"])` continues to work — defaults to local whisper. This is intentional: whisper is local and free, so a default makes sense unlike API-backed services.

`Agent(capabilities={"listen": {"provider": "gemini", "api_key": "..."}})` enables Gemini-backed transcription.

## init.json Config

```json
// Local whisper (default, no key needed)
"capabilities": {
    "listen": {"provider": "whisper"}
}

// Gemini-backed
"capabilities": {
    "listen": {"provider": "gemini", "api_key": "..."}
}
```

## Migration Checklist

1. Create `services/transcription/` package with ABC, factory, and 2 providers
2. Update `capabilities/listen.py` to use `TranscriptionService` for the `transcribe` action
3. Remove `transcribe()` from Gemini adapter
4. Update tests
5. Smoke-test: `python -c "import lingtai"`
