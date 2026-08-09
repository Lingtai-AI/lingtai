---
name: preset-skill-deepseek
description: Official-source-led manual for the TUI `deepseek` template.
version: 2.1.0
last_changed_at: "2026-08-07T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# `deepseek`

`deepseekPreset()` (`tui/internal/preset/preset.go:1187-1196`) uses the
shared OpenAI-compatible text shape: provider `deepseek`, default model
`deepseek-v4-pro`, `https://api.deepseek.com`, and `DEEPSEEK_API_KEY`.
The shipped manifest has no `vision` capability, so this manual records no
direct image route and no DeepSeek plan-level vision MCP.

## Template-specific settings

Read the official [DeepSeek API introduction](https://api-docs.deepseek.com/)
when checking current models, protocol, or limits. For an image request,
report that this preset's shipped wiring is text-only and let the agent
discover an explicitly chosen skill if one exists; do not guess credentials,
switch providers, or auto-load/invoke an MCP. Verify the
provider/model/endpoint/env-var fields in TUI source after a template change.

The deepseek preset's `base_url` offers three options in the TUI preset
editor (`ProviderRegionURLs["deepseek"]`):

- **DeepSeek API** — `https://api.deepseek.com`, `DEEPSEEK_API_KEY`
  (the template default).
- **OpenCode Go** — `https://opencode.ai/zen/go/v1`, `OPENCODE_GO_API_KEY`.
  This option is scoped to **DeepSeek models served through the OpenCode Go
  subscription**: the preset keeps `provider: deepseek` and the model row
  stays on the DeepSeek lineup (`deepseek-v4-pro` / `deepseek-v4-flash`,
  both served by `/zen/go/v1`). For other Go models (grok, gpt-5.6-luna,
  glm, kimi, qwen, minimax, ...) create a **Custom** preset instead, where
  the model row is free-text and `wire_api` stays selectable. See the
  official [OpenCode Go docs](https://opencode.ai/docs/go/). China-hosted
  `deepseek-v4-flash` requires opting in at the OpenCode workspace page
  before first use.
- **Custom** — free-typed endpoint. `api_key_env` keeps whatever slot you
  already configured; change it on the API key row if your endpoint needs a
  different one.

Selecting the DeepSeek API or OpenCode Go option also switches `api_key_env`
to the matching slot.

## Operations

For base URL/API-compat/model/capability declaration shape versus
credentials, see `reference/operations/endpoint-capabilities/SKILL.md`.
