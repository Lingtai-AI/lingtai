---
name: preset-skill-codex
description: Official-source-led manual for the TUI `codex` template.
version: 2.1.0
last_changed_at: "2026-07-19T12:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# `codex`

`codexPreset()` (`tui/internal/preset/preset.go:1148-1176`) uses provider
`codex`, model `gpt-5.6-sol`, the Codex endpoint
`https://chatgpt.com/backend-api/codex`, and ChatGPT OAuth rather than an API
key env-var. The manifest exposes provider-native `vision` and web search.

## Template-specific settings

Exact image support can depend on the current model/account; verify it rather
than treating this manual as a promise.

Read the official [Codex authentication](https://developers.openai.com/codex/auth)
and [Codex models](https://developers.openai.com/codex/models) pages on demand.
No separate Codex vision MCP is established. If the native route fails,
report the failure and use this manual for discovery; do not fall back to a
generic OpenAI key, switch providers, or auto-load/invoke an MCP. Recheck the
TUI preset source for model and capability changes. Never inspect, print, or
reproduce OAuth token contents.

## Operations

For base URL/API-compat/model/capability declaration shape versus
credentials, see `reference/operations/endpoint-capabilities/SKILL.md`. See
also `reference/codex-pool/SKILL.md` for the pooled multi-account variant.

To check live OAuth quota/rate-limit state for this account, query the
app-server directly: complete `initialize`, then send the
`account/rateLimits/read` request (its params are structurally `null` — no
request body), and read the `GetAccountRateLimitsResponse`
(`usedPercent`/`windowDurationMins`/`resetsAt` per window, plan/credits
fields). Optionally also watch the sparse `account/rateLimits/updated`
notification as a rolling supplement, never a substitute for the read.
Full field-by-field routing, the official-272K-vs-measured-372K distinction,
and secret-safety rules live in
`reference/operations/endpoint-capabilities/SKILL.md` — do not restate or
re-derive that evidence here.
