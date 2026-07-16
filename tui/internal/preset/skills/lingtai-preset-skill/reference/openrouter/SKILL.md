---
name: preset-skill-openrouter
description: Official-source-led manual for the TUI `openrouter` template.
version: 1.0.0
last_changed_at: "2026-07-15T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# `openrouter`

`openrouterPreset()` ships gateway model `z-ai/glm-5.1`, provider
`openrouter`, a provider-resolved base URL, and `OPENROUTER_API_KEY`. The exact
shipped slug is text-in/text-out, and the manifest has no `vision` capability.
OpenRouter can accept images only after selecting a different downstream model
that explicitly supports them; gateway-wide multimodality does not change this
default.

Read the official [OpenRouter model guide](https://openrouter.ai/docs/guides/overview/models)
and [image-understanding guide](https://openrouter.ai/docs/guides/overview/multimodal/image-understanding)
on demand. No OpenRouter plan-level vision MCP is evidenced. If the current
route cannot handle an image, report the unknown/unsupported case and let the
agent choose an explicit skill; do not silently change model/provider or
auto-load/invoke an MCP.

Verify provider, model, endpoint resolution, and capability flags in TUI source.
