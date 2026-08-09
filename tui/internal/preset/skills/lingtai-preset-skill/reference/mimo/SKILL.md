---
name: preset-skill-mimo
description: Official-source-led manual for the TUI `mimo` template.
version: 2.1.0
last_changed_at: "2026-08-09T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# `mimo`

`mimoPreset()` (`tui/internal/preset/preset.go:1178-1203`) ships Xiaomi MiMo
model `mimo-v2.5` at `https://api.xiaomimimo.com/v1` with OpenAI
compatibility and `XIAOMI_API_KEY`. The manifest explicitly wires native
vision to that exact default model.

The TUI preset editor's `base_url` row offers two options
(`ProviderRegionURLs["mimo"]`):

- **MiMo** — `https://api.xiaomimimo.com/v1` (the template default).
- **OpenCode Go** — `https://opencode.ai/zen/go/v1`. This is the only row
  that implies a credential: selecting it sets `api_key_env` to
  `OPENCODE_GO_API_KEY` (the shared OpenCode Go account) and saving keeps
  that slot instead of minting a numbered one.

MiMo implies no `api_key_env`, so saving an edited built-in on that row
yields a host-stamped numbered slot (`XIAOMI_1_API_KEY`) rather than
overwriting the provider default. Cycling *off* OpenCode Go restores the slot
that was in place before it was selected, so the choice is reversible.

OpenCode Go model ids are LOWERCASE — `mimo-v2.5`, `mimo-v2.5-pro`,
`mimo-v2-pro`, `mimo-v2-omni`. LingTai-side vision is wired to the default
`mimo-v2.5` only; switching model or endpoint does not re-verify that path.

## Template-specific settings

The current TUI picker retains `mimo-v2.5-pro` as a text-only
sibling; retired V2 Flash IDs are not shipped or selectable.

Read the official [MiMo developer introduction](https://platform.xiaomimimo.com/llms.txt)
and [OpenAI-compatible API page](https://platform.xiaomimimo.com/docs/zh-CN/api/chat/openai-api)
on demand for current models, regions, and image-input rules. No official MiMo
vision MCP is established by the reviewed evidence. A direct-call failure
remains a direct-call failure; do not switch providers or auto-load/invoke an
MCP. Recheck `preset.go` and the preset editor for LingTai-owned model,
endpoint, env-var, and capability wiring.

## Operations

For base URL/API-compat/model/capability declaration shape versus
credentials, see `reference/operations/endpoint-capabilities/SKILL.md`.
