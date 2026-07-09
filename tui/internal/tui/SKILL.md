# SKILL.md — Preset model lists & provider integration

Bookkeeping notes for keeping `providerModels`, `modelHasVision`, and friends in `preset_editor.go` aligned with each provider's real-world catalog. Read this **before** editing those maps so you don't bork an agent network with a typo or a retired model.

## What this file is for

Each LLM provider in `providerModels` (preset_editor.go:134) feeds a model picker on the preset editor's model row. When a user clones a template and cycles ←/→ on the model row, the candidates come straight from this map. The map is the source of truth.

Drift here causes one of two failures:

- **Silent staleness:** the picker doesn't show a model the user knows exists, so they have to free-text edit it. Annoying, recoverable.
- **Loud breakage:** the picker offers a model the provider has retired or doesn't actually serve on our chosen endpoint. Agents pick it from the list, get 4xx/5xx, escalate to STUCK/AED. The user blames Lingtai, not OpenAI.

The second failure mode is what this file exists to prevent.

## Authoritative sources per provider

| Provider | Canonical list | Cadence | Notes |
|---|---|---|---|
| `minimax` | https://platform.minimaxi.com/document/Models | Quarterly | M-series, current = 2.7 |
| `zhipu` | https://docs.bigmodel.cn/cn/guide/models | Quarterly | GLM-5.x family |
| `mimo` | https://www.xiaomi-ai.com/cn/models | Quarterly | Xiaomi MiMo |
| `deepseek` | https://api-docs.deepseek.com/quick_start/pricing | Quarterly | DS-V4 family |
| `codex` | https://developers.openai.com/codex/models | Monthly | ChatGPT-OAuth only — not the standard OpenAI API list |

For codex specifically, **do not** consult `https://platform.openai.com/docs/models`. That's the standard API model list, which includes models the codex backend (`chatgpt.com/backend-api/codex/responses`) doesn't accept (e.g. `gpt-5.5-pro` exists in the standard API but 4xx's on the codex endpoint).

## Curation rules

For each candidate model, decide inclusion against this checklist:

1. **Is it served on our endpoint?** Codex uses `/backend-api/codex/responses`. If a model is listed in OpenAI's general API docs but not in the Codex docs page above, **exclude it**. Same logic for any other provider where we use a non-standard endpoint.
2. **Is it stable, not preview/research?** Skip "Research Preview" / "Beta" tiers — they get yanked without notice and our list rots. Example: `gpt-5.3-codex-spark` is currently a Research Preview, so it's omitted.
3. **Is its vision capability documented?** Visit the model's page, find the "Modalities" / "Input" section, record `true`/`false` in `modelHasVision`. Don't guess — `gpt-5.3-codex` accepts images, which surprised us.
4. **Will it 401 on a free tier?** Some models (`gpt-5.5` for codex) require ChatGPT Plus or higher. We still list them — the user discovers their tier on first use rather than the picker hiding the option. But mention any subscription gate in the comment next to the entry.

## Why some models you might expect are missing

- **`gpt-5.5-pro`** — exists in OpenAI's standard API at `/api/docs/models/gpt-5.5-pro` ($30/$180 per 1M tokens), is available in ChatGPT for Pro/Business/Enterprise, but **is not listed under Codex models**. Adding it would cause 4xx on the codex endpoint. Excluded.
- **`gpt-5.3-codex-spark`** — Research Preview as of 2026-05. Excluded until promoted to GA.
- **`o3-pro` / `o4-mini` / older o-series** — none are in the Codex CLI catalog. Codex serves the GPT-5.x line only.

## When you add a new model

```go
// In providerModels:
"codex": {"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna", "gpt-5.5", ...}, // default first

// In modelHasVision:
"gpt-5.6-sol":   true, // keep named routes aligned with the verified GPT-5.6 family
"gpt-5.6-terra": true,
"gpt-5.6-luna":  true,
```

Order matters in `providerModels` only for the picker UX — left-to-right is the cycle order with ←/→. Putting the desired default first keeps fresh templates and the picker aligned. The `templates/codex.json` (built from `preset.go:codexPreset()`) should also have its `llm.model` bumped when you change that default. Existing saved presets keep whatever model they already declared — that's a feature, not a bug.

## When you remove a retired model

1. Remove from `providerModels`.
2. Remove from `modelHasVision`.
3. **Don't** scan saved presets and rewrite their `llm.model`. Users may have very specific reasons for pinning. Migrating their saved/ files silently is worse than letting them hit the 4xx and choose for themselves. (If we ever do migrate, it's an explicit user-confirmed step — not a startup hook.)

