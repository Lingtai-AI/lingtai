---
name: preset-skill-codex-pool
description: Official-source-led manual for the TUI `codex-pool` template, including the codex-auth-pool.json format and manual-edit protocol.
version: 2.1.0
last_changed_at: "2026-07-19T12:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# `codex-pool`

`codexPoolPreset()` (`tui/internal/preset/preset.go:1178-1208`) ships
provider `codex-pool`, exact model `gpt-5.6-sol`,
`https://chatgpt.com/backend-api/codex`, `thinking: xhigh`, and an empty
`api_key_env` because it selects from local ChatGPT OAuth token files. The
manifest declares `vision` parity with `codex`; pooling changes account
selection, not the model or endpoint.

## Template-specific settings

That manifest parity requires the companion kernel pool-vision dispatch support.
Kernel versions before the pool-vision fix can silently skip this capability
because `codex-pool` was absent from the vision-provider allow-list. Do not
claim current-main runtime parity before that companion support is merged and
verified. No separate official vision MCP is established; a native failure must
remain visible rather than falling back to generic OpenAI, guessing credentials,
or auto-loading an MCP.

Read the official [Codex authentication](https://developers.openai.com/codex/auth)
and [Codex models](https://developers.openai.com/codex/models) pages on demand.
Verify the template’s model, endpoint, and capability fields in TUI source;
never document pool membership or OAuth token contents here.

To check live OAuth quota/rate-limit state for **one pooled account**,
query that account's app-server directly: complete `initialize`, then send
the `account/rateLimits/read` request (params are structurally `null`),
and read the `GetAccountRateLimitsResponse`. This is per-account —
querying one pooled account's rate limits says nothing about the other
accounts in the pool, and the pool file itself carries no rate-limit or
credit data (it is refs + integer weights only, see below). Optionally
watch `account/rateLimits/updated` as a rolling supplement, never a
substitute. Full field-by-field routing, the official-272K-vs-measured-372K
distinction, and secret-safety rules live in
`reference/operations/endpoint-capabilities/SKILL.md` — do not restate or
re-derive that evidence here.

## The pool file: `codex-auth-pool.json`

The kernel's `codex-pool` provider reads a NON-SECRET pool file that lists the
Codex OAuth token files to load-balance across, each with an integer weight.
The pool file is the single source of truth for load balancing — presets do
not encode weights, and enabling the pool never rewrites saved presets.

**Default path:** `$LINGTAI_TUI_DIR/codex-auth-pool.json` when
`LINGTAI_TUI_DIR` is set, else `~/.lingtai-tui/codex-auth-pool.json`.
(TUI: `tui/internal/tui/codex_pool_store.go:97-102`, `107-112`. Kernel:
`auth/codex_pool.py:72-95`.)

### v1 — flat

```json
{"version": 1, "accounts": [{"path": "codex-auth.json", "weight": 1}, ...]}
```

### v2 — exact-model classified (hand-authored)

```json
{"version": 2, "models": {"<exact model>": [{"path": ..., "weight": ...}, ...], ...}}
```

A top-level `models` dict maps an exact, case-sensitive model string to an
account list of the same entry shape as the flat v1 `accounts` list. **Presence
of the `models` key is what classifies the pool — not its size, and not the
`version` field.** An empty `{}` still classifies (every model then falls back
to the legacy token); `models: null` is not a dict and leaves the pool flat.
The kernel keys off structure, never off `version`. When `models` is present
it is the sole source of truth and any `accounts` list in the same file is
ignored. There is no prefix, family, wildcard, or default matching: a model
with no exact category behaves like an unusable pool (legacy fallback). Flat
v1 files keep byte-identical behavior for every model.
(Kernel: `auth/codex_pool.py:117-180`.)

**Classified pools refuse flat TUI edits.** The TUI has no category editor
yet — it round-trips `models` losslessly, stamps version 2, and refuses flat
+/-/0 weight edits on a classified pool (`errCodexPoolModelClassified`) so it
can never destroy a hand-authored classification or write an entry the kernel
would ignore. Edit a classified pool by hand.
(`tui/internal/tui/codex_pool_store.go:64-70`, `301-306`;
`tui/internal/tui/login.go:653-661`.)

### Refs, weights, and disabled accounts

- `path` refs are TUI-dir-relative where possible: the legacy file serializes
  as `"codex-auth.json"`, per-account files as `"codex-auth/<slug>.json"`.
  Files outside the TUI dir fall back to a `"~/"`-prefixed or absolute ref.
  (`tui/internal/tui/codex_pool_store.go:114-137`.)
- The pool file stores only refs and integer weights — **never token
  contents.** Nothing in this store reads, logs, or writes token material.
- **Weight 0 means the account is present but disabled** — it stays in the
  pool file (so its membership is visible in the TUI) but the kernel excludes
  it from selection because `_coerce_weight` requires a strictly-positive
  int; a live kernel selection only ever picks among positive enabled
  weights. (`tui/internal/tui/codex_pool_store.go:73-77`;
  kernel `auth/codex_pool.py:183-197`.)
- Manual edits: a missing `weight` defaults to `1`; `enabled: false` drops
  the entry entirely rather than zero-weighting it; non-numeric or
  non-positive weights are dropped. (Kernel `auth/codex_pool.py:117-138`.)

### Deterministic, sticky selection

Selection is weighted (cumulative weights, no giant list expansion) and
**sticky within one agent wake/session**: the seed is derived from the
stable `codex_session_anchor` plus the agent's `.agent.json` `started_at`,
and deliberately **excludes `molt_count`** — an auth account must not
rotate on a molt. (Kernel `auth/codex_pool.py:200-263`.)

The model is **not** mixed into the selection seed: a flat v1 pool picks the
same account regardless of `model` (zero churn); for v2 the category list
itself already differentiates the outcome.

**Selection happens at adapter/service construction** — the kernel's
`_codex_pool` adapter factory calls `select_codex_pool_auth` when the
`codex-pool` provider is registered for a request/service, not per-turn.
Editing the pool file (or a preset) therefore does **not** reselect an
account for an already-running session; an explicit refresh, relaunch, or
new service construction is required to pick up the change.
(Kernel `_register.py:424-497`, specifically the `select_codex_pool_auth`
call at line 436 inside the `_codex_pool` factory registered at 496-497.)

**Configured weights are inputs, not measured shares.** A weight of 3 vs 1
biases the deterministic hash-based pick toward the heavier account across
many distinct sessions; it is not a live traffic-split percentage for any
single session, and a small number of sessions can deviate from the nominal
ratio.

**Invalid or missing pool falls back to the legacy token.** A missing file,
unreadable file, malformed JSON, non-dict/non-list structure, or a
classified pool with no exact-model category all yield no valid accounts,
and `select_codex_pool_auth`/`select_codex_pool_auth_path` return `None` — the
caller then falls back to the legacy single-account `codex-auth.json` path,
exactly the same fallback the single-account `codex` preset uses.
(Kernel `auth/codex_pool.py:266-341`.)

## Manual pool-file edit protocol

A hand edit to `codex-auth-pool.json` (e.g. to add a v2 `models`
classification) requires all of the following, matching the destructive-edit
discipline used elsewhere in this codebase:

1. **Exact authorization** for the specific edit — do not generalize consent
   from an earlier unrelated approval.
2. **Timestamped backup** of the file before writing (mirrors the
   `codex-auth-pool.json.bak_*` convention already used for this file).
3. **Exact-old-value or hash gate** — verify the current on-disk content
   matches what was last read before overwriting, so a concurrent writer's
   change is not silently clobbered.
4. **Same-directory temp file + atomic rename** to replace the pool file —
   never a truncate-in-place write, so a crash mid-write cannot leave a
   half-written pool file that then falls back unpredictably.
5. **Validate with the actual live `load_codex_auth_pool`** (not a
   hand-rolled JSON-shape check) before treating the edit as active,
   since that function is the authority on what counts as a valid entry.
6. **Preserve the original file on any validation failure** — restore from
   the timestamped backup rather than leaving a partially-applied edit.

**Never print token/auth contents or absolute auth paths** while performing
or reporting on this edit — refs and weights are the only pool-file content
safe to display; an absolute path can reveal machine-local layout. Do not put
machine-local absolute paths (of the pool file itself or of an account file)
in this skill's portable frontmatter or body — describe locations by the
`$LINGTAI_TUI_DIR`-relative rule above instead.

## Display and evidence

Safe display order for an account: label → email → default/slug (never the
raw token). (`tui/internal/tui/login.go:171-201`.)

Evidence anchors:

- TUI pool store: `tui/internal/tui/codex_pool_store.go:11-330`
- TUI login/credentials wiring: `tui/internal/tui/login.go:171-201,285-299,603-702`
- Live kernel logical sources: `auth/codex_pool.py:72-323`,
  `_register.py:424-497`

## Operations

For base URL/API-compat/model/capability declaration shape versus
credentials, and the full Codex OAuth quota-inspection routing (exact
app-server sequence, secret-safe fields, official-vs-measured context
window), see `reference/operations/endpoint-capabilities/SKILL.md`. For
why saving a pool-bound preset no longer runs a live availability check,
see `reference/operations/availability-save-gate/SKILL.md`. For what an explicit
refresh actually does (and does not) reselect, see
`reference/operations/activation-session-refresh/SKILL.md`.
