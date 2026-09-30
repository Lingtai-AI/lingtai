---
name: preset-skill-anthropic
description: "Use when configuring or revising the built-in anthropic (Anthropic-compatible) TUI preset, including pointing it at another vendor's Anthropic-compatible endpoint."
version: 1.0.0
last_changed_at: "2026-09-29T00:00:00Z"
related_files:
  - tui/internal/preset/preset.go
  - tui/internal/tui/preset_editor.go
  - tui/internal/tui/doctor.go
  - tui/internal/tui/SKILL.md
  - tui/CONTRACT.md
  - tui/internal/preset/revision.go
  - tui/internal/headless/preset_revision.go
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# anthropic preset (Anthropic-compatible)

Use this child for the built-in `anthropic` template (display
"Anthropic-compatible") and for any saved preset whose provider is
`anthropic`. anthropicPreset in tui/internal/preset/preset.go ships provider
`anthropic`, an empty model, `api_key_env` `ANTHROPIC_API_KEY`, and
`base_url` null, with web_search on DuckDuckGo, vision inheriting the agent's
own provider, and the skills default. It is an API-key route; the Claude Code
CLI login is the separate `claude` preset (`reference/claude/SKILL.md`).

## Template-specific settings

- **`base_url`** — optional. Empty means the official Anthropic API
  (`https://api.anthropic.com`). Set it to any Anthropic Messages-compatible
  endpoint (another vendor's `/anthropic` route, a relay, or a local proxy).
  The value excludes the version path: requests go to
  `{base_url}/v1/messages`.
- **`api_key_env`** — the env-var name holding the key (default
  `ANTHROPIC_API_KEY`). Editing and saving the template stamps a fresh
  per-preset slot (`ANTHROPIC_1_API_KEY`, ...). The key is sent as
  `x-api-key`.
- **`model`** — free text. Use a model ID the configured endpoint serves:
  for the official API read the Anthropic models documentation (or an
  authenticated `GET {base_url}/v1/models`); for another vendor read that
  vendor's documentation. Save refuses an empty model.
- **`thinking`** — standard reasoning effort: `none`, `minimal`, `low`,
  `medium`, `high`, `xhigh`, or omitted (the kernel default); the adapter
  maps a level to an extended-thinking budget.

There is no `wire_api` or `service_tier` on this family. A vendor that only
exposes an OpenAI-compatible endpoint uses `reference/openai/SKILL.md`.

## TUI surfaces to revise

Start at anthropicPreset in tui/internal/preset/preset.go. In
tui/internal/tui/preset_editor.go `anthropic` has no `providerModels` entry
(free-text model), base_url is free text with the official endpoint shown
when empty, `levelThinkingOptions` drives the reasoning row, and the service
tier and wire rows are hidden. `/doctor` probes this family with
`GET {base_url}/v1/models` (`x-api-key` + `anthropic-version`) and one
`max_tokens=1` Messages call (`probeLLM` in tui/internal/tui/doctor.go).

## Reviewed deterministic revision

Prepare an evidence-bound manifest and explicit input, then run
lingtai-tui presets revise --manifest PATH --input PATH --mode dry-run|check|apply
[--output-dir PATH]. Review the JSON plan, use dry-run/check before apply, and
apply only to a new explicit output directory. revision.go validates hashes,
route bindings, and evidence and preserves unowned bytes.

Maintenance: If the relevant TUI preset/page is revised, check whether this sub-skill also needs revision and, if so, include it in the same PR.

## Operations

For save, endpoint/capability, availability, activation/refresh, or
troubleshooting, use the five shared operation children under
`reference/operations/`; this child owns the Anthropic-compatible family
facts, so inspect the actual saved manifest for user-owned endpoint, model,
credential, and capability facts.
