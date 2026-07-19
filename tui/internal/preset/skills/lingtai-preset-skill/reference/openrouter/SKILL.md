---
name: preset-skill-openrouter
description: Official-source-led manual for the TUI `openrouter` template.
version: 2.0.0
last_changed_at: "2026-07-19T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# `openrouter`

`openrouterPreset()` (`tui/internal/preset/preset.go:1127-1146`) ships
gateway model `z-ai/glm-5.1`, provider `openrouter`, a provider-resolved
base URL, and `OPENROUTER_API_KEY`. The exact shipped slug is
text-in/text-out, and the manifest has no `vision` capability.

## Template-specific settings

The stock template is text-only and does not invoke the vision tool. If an
operator creates a user-owned saved preset that explicitly adds
`capabilities.vision`, actual image support still depends on the selected
downstream model; only then may the vision tool attempt the saved preset's
endpoint, model, and credential over the compatible route. Gateway-wide
multimodality alone is not proof that a particular downstream model accepts
images.

Read the official [OpenRouter model guide](https://openrouter.ai/docs/guides/overview/models)
and [image-understanding guide](https://openrouter.ai/docs/guides/overview/multimodal/image-understanding)
on demand. No OpenRouter plan-level vision MCP is evidenced. If the real image
request fails, the sanitized vision tool result reports the failure type and
points to `vision(action="manual")` for explicit alternatives. Do not silently
change model/provider or auto-load/invoke an MCP. Verify provider, model,
endpoint resolution, and capability flags in TUI source.

## Operations

For base URL/API-compat/model/capability declaration shape versus
credentials, see `reference/operations/endpoint-capabilities/SKILL.md`.
