# SKILL.md — Preset model lists & provider integration

Bookkeeping notes for keeping `providerModels` and friends in
`preset_editor.go` aligned with each curated catalog's real-world lineup. Read
this **before** editing those maps so you don't bork an agent network with a
typo or a retired model.

**Codex is no longer maintained through this manual process.** Since
2026-09-30 Codex's model row is per-editor-instance state fetched from a
public directory, cached, and offline-fallback-protected — see
`tui/CONTRACT.md`'s "Model list curation" → "Codex exception" for the full
binding contract (display_name filter, slug/label split, cache, refresh
preservation, and the `c` custom-entry key). Everything below this point that
talks about hand-curating a Codex list (the authoritative-source row, Rule 0's
scope, and "When you add/remove a model") is retained as historical rationale
for WHY the old static entries were chosen, not as a live maintenance
procedure — do not add a new hardcoded Codex generation here. The Claude Code
CLI-alias guidance below is unaffected and still current.

## What this file is for

LingTai ships four provider families (`editorProviders`: openai, anthropic,
codex, claude-code). Only one still carries a STATIC package-level model
catalog in `providerModels`: the Claude Code CLI aliases. Codex's lineup is
public-directory-sourced per-editor state (`PresetEditorModel.codexModels`,
see the note above) rather than a `providerModels` entry. The `openai` and
`anthropic` families point at arbitrary endpoints (the official APIs, other
vendors' compatible endpoints, or account pools), so their model row is free
text and there is no list to keep current. Every static-catalog/free-text
decision calls `modelOptions(provider)`; Codex additionally consults
`(*PresetEditorModel).modelSlugOptions`/`modelDisplayOptions` for its own
per-instance lineup.

Drift in a curated catalog causes one of two failures:

- **Silent staleness:** the picker doesn't show a model the user knows exists, so they have to free-text edit it. Annoying, recoverable.
- **Loud breakage:** the picker offers a model the provider has retired or doesn't actually serve on our chosen endpoint. Agents pick it from the list, get 4xx/5xx, escalate to STUCK/AED. The user blames Lingtai, not OpenAI.

The second failure mode is what this file exists to prevent.

## Service tier and reasoning rows

`serviceTierOptions` (`preset_editor.go`) is the `normal | fast` vocabulary
(`fast` is sent as `priority`). The row exists only for the families that
accept it — `openai` and `codex` (`llmHasServiceTier`) — and is hidden for
`anthropic` and `claude-code`. A missing or `normal` value displays as `normal`
and omits `manifest.llm.service_tier` on commit; `fast` persists as the string
`"fast"`; unknown legacy values display as `normal` and are dropped on commit.
The TUI performs no endpoint support check.

Reasoning effort: `openai` and `anthropic` use `levelThinkingOptions`
(`default | none | minimal | low | medium | high | xhigh`, where `default`
omits the field); Codex keeps `codexThinkingOptions` (`low..xhigh`) with an
explicit `xhigh` default; `claude-code` has no reasoning row.

## Authoritative sources per curated catalog

| Provider | Canonical list | Cadence | Notes |
|---|---|---|---|
| `codex` | `config.CodexPublicModelsURL` (the openai/codex public directory) | Live, per editor entry | No longer hand-curated here — fetched/cached/fallback per `tui/CONTRACT.md`'s Codex exception. The historical `https://developers.openai.com/codex/models` source below documents WHY the old static entries existed, not a list to keep updating by hand. |
| `claude-code` | the installed `claude` CLI's model-selection help | On CLI releases | CLI aliases (`opus`/`fable`/`sonnet`/`haiku`), not dated API ids — still hand-maintained |

Historical note (pre-2026-09-30 static catalog): the old hand-maintained Codex
list was sourced from https://developers.openai.com/codex/models and
deliberately never from `https://platform.openai.com/docs/models` — the
standard API list, which includes models the codex backend
(`chatgpt.com/backend-api/codex/responses`) doesn't accept (e.g.
`gpt-5.5-pro` 4xx's on the codex endpoint). The new public-directory fetch
applies its own binding filter instead (display_name starts with `GPT`,
case-sensitive) and does not re-derive this docs-page distinction — a
returned entry is a suggestion, not a served-today guarantee, for exactly
the reasons this paragraph used to guard against by hand.

## Curation rules

**Rule 0 — latest two generations only, for STATIC catalogs.** `tui/CONTRACT.md` ("Model list curation") caps every family still governed by a package-level catalog at its latest two generations. Today that means Claude Code's CLI aliases only — Codex is the documented exception (public-directory-sourced, no generation/visibility filter). Adding a new generation to a still-governed family is the same change that removes the third-newest. Variants inside a generation (e.g. `-mini`-style suffixes) are not generations and all stay. Read that section before touching `providerModels`; the checklist below decided inclusion for the retired static Codex catalog and still applies to any future static catalog.

Never add a catalog for `openai` or `anthropic`: their endpoint is whatever
the user configured, so no list can be correct for everyone. When the user
switches provider, `switchProvider` replaces a curated id the new family cannot
serve with that family's first entry (or clears it for a free-text family);
arbitrary user text is never rewritten.

For each candidate model, decide inclusion against this checklist:

1. **Is it served on our endpoint?** Codex uses `/backend-api/codex/responses`. If a model is listed in OpenAI's general API docs but not in the Codex docs page above, **exclude it**. Same logic for any other provider where we use a non-standard endpoint.
2. **Is it stable, not preview/research?** Skip "Research Preview" / "Beta" tiers — they get yanked without notice and our list rots. Example: `gpt-5.3-codex-spark` is currently a Research Preview, so it's omitted.
3. **Is its vision capability documented?** Templates route vision through `vision: inherit` and the TUI keeps no per-model vision table, so record the evidence in the provider manual (`internal/preset/skills/lingtai-preset-skill/reference/<name>/SKILL.md`) rather than in code. Don't guess.
4. **Will it 401 on a free tier or rollout gate?** `gpt-6-astra` is
   documented, but exact authenticated Codex OAuth availability is not proven
   for every account. Keep it picker-visible with metadata, retain
   `gpt-5.6-sol` as the default-first/native default, and mention the gate
   beside the entry. Do not silently promote it.

## Why some models you might expect are missing (historical — pre-2026-09-30 static catalog)

This section explains the reasoning behind the OLD hand-picked Codex list; it
is no longer an active exclusion process (the public-directory fetch applies
only the display_name/`GPT`-prefix filter, not this reasoning) but is kept so
the historical defaults in `config.DefaultCodexModelOptions()` remain
explainable.

- **`gpt-5.5-pro`** — existed in OpenAI's standard API at `/api/docs/models/gpt-5.5-pro` ($30/$180 per 1M tokens), was available in ChatGPT for Pro/Business/Enterprise, but was **not listed under Codex models**. Adding it would have caused 4xx on the codex endpoint. Excluded from the old static list.
- **`gpt-5.3-codex-spark`** — Research Preview as of 2026-05. Excluded from the old static list until promoted to GA.
- **`o3-pro` / `o4-mini` / older o-series** — none were in the Codex CLI catalog the old static list was drawn from.

## When you add a new model (STATIC catalogs only — not Codex)

```go
// In providerModels:
preset.ProviderClaudeCode: {"opus", "fable", "sonnet", "haiku"},
```

Order matters in `providerModels` only for the picker UX — left-to-right is the cycle order with ←/→. Putting the desired default first keeps fresh templates and the picker aligned. Existing saved presets keep whatever model they already declared — that's a feature, not a bug. Codex has no equivalent step: its default is `config.DefaultCodexModelOptions()[0]` (`tui/internal/config/codex_public_models.go`), and `templates/codex.json` (built from `preset.go:codexPreset()`) pins that same offline-fallback default; the live picker lineup itself is never hand-edited.

## When you remove a retired model (STATIC catalogs only — not Codex)

1. Remove from `providerModels` (and from any provider manual that enumerates the lineup).
2. **Don't** scan saved presets and rewrite their `llm.model`. Users may have very specific reasons for pinning. Migrating their saved/ files silently is worse than letting them hit the 4xx and choose for themselves. (If we ever do migrate, it's an explicit user-confirmed step — not a startup hook.)

