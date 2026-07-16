---
name: preset-skill-custom
description: Official-source-led manual for the TUI `custom` template.
version: 1.0.0
last_changed_at: "2026-07-15T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# `custom`

`customPreset()` is a user-supplied OpenAI-compatible template: model is
empty until configured, the key slot is `LLM_API_KEY`, and the endpoint is
user-supplied. Its `vision` capability inherits the configured LLM endpoint.
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
agent choose an explicit skill. Do not guess credentials, switch providers,
or automatically load or invoke an MCP. Verify all inherited fields in TUI
source and inspect the saved manifest for the user’s actual configuration.
