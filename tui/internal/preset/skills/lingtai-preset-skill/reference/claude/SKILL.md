---
name: preset-skill-claude
description: "Use when revising the built-in claude TUI preset."
version: 5.0.0
last_changed_at: "2026-09-30T00:00:00Z"
related_files:
  - tui/internal/preset/preset.go
  - tui/internal/tui/preset_editor.go
  - tui/internal/tui/claude_auth.go
  - tui/internal/tui/firstrun.go
  - tui/internal/tui/doctor.go
  - tui/internal/tui/login.go
  - tui/internal/tui/SKILL.md
  - tui/CONTRACT.md
  - tui/internal/preset/revision.go
  - tui/internal/headless/preset_revision.go
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# claude preset revision

Use this child for the named built-in claude preset. claudePreset in
tui/internal/preset/preset.go uses canonical provider claude-code (shown as
"claude-p" in the TUI): the kernel runs Claude Code print mode (`claude -p`)
with LingTai's system prompt in place of Claude Code's. The template carries
no model and no thinking — Claude Code runs its own default model and effort
— and no base_url, web_search override, or LingTai vision capability. An
Anthropic API key or an Anthropic-compatible endpoint is the separate
`anthropic` family (`reference/anthropic/SKILL.md`).

## Template-specific settings

Credential order:

1. A long-lived OAuth token from `claude setup-token`, stored in
   `~/.lingtai-tui/.env` under the preset's `api_key_env` (the template, and
   every Claude preset, uses the one shared `CLAUDE_CODE_OAUTH_TOKEN` slot;
   a legacy Claude preset with no `api_key_env` reads that slot too). When
   present it takes precedence, and the kernel isolates `~/.claude`.
2. Otherwise the local `claude` CLI login. The TUI detects it with the
   non-billed `claude auth status --json` (tui/internal/tui/claude_auth.go)
   and never reads Claude credential files.
3. Neither: the first-run paste step, the editor's auth row, `/doctor`, and
   Setup → Credentials all say "Run `claude setup-token` in a terminal and
   paste the token here".

No TUI surface spends a model call to verify either path; status is presence
only (`ResolvePresetWithAuth`, `probeLLM`, the credentials page).

## TUI surfaces to revise

Start at claudePreset and `ClaudeCodeOAuthTokenEnv` / `APIKeyEnvName` in
tui/internal/preset/preset.go. In tui/internal/tui/preset_editor.go the
claude-code editor is auth-only: `fieldVisible` hides provider, model,
base_url, reasoning, service tier, and wire rows; `claudeAuthLabel` renders
the auth row, which takes a pasted token; `normalizeClaudeCode` drops model,
base_url, and codex_auth_path on save. The first-run key step
(`presetNeedsKey`, `ensureClaudeLoginFor` in firstrun.go), `/doctor`
(`probeClaudeToken` / `probeClaudeLogin` / `probeClaudeNoAuth` in
doctor.go), and Setup → Credentials (`claudeStatus` in login.go) share the
`resolveClaudeAuth` order in claude_auth.go. There is no model catalog for
this provider.

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
`reference/operations/`; this child owns the Claude credential-order facts.
