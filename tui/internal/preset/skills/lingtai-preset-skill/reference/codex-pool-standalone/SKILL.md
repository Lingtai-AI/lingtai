---
name: preset-skill-codex-pool-standalone
description: "Use when revising the built-in codex-pool-standalone TUI preset."
version: 1.0.0
last_changed_at: "2026-09-10T00:00:00Z"
related_files:
  - tui/internal/preset/preset.go
  - tui/internal/tui/preset_editor.go
  - tui/internal/tui/login.go
  - tui/internal/tui/SKILL.md
  - tui/CONTRACT.md
  - tui/internal/preset/revision.go
  - tui/internal/headless/preset_revision.go
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# codex-pool-standalone preset revision

Use this child for the named built-in codex-pool-standalone preset.
codexPoolStandalonePreset in tui/internal/preset/preset.go binds the kernel's
ordinary generic OpenAI-compatible Responses route: `provider: "custom"`,
`api_compat: "openai"`, `wire_api: "responses"`, empty model, a local
`base_url` default of `http://127.0.0.1:8765/v1`, and `api_key_env` pointing
at `CODEX_POOL_API_KEY`, the same local-service key variable consumed by
`codex-pool serve` — never an embedded key. The template model is intentionally
blank for editing: set an upstream-supported model before activation; the proxy
forwards the caller model and does not choose a default model for an empty request.
`compact_threshold: null` disables generic server-side `context_management`,
which Codex does not implement. The existing custom Responses adapter already
replays full history without `previous_response_id`; no Codex-specific adapter
or account logic is added to this preset.

This preset is deliberately NOT the builtin `codex-pool` provider
(reference/codex-pool/SKILL.md): that string is the OAuth multi-account pool
route with its own token-pool file and selection logic. codex-pool-standalone
routes through the same generic custom/OpenAI-compatible path any user-run
Responses-speaking server would use — LingTai has no special-cased knowledge
of the account, quota, or auth behind it.

## Template-specific settings

There is no universal official catalog for codex-pool-standalone, the same as
`custom`: it is a preconfigured generic-OpenAI-compatible endpoint pointed at
a local service, not a native LingTai-owned provider. Read the standalone
`codex-pool` CLI/service's own documentation for its served model IDs, and
verify against its live `/v1` responses route rather than assuming an OpenAI
platform catalog applies.

## TUI surfaces to revise

Start at codexPoolStandalonePreset in tui/internal/preset/preset.go. Because
its provider is exactly `"custom"`, it shares the free-text model/base_url
picker behavior and the `wire_api`/`responses_transport` fields gated by
isCustomOpenAI / isCustomOpenAIResponses in tui/internal/tui/preset_editor.go
— there is no separate picker binding to maintain. The Codex/Setup credentials
screen (tui/internal/tui/login.go) additionally offers a `[p]` action that
launches the independent `codex-pool tui` binary as an external interactive
process (existing `tea.ExecProcess` pattern) and returns to LingTai on exit;
LingTai does not implement account storage, auth, or quota display for that
tool itself — it only shells out to the installed `codex-pool` CLI and shows
install guidance when the binary is not on PATH.

## Relationship to the old builtin Pool

The old builtin `codex-pool` OAuth multi-account pool (reference/codex-pool/
SKILL.md) is unaffected by this preset and remains fully functional; it is a
future deprecation target announced only where its own pool-weight UI renders
(tui/internal/tui/login.go, gated on the active old-Pool credential family
within the Codex account section), never across all
OpenAI-compatible requests. Native single-account Codex (`codex` preset) is
not deprecated and is unaffected by either pool preset.

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
`reference/operations/`; this child owns codex-pool-standalone's
endpoint-derived facts and its distinctness from the old builtin pool.
