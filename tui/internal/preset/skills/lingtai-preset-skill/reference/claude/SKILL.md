---
name: preset-skill-claude
description: Official-source-led manual for the TUI `claude` template.
version: 2.1.0
last_changed_at: "2026-07-21T00:00:00-07:00"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# `claude`

`claudePreset()` (`tui/internal/preset/preset.go:1212-1241`) uses canonical
provider `claude-code`, displayed in the TUI as print-mode backend `claude-p`.
It reuses the current Claude Code OAuth login and shows the account email
reported by `claude auth status --json`; the TUI never reads or stores Claude
credentials. The default model is `opus`, and the editor also offers `fable`
(the current Claude Code CLI maps it to full model id `claude-fable-5`),
`sonnet`, and `haiku`.

The shipped manifest intentionally has no API-key env-var, base URL, web-search
override, or LingTai `vision` capability.

## Template-specific settings

The underlying Claude models and interactive Claude Code may accept images, but
that does not establish image forwarding through LingTai's CLI adapter. No
separate Claude plan-level vision MCP was established by the reviewed evidence.

Read the official [Claude vision guide](https://platform.claude.com/docs/en/build-with-claude/vision)
and [Claude Code common workflows](https://code.claude.com/docs/en/common-workflows)
when current CLI capabilities matter. If the local CLI route cannot handle an
image, report that limitation; do not invent an HTTP endpoint, guess auth, or
auto-load/invoke an MCP. Recheck TUI source for the aliases and conservative
capability wiring.

## Operations

For base URL/API-compat/model/capability declaration shape versus credentials,
see `reference/operations/endpoint-capabilities/SKILL.md`.
