---
name: preset-skill-codex
description: Official-source-led manual for the TUI `codex` template.
version: 1.0.0
last_changed_at: "2026-07-18T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# `codex`

`codexPreset()` uses provider `codex`, model `gpt-5.6-sol`, the Codex endpoint
`https://chatgpt.com/backend-api/codex`, and ChatGPT OAuth rather than an API
key env-var. The manifest exposes provider-native `vision` and web search.
Exact image support can depend on the current model/account; verify it rather
than treating this manual as a promise.

Read the official [Codex authentication](https://developers.openai.com/codex/auth)
and [Codex models](https://developers.openai.com/codex/models) pages on demand.
No separate Codex vision MCP is established. If the native route fails,
report the failure and use this manual for discovery; do not fall back to a
generic OpenAI key, switch providers, or auto-load/invoke an MCP. Recheck the
TUI preset source for model and capability changes. Never inspect, print, or
reproduce OAuth token contents.
