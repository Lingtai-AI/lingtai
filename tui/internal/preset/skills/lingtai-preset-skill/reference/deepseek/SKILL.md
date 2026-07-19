---
name: preset-skill-deepseek
description: Official-source-led manual for the TUI `deepseek` template.
version: 1.0.0
last_changed_at: "2026-07-18T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# `deepseek`

`deepseekPreset()` uses the shared OpenAI-compatible text shape in
`tui/internal/preset/preset.go`: provider `deepseek`, default model
`deepseek-v4-pro`, `https://api.deepseek.com`, and `DEEPSEEK_API_KEY`.
The shipped manifest has no `vision` capability, so this manual records no
direct image route and no DeepSeek plan-level vision MCP.

Read the official [DeepSeek API introduction](https://api-docs.deepseek.com/)
when checking current models, protocol, or limits. For an image request,
report that this preset’s shipped wiring is text-only and let the agent
discover an explicitly chosen skill if one exists; do not guess credentials,
switch providers, or auto-load/invoke an MCP. Verify the
provider/model/endpoint/env-var fields in TUI source after a template change.
