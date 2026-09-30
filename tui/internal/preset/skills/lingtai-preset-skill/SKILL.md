---
name: lingtai-preset-skill
description: "Use when asking about built-in TUI presets, the four provider families, or their shared operations."
version: 4.0.0
last_changed_at: "2026-09-29T00:00:00Z"
related_files:
  - tui/internal/preset/preset.go
  - tui/internal/tui/preset_editor.go
  - tui/internal/tui/SKILL.md
  - tui/internal/preset/ANATOMY.md
  - tui/CONTRACT.md
  - tui/internal/preset/revision.go
  - tui/internal/headless/preset_revision.go
  - tui/main.go
  - tui/internal/preset/skills/lingtai-preset-skill/reference/codex/SKILL.md
  - tui/internal/preset/skills/lingtai-preset-skill/reference/claude/SKILL.md
  - tui/internal/preset/skills/lingtai-preset-skill/reference/openai/SKILL.md
  - tui/internal/preset/skills/lingtai-preset-skill/reference/anthropic/SKILL.md
  - tui/internal/preset/skills/lingtai-preset-skill/reference/operations/saved-presets/SKILL.md
  - tui/internal/preset/skills/lingtai-preset-skill/reference/operations/endpoint-capabilities/SKILL.md
  - tui/internal/preset/skills/lingtai-preset-skill/reference/operations/availability-save-gate/SKILL.md
  - tui/internal/preset/skills/lingtai-preset-skill/reference/operations/activation-session-refresh/SKILL.md
  - tui/internal/preset/skills/lingtai-preset-skill/reference/operations/troubleshooting-migration/SKILL.md
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# Built-in preset router

LingTai ships exactly four provider families, one built-in template each.
This router covers exactly the names returned by `BuiltinPresets()` in
`tui/internal/preset/preset.go`. It covers TUI-owned template presets only,
not arbitrary saved presets. Route a named-preset question or revision to the
direct child with the matching name under `reference/<name>/SKILL.md`. That
child owns the family's endpoint/credential facts, exact TUI surfaces, and
named-preset revision instructions.

There are no per-vendor templates. Any other vendor, subscription, gateway,
or account pool is reached through the `openai` or `anthropic` family pointed
at that vendor's compatible endpoint (`base_url`) — never through a
vendor-named provider, which the kernel rejects.

## Direct children: the 4 BuiltinPresets names

| Name | Provider | Direct child | Route hint |
|---|---|---|---|
| codex | `codex` | reference/codex/SKILL.md | ChatGPT-account (Codex OAuth) route; the first-run default |
| claude | `claude-code` | reference/claude/SKILL.md | local Claude Code CLI login ("claude-p") |
| openai | `openai` | reference/openai/SKILL.md | any OpenAI-compatible endpoint, Chat Completions or Responses; also account pools such as sub2api / subs-pool |
| anthropic | `anthropic` | reference/anthropic/SKILL.md | any Anthropic Messages-compatible endpoint |

```yaml
- name: preset-skill-codex
  location: reference/codex/SKILL.md
- name: preset-skill-claude
  location: reference/claude/SKILL.md
- name: preset-skill-openai
  location: reference/openai/SKILL.md
- name: preset-skill-anthropic
  location: reference/anthropic/SKILL.md
```

Do not merge the Codex OAuth catalog, the Claude CLI aliases, and an
arbitrary endpoint's served models. `openai` and `anthropic` have no universal
model: the model is free text and must be checked against the configured
endpoint. The exact constructor in `preset.go` is always the first source to
inspect; picker, capability, and credential surfaces are listed by each
direct child.

## Shared operation children: five unchanged mechanics

| Question | Child |
|---|---|
| Saved templates/presets, load/save/delete, and bootstrap ordering | reference/operations/saved-presets/SKILL.md |
| Endpoint, provider/model/capability declarations, and Codex OAuth quota | reference/operations/endpoint-capabilities/SKILL.md |
| Whether Save calls a provider and where availability is diagnosed | reference/operations/availability-save-gate/SKILL.md |
| Activation, session state, and what refresh switches | reference/operations/activation-session-refresh/SKILL.md |
| Bounded troubleshooting and migration routing | reference/operations/troubleshooting-migration/SKILL.md |

```yaml
- name: preset-skill-op-saved-presets
  location: reference/operations/saved-presets/SKILL.md
- name: preset-skill-op-endpoint-capabilities
  location: reference/operations/endpoint-capabilities/SKILL.md
- name: preset-skill-op-availability-save-gate
  location: reference/operations/availability-save-gate/SKILL.md
- name: preset-skill-op-activation-session-refresh
  location: reference/operations/activation-session-refresh/SKILL.md
- name: preset-skill-op-troubleshooting-migration
  location: reference/operations/troubleshooting-migration/SKILL.md
```

These five children remain shared production mechanics. A named revision
composes its direct child with the mechanics it needs; it does not create a
second provider tree or a revision operation child.

## Deterministic revision CLI and engine

For a reviewed change, prepare an evidence-bound manifest and explicit input,
then run:

```text
lingtai-tui presets revise --manifest PATH --input PATH \
  --mode dry-run|check|apply [--output-dir PATH]
```

The headless adapter at tui/internal/headless/preset_revision.go reads only
those paths and dispatches before preset bootstrap, provider access, OAuth,
MCP, network, auth, or runtime reads. dry-run and check do not write; apply
requires a new explicit output directory. The pure engine at
tui/internal/preset/revision.go validates the typed target state, route
bindings, evidence, expected old values, and input/post-image hashes, then
splices only owned JSON bytes while preserving unowned bytes. Review the JSON
plan and diagnostics before apply.

## Boundaries and maintenance

Family children record only reviewed facts and official lookup paths; a
failed direct route is not permission to switch providers, guess credentials,
or auto-load an MCP. Saved presets may change provider, model, endpoint,
credentials, or capabilities, so inspect their actual manifest. A saved preset
that still names a retired vendor provider (`custom`, `deepseek`, `minimax`,
...) must be moved to one of the four families — see
`reference/operations/troubleshooting-migration/SKILL.md`.

When `BuiltinPresets()` gains a new template name, add exactly one matching
direct child and catalog row in the same PR. When a new cross-cutting mechanic is added, extend the five-operation catalog rather than adding a revision
child. When a relevant TUI preset/page is revised, check the matching child
and include its revision in the same PR when needed.