## Codex preset specifics

Codex is the odd one out — it uses ChatGPT OAuth instead of an API key. A few things only apply to it:

- **`api_key_env: ""`** in the preset. Don't change to a placeholder env var name; the kernel's `_codex` factory in `lingtai-kernel/src/lingtai/llm/_register.py` ignores `api_key` entirely and reads the OAuth token from the account file named by `manifest.llm.codex_auth_path`, falling back to the legacy `~/.lingtai-tui/codex-auth.json` when that field is absent/empty.
- **Multiple accounts via `codex_auth_path`.** A codex preset may bind to a specific ChatGPT account by setting the non-secret `manifest.llm.codex_auth_path` to a token file (e.g. `~/.lingtai-tui/codex-auth/work.json`). Additional accounts are added from Setup → Credentials (`listCodexAccounts` / `newCodexAuthPath` in `codex_auth_store.go`); the editor's API-key row doubles as an account selector (←/→) for codex. An empty/absent field means the legacy single-account file — existing presets keep working unchanged. Token files are 0600 and never logged.
- **`base_url: "https://chatgpt.com/backend-api/codex"`** — note the `/codex` suffix. Without it, requests hit `/backend-api` (the generic ChatGPT backend) and fail with HTML / Cloudflare responses. Source: `lingtai-kernel/discussions/codex-oauth-stateless-patch.md`.
- **No model picker on `stepPresetKey`.** The codex flow used to render a model strip on the API-key page; that picker was removed in 2026-05 in favor of the standard editor model row. The first-run wizard's stepPresetKey for codex is now pure OAuth-status display. If you find yourself wanting to add a picker there again, you've hit a different bug — fix the editor instead.
- **Two login methods, one completion path.** Codex login first shows a method chooser: browser OAuth/localhost for same-machine use, or device code for remote/headless use. `CodexOAuthDoneMsg` writes the token bundle after either method completes; stale completions are epoch-gated so cancelled attempts cannot overwrite `codex-auth.json`.
- **Empty email is valid.** OpenAI's id_token JWT sometimes ships without the profile claim. We treat `RefreshToken != ""` as the canonical "session is usable" signal and fall back to `(logged in)` for display. Don't gate any logic on `Email != ""`.

## Verification when bumping the codex list

After editing `providerModels["codex"]` / `modelHasVision`:

1. **Build:** `cd tui && go vet ./... && go test ./... && make build`
2. **Manual:** open the preset editor on the codex template, cycle through models with ←/→. Each one should render in the model row, the vision row should toggle correctly with the model.
3. **Live test:** restart an agent on each model in the new list. If you can't run all of them, at least run the new latest and the previous default to confirm the codex endpoint accepts both.
4. **Don't** assume the docs page is canonical for the Codex backend's actual acceptance. The docs sometimes list models still rolling out. If a model 4xx's, it's not in your account yet — leave it in the list (it'll work for users who have it) but note the rollout status in the comment.

## Cross-references

- `preset_editor.go:120` — `providerModels` map
- `preset_editor.go:178` — `modelHasVision` map
- `preset_editor.go:106` — `capabilityProviderOptions` (web_search, vision provider routing)
- `internal/preset/preset.go:codexPreset()` — built-in template, sets default model
- `firstrun.go` `startCodexLogin` — first-run Codex browser/device-code login launcher
- `firstrun.go` / `login.go` `CodexOAuthDoneMsg` handlers — save tokens after matching-epoch browser/device-code completion
- `oauth.go` — browser OAuth, device-code login, token exchange, JWT email parser
- `lingtai-kernel/discussions/codex-oauth-stateless-patch.md` — kernel-side stateless responses contract

When in doubt, search the OpenAI Codex docs and the codex-rs Rust source (https://github.com/openai/codex) for ground truth on what the chatgpt.com endpoint actually accepts.
