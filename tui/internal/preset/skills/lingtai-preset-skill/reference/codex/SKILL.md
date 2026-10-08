---
name: preset-skill-codex
description: "Use when revising the built-in codex TUI preset."
version: 4.1.0
last_changed_at: "2026-09-30T00:00:00Z"
related_files:
  - tui/internal/preset/preset.go
  - tui/internal/tui/preset_editor.go
  - tui/internal/tui/SKILL.md
  - tui/CONTRACT.md
  - tui/internal/preset/revision.go
  - tui/internal/headless/preset_revision.go
  - tui/internal/config/codex_public_models.go
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# codex preset revision

Use this child for the named built-in codex preset — the first-run default
template. codexPreset in tui/internal/preset/preset.go ships provider codex,
gpt-5.6-sol, https://chatgpt.com/backend-api/codex, ChatGPT OAuth with an
empty api_key_env, thinking xhigh, web_search on DuckDuckGo, and vision
inheriting the agent's own (Codex) provider. It is not the OpenAI API
preset: an OpenAI API key or an OpenAI-compatible endpoint uses the `openai`
family (`reference/openai/SKILL.md`), and several ChatGPT accounts behind one
endpoint use an external pool such as sub2api / subs-pool through that same
`openai` family.

## Template-specific settings

Since 2026-09-30 the picker's model lineup is fetched, not hand-curated: see
"Public model-directory picker" below before assuming the old
docs-page-comparison workflow still applies to the live list. Never inspect
or print OAuth token contents.

## TUI surfaces to revise

Start at codexPreset in tui/internal/preset/preset.go, which pins the
constructor/default to `config.DefaultCodexModelOptions()[0]`
(tui/internal/config/codex_public_models.go) — currently gpt-5.6-sol: public
Astra documentation is positive, but exact authenticated OAuth-route
availability is not, so the offline default does not silently promote Astra.
Revise codexThinkingOptions in tui/internal/tui/preset_editor.go when Codex's
reasoning choices change; the service tier row offers normal/fast (fast is
sent as priority). Preserve the /codex base_url suffix, empty api_key_env,
OAuth identity (codex_auth_path account binding), and explicit xhigh default.
Follow the Codex-specific checklist in tui/internal/tui/SKILL.md. The
latest-two-generation rule in tui/CONTRACT.md no longer governs Codex — see
that contract's "Codex exception" and the next section — but still governs
Claude Code's CLI aliases.

## Public model-directory picker

Codex's model row is per-editor state
(`PresetEditorModel.codexModels`), not a `providerModels` map entry — there is
no static list to edit when a new model ships. On preset-editor entry,
`config.RefreshCodexPublicModels` (tui/internal/config/codex_public_models.go)
fetches the fixed public directory
`https://raw.githubusercontent.com/openai/codex/main/codex-rs/models-manager/models.json`
(bounded ~5s/~2MiB, last-good cache under `<globalDir>/cache/`, offline
static fallback on any failure) and retains entries whose `display_name`
starts with the exact case-sensitive prefix `GPT` — no numeric-generation,
visibility, or account-entitlement filter. The retained `display_name` is the
displayed label; the entry's `slug` is what gets persisted into
`manifest.llm.model`. This is public metadata, not proof the current account
can use a listed model — treat every entry as a suggestion and let the actual
run be the entitlement check. A saved/custom id absent from the live/cached
list keeps rendering (hollow) and keeps working; pressing `c` on the model
row opens a free-text custom entry for any account-specific or brand-new id.
A completed refresh never changes a saved preset's model, an agent's active
preset, or its thinking level, and never gates Save. To inspect live OAuth
quota instead, complete the app-server initialize handshake, send
account/rateLimits/read with structurally `null` params, and optionally
observe account/rateLimits/updated; read usedPercent, windowDurationMins, and
resetsAt without exposing secrets.

## Reviewed deterministic revision

Prepare an evidence-bound manifest and explicit input, then run
lingtai-tui presets revise --manifest PATH --input PATH --mode dry-run|check|apply
[--output-dir PATH]. Review the JSON plan, use dry-run/check before apply, and
apply only to a new explicit output directory. revision.go validates hashes,
route bindings, and evidence and preserves unowned bytes. The
constructor/default stays at gpt-5.6-sol behind the exact-route availability
gate.

Maintenance: If the relevant TUI preset/page is revised, check whether this sub-skill also needs revision and, if so, include it in the same PR.

## Operations

For save, endpoint/capability, availability, activation/refresh, or
troubleshooting, use the five shared operation children under
`reference/operations/`; for live OAuth quota/rate limits, use
`reference/operations/endpoint-capabilities/SKILL.md` and never expose auth
paths or tokens. This child owns Codex preset facts.

## Purchased Codex credits

In either the terminal or desktop preset editor, select **codex** and turn
**Paid Codex credits** On to allow requests after included ChatGPT usage runs
out. The shared field is `manifest.llm.codex_allow_credits: true` (JSON boolean).
It defaults Off, including for existing presets; Off omits the field. Saved
presets retain the choice. Switching away from Codex clears it. This is for
purchased Codex credits on the signed-in account, not an OpenAI API balance,
and requires a kernel that implements the option.

The kernel checks included usage before requests with the option Off. If usage
is exhausted or unverifiable, that turn stops; retry after reset or service
recovery, or explicitly opt in. On uses the same Codex connection and lets
OpenAI decide allowance, credits, debit, and spending limits. The local check
cannot guarantee zero charges when usage changes during a request, and does
not prioritize credits ahead of included allowance. External pools through
`openai` manage their own billing policy.