Codex has no "remove" step here: a model falling out of the live public
directory simply stops appearing in a future refresh's candidate list (see
the Codex exception in `tui/CONTRACT.md`) — there is no static entry to edit,
and a saved/custom id already in use keeps working and keeps rendering
exactly like the "don't rewrite saved presets" rule above.

## Codex preset specifics

Codex is the odd one out — it uses ChatGPT OAuth instead of an API key. A few things only apply to it:

- **`api_key_env: ""`** in the preset. Don't change to a placeholder env var name; the kernel's `_codex` factory in `lingtai-kernel/src/lingtai/llm/_register.py` ignores `api_key` entirely and reads the OAuth token from the account file named by `manifest.llm.codex_auth_path`, falling back to the legacy `~/.lingtai-tui/codex-auth.json` when that field is absent/empty.
- **Multiple accounts via `codex_auth_path`.** A codex preset may bind to a specific ChatGPT account by setting the non-secret `manifest.llm.codex_auth_path` to a token file (e.g. `~/.lingtai-tui/codex-auth/work.json`). Additional accounts are added from Setup → Credentials (`listCodexAccounts` / `newCodexAuthPath` in `codex_auth_store.go`); the editor's API-key row doubles as an account selector (←/→) for codex. An empty/absent field means the legacy single-account file — existing presets keep working unchanged. Token files are 0600 and never logged.
- **`base_url: "https://chatgpt.com/backend-api/codex"`** — note the `/codex` suffix. Without it, requests hit `/backend-api` (the generic ChatGPT backend) and fail with HTML / Cloudflare responses. Source: `lingtai-kernel/discussions/codex-oauth-stateless-patch.md`.
- **No model picker on `stepPresetKey`.** The codex flow used to render a model strip on the API-key page; that picker was removed in 2026-05 in favor of the standard editor model row. The first-run wizard's stepPresetKey for codex is now pure OAuth-status display. If you find yourself wanting to add a picker there again, you've hit a different bug — fix the editor instead.
- **Two login methods, one completion path.** Codex login first shows a method chooser: browser OAuth/localhost for same-machine use, or device code for remote/headless use. `CodexOAuthDoneMsg` writes the token bundle after either method completes; stale completions are epoch-gated so cancelled attempts cannot overwrite `codex-auth.json`.
- **Empty email is valid.** OpenAI's id_token JWT sometimes ships without the profile claim. We treat `RefreshToken != ""` as the canonical "session is usable" signal and fall back to `(logged in)` for display. Don't gate any logic on `Email != ""`.

