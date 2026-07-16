---
name: preset-skill-kimi
description: Official-source-led manual for the TUI `kimi` template.
version: 1.0.0
last_changed_at: "2026-07-15T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# `kimi`

`kimiPreset()` ships Kimi Code model `kimi-for-coding` at the exact
OpenAI-compatible endpoint `https://api.kimi.com/coding/v1` with
`KIMI_CODE_API_KEY`. The manifest has no built-in `vision` capability, so this
shipped preset remains unwired for LingTai-side vision.

Current Kimi Code/K2.7 evidence describes native text, image, and video input,
but the literal `kimi-for-coding` endpoint mapping was not mechanically pinned
by the reviewed provider model table. Treat that mapping as conditional rather
than claiming provider-wide text-only or unconditional direct support. Read the
official [Kimi Code docs](https://www.kimi.com/code/docs/en/), [K2.7 Code
quickstart](https://platform.kimi.com/docs/guide/kimi-k2-7-code-quickstart), and
[provider configuration](https://www.kimi.com/code/docs/en/kimi-code-cli/configuration/providers.html)
when the model or endpoint changes. This manual makes no automatic fallback or
MCP claim; do not guess credentials or silently switch providers.

Verify current wiring in `preset.go`; provider pages own volatile facts.
