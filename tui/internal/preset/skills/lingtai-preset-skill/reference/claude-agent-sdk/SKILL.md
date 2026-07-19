---
name: preset-skill-claude-agent-sdk
description: Official-source-led manual for the TUI `claude-agent-sdk` template.
version: 2.0.0
last_changed_at: "2026-07-19T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# `claude-agent-sdk`

`claudeAgentSDKPreset()` (`tui/internal/preset/preset.go:1210-1240`) uses
provider `claude-agent-sdk`, the CLI model alias `opus`, and the user’s local
Claude Code login. The shipped manifest intentionally has no API key
env-var, base URL, web-search override, or LingTai `vision` capability.

## Template-specific settings

The underlying Claude Opus model is vision-capable, and interactive Claude
Code accepts image input, but that does not establish image forwarding
through the programmatic Agent SDK `query()` surface. No separate Claude
plan-level vision MCP was established by the reviewed evidence.

Read the official [Claude vision guide](https://platform.claude.com/docs/en/build-with-claude/vision),
[Claude Code common workflows](https://code.claude.com/docs/en/common-workflows),
and [Agent SDK overview](https://code.claude.com/docs/en/agent-sdk/overview)
when current CLI capabilities matter. If the local SDK route cannot handle an
image, report that limitation; do not invent an HTTP endpoint, guess auth, or
auto-load/invoke an MCP. Recheck TUI source for the alias and conservative
capability wiring. The TUI does not store or reproduce the local login
credentials.

## Operations

For base URL/API-compat/model/capability declaration shape versus
credentials, see `reference/operations/endpoint-capabilities/SKILL.md`.