## Verification when touching the Codex public-directory fetch

There is no "codex list" to bump by hand any more. When you touch
`tui/internal/config/codex_public_models.go` or the per-editor wiring in
`preset_editor.go` instead:

1. **Build/unit test:** `cd tui && go vet ./... && go test ./...` — the parser
   filter, dedupe, cache, and timeout/error fallback tests are hermetic
   (injected transport/bytes, `t.TempDir()`), never a live endpoint.
2. **Manual:** open the preset editor on the codex template; the model row
   should show the compiled-in offline fallback immediately, then update in
   place (same slugs persisted, labels possibly refreshed) once the
   background fetch completes. ←/→ still cycles; `c` still opens a free-text
   custom entry.
3. **Live spot-check (optional, not a unit test):** confirm the fixed URL
   still serves the expected shape and that a live-fetched slug you select
   actually runs — the directory is a suggestion, not a guarantee, so a 4xx
   on a specific account is expected behavior, not a bug to chase by editing
   a filter.
4. **Don't** add a numeric-generation, visibility, or entitlement filter back
   in — the human-authorized contract (`tui/CONTRACT.md`, "Codex exception")
   is display_name-prefix-only on purpose.

## Cross-references

- `preset_editor.go` — `editorProviders`, `providerModels`, `modelOptions`, `switchProvider` (static/free-text catalogs)
- `preset_editor.go` — `codexModels` field, `modelDisplayOptions`/`modelSlugOptions`/`isCuratedModelSlug`, `codexPublicModelsCmd`/`codexPublicModelsMsg`, `Init()` (per-editor Codex public-directory state and its refresh)
- `preset_editor.go` — `mandatoryCapRow` (fixed, informational capabilities rendering)
- `tui/internal/config/codex_public_models.go` — fetch/parse/cache/fallback (`RefreshCodexPublicModels`, `ParseCodexPublicModels`, `DefaultCodexModelOptions`)
- `internal/preset/preset.go:codexPreset()` — built-in template, sets default model (pinned to the offline fallback's first entry)
- `firstrun.go` `startCodexLogin` — first-run Codex browser/device-code login launcher
- `firstrun.go` / `login.go` `CodexOAuthDoneMsg` handlers — save tokens after matching-epoch browser/device-code completion
- `oauth.go` — browser OAuth, device-code login, token exchange, JWT email parser
- `lingtai-kernel/discussions/codex-oauth-stateless-patch.md` — kernel-side stateless responses contract

When in doubt, search the OpenAI Codex docs and the codex-rs Rust source (https://github.com/openai/codex) for ground truth on what the chatgpt.com endpoint actually accepts.
