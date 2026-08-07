---
name: preset-skill-opencode-go
description: Official-source-led manual for the TUI `opencode-go` template.
version: 1.0.0
last_changed_at: "2026-08-07T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# `opencode-go`

`opencodeGoPreset()` (`tui/internal/preset/preset.go:534-560`) is the TUI
built-in template for the OpenCode Go subscription
(https://opencode.ai/docs/go/) — curated open coding models served through
the cloud Zen Go endpoint. It is a plain OpenAI-compatible endpoint, so the
preset reuses the generic `custom` provider with an explicit `base_url` of
`https://opencode.ai/zen/go/v1`, `api_compat: "openai"`, and
`api_key_env: "OPENCODE_GO_API_KEY"`. No kernel-side provider registration
is needed.

The preset deliberately leaves `model` blank: users fill in a Go model id
themselves. See https://opencode.ai/docs/go/ for the current model list, or
call `GET https://opencode.ai/zen/go/v1/models` with their API key.

## Template-specific settings

The preset exposes only the `chat/completions` wire (`wire_api:
"chat_completions"`). The Zen Go endpoint also supports a Responses
(`/responses`) route for some models (e.g. gpt-5.6-luna), but surfacing both
wires in the template would be confusing, so the template pins chat only.

Like the stock `custom` template, the preset is text-only by default: the
manifest has no `vision` capability. If an operator creates a user-owned
saved preset that explicitly adds `capabilities.vision`, actual image
support still depends on the selected Go model; only then may the vision
tool attempt the saved preset's endpoint, model, and credential over the
compatible route.

Read the official [OpenCode Go docs](https://opencode.ai/docs/go/) on demand.
No OpenCode plan-level vision MCP is evidenced. If a real image request
fails, the sanitized vision tool result reports the failure type and points
to `vision(action="manual")` for explicit alternatives. Do not silently
change model/provider or auto-load/invoke an MCP. Verify provider, model,
endpoint, and capability flags in TUI source.

## Operations

For base URL/API-compat/model/capability declaration shape versus
credentials, see `reference/operations/endpoint-capabilities/SKILL.md`.
