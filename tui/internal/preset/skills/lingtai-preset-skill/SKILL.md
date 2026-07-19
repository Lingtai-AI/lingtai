---
name: lingtai-preset-skill
description: >
  Routes questions about the 12 TUI-shipped built-in preset templates to one
  thin, official-source-led child manual. Read a child only when its preset is
  relevant; this does not describe arbitrary saved presets.
version: 1.0.0
last_changed_at: "2026-07-18T00:00:00Z"
related_files:
  - tui/internal/preset/skills/lingtai-preset-skill/SKILL.md
  - tui/internal/preset/preset.go
  - tui/internal/preset/ANATOMY.md
  - tui/internal/preset/preset_skill_router_test.go
  - tui/internal/preset/skill_metadata_test.go
  - tui/internal/preset/skills/lingtai-preset-skill/reference/minimax/SKILL.md
  - tui/internal/preset/skills/lingtai-preset-skill/reference/zhipu/SKILL.md
  - tui/internal/preset/skills/lingtai-preset-skill/reference/mimo/SKILL.md
  - tui/internal/preset/skills/lingtai-preset-skill/reference/deepseek/SKILL.md
  - tui/internal/preset/skills/lingtai-preset-skill/reference/gemini/SKILL.md
  - tui/internal/preset/skills/lingtai-preset-skill/reference/kimi/SKILL.md
  - tui/internal/preset/skills/lingtai-preset-skill/reference/nvidia/SKILL.md
  - tui/internal/preset/skills/lingtai-preset-skill/reference/openrouter/SKILL.md
  - tui/internal/preset/skills/lingtai-preset-skill/reference/codex/SKILL.md
  - tui/internal/preset/skills/lingtai-preset-skill/reference/codex-pool/SKILL.md
  - tui/internal/preset/skills/lingtai-preset-skill/reference/claude-agent-sdk/SKILL.md
  - tui/internal/preset/skills/lingtai-preset-skill/reference/custom/SKILL.md
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# Built-in preset manuals

This router covers exactly the names returned by `BuiltinPresets()` in
`tui/internal/preset/preset.go`. It is for TUI-owned template presets only.
When a question names one of them, open its child manual and then read the
linked official introduction or vision page on demand. Provider pages own
volatile model, pricing, endpoint, protocol, and plan facts; the TUI source
owns the shipped name, manifest wiring, and capability flags.

## Nested reference catalog

Each entry is a child manual, not a standalone top-level skill:

```yaml
- name: preset-skill-minimax
  location: reference/minimax/SKILL.md
- name: preset-skill-zhipu
  location: reference/zhipu/SKILL.md
- name: preset-skill-mimo
  location: reference/mimo/SKILL.md
- name: preset-skill-deepseek
  location: reference/deepseek/SKILL.md
- name: preset-skill-gemini
  location: reference/gemini/SKILL.md
- name: preset-skill-kimi
  location: reference/kimi/SKILL.md
- name: preset-skill-nvidia
  location: reference/nvidia/SKILL.md
- name: preset-skill-openrouter
  location: reference/openrouter/SKILL.md
- name: preset-skill-codex
  location: reference/codex/SKILL.md
- name: preset-skill-codex-pool
  location: reference/codex-pool/SKILL.md
- name: preset-skill-claude-agent-sdk
  location: reference/claude-agent-sdk/SKILL.md
- name: preset-skill-custom
  location: reference/custom/SKILL.md
```

## Routing table

| Preset | Child manual | Stable routing hint |
|---|---|---|
| `minimax` | `reference/minimax/SKILL.md` | MiniMax, Anthropic-shaped endpoint |
| `zhipu` | `reference/zhipu/SKILL.md` | Zhipu/Z.AI GLM Coding Plan |
| `mimo` | `reference/mimo/SKILL.md` | Xiaomi MiMo, OpenAI-compatible |
| `deepseek` | `reference/deepseek/SKILL.md` | DeepSeek, OpenAI-compatible text route |
| `gemini` | `reference/gemini/SKILL.md` | Google Gemini native multimodal route |
| `kimi` | `reference/kimi/SKILL.md` | Kimi Code, OpenAI-compatible |
| `nvidia` | `reference/nvidia/SKILL.md` | NVIDIA NIM/API Catalog gateway |
| `openrouter` | `reference/openrouter/SKILL.md` | OpenRouter gateway |
| `codex` | `reference/codex/SKILL.md` | ChatGPT OAuth Codex route |
| `codex-pool` | `reference/codex-pool/SKILL.md` | pooled ChatGPT OAuth Codex route |
| `claude-agent-sdk` | `reference/claude-agent-sdk/SKILL.md` | local Claude Code login |
| `custom` | `reference/custom/SKILL.md` | user-supplied compatible endpoint |

## Boundaries and maintenance

The manuals describe direct provider-native or current-preset
OpenAI-compatible vision conservatively. A missing or failed direct route is
not a reason to switch providers, guess credentials, or auto-load/invoke an
MCP. Reviewed official evidence positively identifies optional plan-level
vision MCPs for Zhipu/Z.AI and MiniMax; their child manuals link them as
explicit, manual-only methods. Unknowns remain unknown.

Saved presets are different: a clone under `~/.lingtai-tui/presets/saved/`
may change provider, model, endpoint, credentials, or capabilities. Inspect
its actual manifest instead of treating this catalog as coverage for it.

When `BuiltinPresets()` gains a new template name, the same change must add a
`reference/<name>/SKILL.md` child and both parent entries. The focused router
test keeps the source list, embedded children, parent metadata, and extracted
paths in bijection.
