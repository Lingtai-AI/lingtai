---
name: preset-skill-custom
description: Official-source-led manual for the TUI `custom` template.
version: 2.1.0
last_changed_at: "2026-08-01T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# `custom`

`customPreset()` (`tui/internal/preset/preset.go:1242-1264`) is a
user-supplied OpenAI-compatible template: model is empty until configured,
the key slot is `LLM_API_KEY`, and the endpoint is user-supplied. Its
`vision` capability inherits the configured LLM endpoint.

## Template-specific settings

Whether images work is therefore unknown until the actual provider, model,
protocol, and endpoint are identified. The vision tool still tries the current
OpenAI-compatible endpoint, model, and credential by default instead of treating
that uncertainty as setup-time manual-only.

Read the configured endpoint’s official documentation on demand. Useful
protocol references are the [OpenAI Chat API](https://platform.openai.com/docs/api-reference/chat),
[Anthropic Messages API](https://docs.anthropic.com/en/api/messages), or
[Gemini API](https://ai.google.dev/gemini-api/docs), as applicable; none is a
claim about an unspecified endpoint. No plan-level vision MCP can be asserted.

If direct vision fails, report the endpoint-specific limitation and let the
agent choose an explicit skill. Do not guess credentials, switch providers, or
auto-load/invoke an MCP. Verify all inherited fields in TUI source and inspect
the saved manifest for the user’s actual configuration.

## Responses transport

For `provider: "custom"` with `api_compat: "openai"`, selecting the explicit
`wire_api: "responses"` path reveals a separate Responses transport choice:

- `http` is the default and is omitted from the saved manifest.
- `websocket` writes `responses_transport: "websocket"` and opts into
  Responses WebSocket v2.

The selector appears only in that exact scope. Changing provider,
`api_compat`, or `wire_api` removes a stale `responses_transport` value before
save. Selecting WebSocket does not claim that an arbitrary compatible endpoint
supports it: a matching Kernel version performs the actual handshake and
reports that the configured upstream does not support or rejected Responses
WebSocket v2. It does not silently fall back to HTTP; choose `http` explicitly
when the upstream lacks WebSocket support.

## Operations

For base URL/API-compat/model/capability declaration shape versus
credentials, see `reference/operations/endpoint-capabilities/SKILL.md`.
