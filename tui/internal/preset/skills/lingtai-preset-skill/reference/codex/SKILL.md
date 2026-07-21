---
name: preset-skill-codex
description: Canonical, official-source-led manual for the TUI Codex templates (`codex`, `codex-pool`) and the kernel's unified Codex provider — default dynamic account selection, fixed-account override, compatibility aliases, and safe observability.
version: 3.0.0
last_changed_at: "2026-07-20T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# `codex`

**One kernel provider, one factory, two TUI templates.** The kernel serves
`codex`, `codex-pool`, and `codex_pool` as settings/provider aliases into a
single `_codex` factory — there is one native Codex adapter, not separate
adapters, sessions, or retry implementations per alias. The TUI ships two
built-in templates that both route into this one provider: `codexPreset()`
(`tui/internal/preset/preset.go:1148-1176`) and `codexPoolPreset()`
(`tui/internal/preset/preset.go:1178-1208`). Both use model `gpt-5.6-sol`,
the Codex endpoint `https://chatgpt.com/backend-api/codex`, `thinking: xhigh`,
and ChatGPT OAuth rather than an API key env-var; the manifest exposes
provider-native `vision` and web search on both. This manual is the single
canonical Codex behavior description — `reference/codex-pool/SKILL.md` is a
short compatibility redirect here, not a second source of truth.

## Default: dynamic per-request account selection

With no explicit `codex_auth_path` set on the preset, the kernel provider
uses a `WeightedAccountSource` and **dynamically selects one account at each
real provider-send boundary** — not once at chat/session construction. Chat
construction and any rebuild/replay consume no account draw; only an actual
send to the provider selects. This is the default for both the `codex` and
`codex-pool` templates when neither carries a bound `codex_auth_path` — the
weighted pool (see `codex-auth-pool.json` below) is live-consulted per send,
not fixed for the life of a session. When a complete, comparable quota snapshot
exists, effective weight is `configured weight × remaining-quota fraction`; if
any eligible account lacks a comparable value, the whole draw falls back to
configured weights rather than mixing stale and fresh quota data.

Changing the selected account across sends resets the authenticated WS epoch
and its `previous_response_id` continuation before the new account is used. The
prompt-cache key remains stable across the switch. Inspect live ledger/cache
evidence when diagnosing a deployment; do not turn one measured cache rate or
calls/min sample into a permanent product guarantee.

## Fixed-account override

Setting an explicit `codex_auth_path` on the preset (the single-account
`codex` template's per-account binding; see
`reference/operations/endpoint-capabilities/SKILL.md` for how
`ResolveRefsWithAuth` judges its credential validity) pins the provider to
that one account for every send — this is the override, not the default.
Absent an explicit path, the provider falls back through
`WeightedAccountSource` as described above, never to a silently-fixed single
account chosen once and reused.

## What the kernel owns vs. what Codex itself owns

Selecting an account per send is the kernel provider's job. Once a send is
underway, ordinary Codex owns its own history, client, token refresh,
REST/WS transport, cache, compaction, service tier, and built-in self-heal —
the kernel does not reimplement these per alias. The kernel's outer AED layer
owns retry, rebuild, and replay across sends; it does not own what happens
inside one Codex call. Do not describe per-alias retry or session logic here
that the kernel does not actually carry — verify any such claim against
kernel source before repeating it in this manual.

## `service_tier` / fast tier

`service_tier: fast` is user-facing LingTai configuration; the kernel
normalizes it onto the Codex wire field `priority`. Treat `service_tier` as
the name to use when discussing or configuring this from the TUI/preset
side; `priority` is the wire-level detail, not a separate user-facing knob.

## Pool exhaustion and failure semantics

A **nonempty but exhausted** pool (every account present but currently
unusable) fails closed — it does not silently fall back to the legacy
single-account token. Legacy fallback is reserved for a **truly empty**
pool (no valid accounts at all): a missing file, unreadable file, malformed
JSON, or a classified pool with no exact-model category. Do not conflate
"exhausted" with "empty" when describing or triaging a Codex failure; they
have different fallback outcomes.

## Safe per-call observability

Safe per-call metadata/ledger fields for account selection may include the
account's sha8, its source index within the pool, and its configured weight
— never plaintext token contents, the auth file path, the account email, or
any other credential material. Current quota/rate-limit metadata is availability-dependent, not a promised
field on every response. Merged source can emit `codex_pool_quota_left` for a
dynamic selection when its quota snapshot is complete, but it is not yet
consistently present for every real request: fixed-account binding does not yet
probe it, and narrower ledger/event projections may omit it. A kernel follow-up
will make the selected account's realtime balance ratio consistent in both
dynamic and fixed modes. Until that merges, do not promise per-row coverage or
present configured `weight` as if it were account balance.

## Account-pool / auth setup ownership

The account pool file, its format, weights, and the manual-edit protocol are
owned by `reference/codex-pool/SKILL.md` — read it for
`codex-auth-pool.json`'s v1/v2 shape, weight-0 disable semantics, and the
destructive-edit discipline for hand-editing it. This canonical manual
describes selection *behavior* against that pool; it does not restate the
pool file's format.

## Template-specific settings

Exact image support can depend on the current model/account; verify it
rather than treating this manual as a promise. Read the official
[Codex authentication](https://developers.openai.com/codex/auth) and
[Codex models](https://developers.openai.com/codex/models) pages on demand.
No separate Codex vision MCP is established. If the native route fails,
report the failure and use this manual for discovery; do not fall back to a
generic OpenAI key, switch providers, or auto-load/invoke an MCP. Recheck the
TUI preset source for model and capability changes. Never inspect, print, or
reproduce OAuth token contents.

Kernel source of truth lives in the separate `lingtai-kernel` repository:
`src/lingtai/llm/_register.py::_codex` owns the aliases and fixed-vs-weighted
source choice; `src/lingtai/auth/codex_account_source.py` owns pool snapshots and
weight arithmetic; and
`src/lingtai/llm/openai/adapter.py::{_select_codex_account,send_stream}` owns the
per-request bind, quota lookup, exclusion/failover, and WS reset. The unified
architecture landed in [kernel PR #1003](https://github.com/Lingtai-AI/lingtai-kernel/pull/1003).
Verify those symbols before changing exact behavior claims; TUI source alone
cannot prove kernel behavior.

## Operations

For base URL/API-compat/model/capability declaration shape versus
credentials, see `reference/operations/endpoint-capabilities/SKILL.md`.

To check live OAuth quota/rate-limit state for one account, query the
app-server directly: complete `initialize`, then send the
`account/rateLimits/read` request (its params are structurally `null` — no
request body), and read the `GetAccountRateLimitsResponse`
(`usedPercent`/`windowDurationMins`/`resetsAt` per window, plan/credits
fields). Optionally also watch the sparse `account/rateLimits/updated`
notification as a rolling supplement, never a substitute for the read. Under
dynamic selection this is inherently per-account, not per-preset — querying
one account's rate limits says nothing about another account the same
preset may select on its next send. Full field-by-field routing, the
official-272K-vs-measured-372K distinction, and secret-safety rules live in
`reference/operations/endpoint-capabilities/SKILL.md` — do not restate or
re-derive that evidence here.

For the availability/save gate this preset still goes through, see
`reference/operations/availability-save-gate/SKILL.md` — note its documented
`codex-pool`-vs-`oauthProviders` gap is a TUI-side Save-editor fact, not a
kernel routing fact, and is unaffected by anything in this manual. For what
an explicit refresh actually does (and does not) reselect, see
`reference/operations/activation-session-refresh/SKILL.md`.
