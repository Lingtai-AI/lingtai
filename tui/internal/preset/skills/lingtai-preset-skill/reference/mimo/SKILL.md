---
name: preset-skill-mimo
description: Official-source-led manual for the TUI `mimo` template.
version: 1.0.0
last_changed_at: "2026-07-15T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# `mimo`

`mimoPreset()` ships Xiaomi MiMo model `mimo-v2.5` at
`https://api.xiaomimimo.com/v1` with OpenAI compatibility and
`XIAOMI_API_KEY`. The manifest explicitly wires native vision to that exact
default model. The current TUI picker retains `mimo-v2.5-pro` as a text-only
sibling; retired V2 Flash IDs are not shipped or selectable.

Read the official [MiMo developer introduction](https://platform.xiaomimimo.com/llms.txt)
and [OpenAI-compatible API page](https://platform.xiaomimimo.com/docs/zh-CN/api/chat/openai-api)
on demand for current models, regions, and image-input rules. No official MiMo
vision MCP is established by the reviewed evidence. A direct-call failure
remains a direct-call failure; do not switch providers or auto-load/invoke an
MCP.

Recheck `preset.go` and the preset editor for LingTai-owned model, endpoint,
env-var, and capability wiring. Never include credentials here.
