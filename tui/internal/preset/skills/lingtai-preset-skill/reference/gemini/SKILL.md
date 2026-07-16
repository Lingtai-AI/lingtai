---
name: preset-skill-gemini
description: Official-source-led manual for the TUI `gemini` template.
version: 1.0.0
last_changed_at: "2026-07-15T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# `gemini`

`geminiPreset()` ships provider `gemini` with the exact default model
`gemini-3-flash-preview` and `GEMINI_API_KEY`. It uses Google’s native adapter:
there is no `base_url` or OpenAI-compat override. Its manifest includes
`vision: {provider: gemini, api_key_env: GEMINI_API_KEY}`, alongside the
DuckDuckGo `web_search` and default `skills` capabilities.

The model’s inline image input in an ordinary chat turn is native
multimodality. The explicit LingTai `vision` capability is a separate tool
path; both use the shipped Gemini provider rather than a fallback provider.
Read Google’s official [model page](https://ai.google.dev/gemini-api/docs/models)
and [image-understanding guide](https://ai.google.dev/gemini-api/docs/image-understanding)
for current model and image-input details. No official Google plan-level vision
MCP is established here. Do not silently switch providers or auto-load/invoke an
MCP when the direct route fails.

Recheck `preset.go` for the shipped model, credential env-var, and capability
wiring. Never record a key or guessed account details.
