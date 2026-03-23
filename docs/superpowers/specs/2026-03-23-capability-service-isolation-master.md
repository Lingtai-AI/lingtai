# Capability Service Isolation — Master Plan

**Date:** 2026-03-23
**Scope:** lingtai (services, capabilities, adapters)
**Status:** Draft

## Principle

**Adapters are for LLM calls only.** Every non-chat capability (search, vision, TTS, image generation, transcription, music generation) becomes a standalone service with its own `provider` + `api_key` configuration. No capability routes through adapter methods. No silent fallback to the agent's LLM provider.

## The 6 Specs

Each spec is self-contained with its own migration checklist. They can be implemented in parallel by independent agents.

| # | Spec | Service | Package | Providers |
|---|------|---------|---------|-----------|
| 1 | [websearch-standalone](2026-03-23-websearch-standalone-design.md) | `SearchService` | `services/websearch/` | duckduckgo, minimax, anthropic, openai, gemini |
| 2 | [vision-standalone](2026-03-23-vision-standalone-design.md) | `VisionService` | `services/vision/` | anthropic, openai, gemini, minimax |
| 3 | [tts-standalone](2026-03-23-tts-standalone-design.md) | `TTSService` | `services/tts/` | minimax, gemini |
| 4 | [image-gen-standalone](2026-03-23-image-gen-standalone-design.md) | `ImageGenService` | `services/image_gen/` | minimax, gemini |
| 5 | [transcription-standalone](2026-03-23-transcription-standalone-design.md) | `TranscriptionService` | `services/transcription/` | whisper (local), gemini |
| 6 | [music-gen-standalone](2026-03-23-music-gen-standalone-design.md) | `MusicGenService` | `services/music_gen/` | minimax |

## Cross-Cutting Concerns

### Adapter ABC cleanup (after all 6 specs complete)

Remove from `lingtai/llm/base.py`:
- `supports_web_search` property

Remove from Gemini adapter:
- `web_search()`, `generate_vision()`, `generate_multimodal()`
- `text_to_speech()`, `generate_image()`, `transcribe()`, `generate_music()`
- `_model_web_search`, `_model_vision`, `_model_image_gen`, `_model_tts`, `_model_transcribe` (capability model fields)
- `capability_models` from `gemini/defaults.py`

Remove from Anthropic adapter:
- `supports_web_search` property, `web_search()`, `generate_vision()`

Remove from OpenAI adapter:
- `supports_web_search` property, `web_search()`, `generate_vision()`

Remove from MiniMax adapter:
- `supports_web_search` property, `web_search()`, `generate_vision()`

### MiniMax MCP client cleanup (after specs 1, 2 complete)

After web_search and vision are extracted, `llm/minimax/mcp_client.py` (the coding-plan MCP singleton) becomes dead code — nothing in the adapter calls `get_minimax_mcp_client()` anymore. Delete it.

`llm/minimax/mcp_media_client.py` stays until specs 3, 4, 6 complete. After those, the media capabilities create their own service-level MCP clients, so `mcp_media_client.py` also becomes dead code. Delete it.

### `__init__.py` public API

After all specs complete, update `src/lingtai/__init__.py`:

Remove:
- `LLMVisionService` (eliminated)
- `LLMSearchService` (eliminated)

Add:
- `create_vision_service`
- `create_search_service`
- `TTSService`, `create_tts_service`
- `ImageGenService`, `create_image_gen_service`
- `TranscriptionService`, `create_transcription_service`
- `MusicGenService`, `create_music_gen_service`

Update paths:
- `VisionService` → from `services.vision`
- `SearchService`, `SearchResult` → from `services.websearch`

### Shared MCP utility pattern

Specs 1-2 (websearch, vision) use `minimax-coding-plan-mcp`. Specs 3-4, 6 (tts, image_gen, music_gen) use `minimax-mcp`. Each service creates its own `MCPClient`. If MCP subprocess overhead becomes a concern, a shared `MCPClientPool` could be introduced later — but for now, each service owns its lifecycle.

### Delete old service files

- `services/search.py` → replaced by `services/websearch/`
- `services/vision.py` → replaced by `services/vision/`

## Execution Order

The 6 specs have no dependencies on each other — they can all be implemented in parallel. Each spec touches different files:

| Spec | Capability file | Service package | Adapter methods removed |
|------|----------------|-----------------|------------------------|
| websearch | `capabilities/web_search.py` | `services/websearch/` | `web_search()` on 4 adapters |
| vision | `capabilities/vision.py` | `services/vision/` | `generate_vision()` on 4 adapters, `generate_multimodal()` on Gemini |
| tts | `capabilities/talk.py` | `services/tts/` | `text_to_speech()` on Gemini |
| image_gen | `capabilities/draw.py` | `services/image_gen/` | `generate_image()` on Gemini |
| transcription | `capabilities/listen.py` | `services/transcription/` | `transcribe()` on Gemini |
| music_gen | `capabilities/compose.py` | `services/music_gen/` | `generate_music()` on Gemini |

The only shared files are the adapter files (each spec removes different methods) and `__init__.py` (each spec updates different exports). These can be merged cleanly.

### After all 6 complete

1. Verify all adapter non-chat methods are gone
2. Remove `supports_web_search` from `lingtai/llm/base.py`
3. Delete `llm/minimax/mcp_client.py` (coding-plan singleton)
4. Delete `llm/minimax/mcp_media_client.py` (media client factory)
5. Clean up `gemini/defaults.py` — remove `capability_models` dict
6. Final `__init__.py` audit
7. Full test suite: `python -m pytest tests/`
8. Smoke-test: `python -c "import lingtai"`
