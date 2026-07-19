---
name: preset-skill-minimax
description: Official-source-led manual for the TUI `minimax` template.
version: 2.0.0
last_changed_at: "2026-07-19T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# `minimax`

`minimaxPreset()` (`tui/internal/preset/preset.go:979-1004`) ships provider
`minimax`, exact model `MiniMax-M3`, and `MINIMAX_API_KEY` at the
Anthropic-compatible regional endpoint `https://api.minimaxi.com/anthropic`
(CN default) or `https://api.minimax.io/anthropic` (INTL). Its manifest
explicitly wires `vision`.

## Template-specific settings

MiniMax-M3 natively accepts image/video content blocks on that same
endpoint; this native path does not depend on an MCP or a separate plan.

MiniMax also publishes the separate optional
[MiniMax-Coding-Plan-MCP](https://github.com/MiniMax-AI/MiniMax-Coding-Plan-MCP),
package `minimax-coding-plan-mcp`, whose `understand_image` tool is available
through manual install/client registration. The setup guide requires a Token
Plan seat or purchased Credits, and the MCP is not auto-wired into this TUI
preset. Keep this tool path distinct from native M3 vision. The similarly named
community `tomlee2013/minimax-mcp-vision` project is not official.

Read the official [Anthropic-compatible API documentation](https://platform.minimax.io/docs/api-reference/text-chat-anthropic)
and [Token Plan MCP guide](https://platform.minimax.io/docs/guides/token-plan-mcp-guide)
for current details. Do not silently switch providers or auto-load/invoke an
MCP when the native route fails. Verify the manifest in TUI source after
template changes.

## Operations

For base URL/API-compat/model/capability declaration shape versus
credentials, see `reference/operations/endpoint-capabilities/SKILL.md`.
