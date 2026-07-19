---
name: preset-skill-op-endpoint-capabilities
description: Base URL / API-compat / provider / model / capability declaration shape versus credentials and live probes; includes proven Codex OAuth quota inspection with exact agent query routing and dated official-vs-measured context-window evidence.
version: 1.1.0
last_changed_at: "2026-07-19T12:00:00Z"
related_files:
  - tui/internal/preset/preset.go
  - tui/internal/tui/model_validity.go
  - tui/internal/preset/preset_skill_router_test.go
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# Endpoint and capability declarations

Every built-in preset's `manifest.llm` declares a fixed set of fields; this
child names what each one is and is not, so provider children can link here
instead of re-explaining the shape every time.

## Declaration fields (non-credential)

- **`provider`** — the kernel adapter name (`minimax`, `zhipu`, `codex`,
  `codex-pool`, ...). Selects which adapter class handles the request; not
  itself a credential.
- **`model`** — the exact model string sent to the provider. Some
  providers (`gemini`, `claude-agent-sdk`) use a native alias or model id
  with no override surface; others (`custom`) leave it empty until
  configured.
- **`base_url`** — the endpoint the adapter calls. Native adapters
  (`gemini`, `claude-agent-sdk`) omit it entirely; OpenAI-compatible and
  gateway providers set it explicitly or leave it `nil` for
  provider-resolved defaults (`openrouter`).
- **`api_compat`** — when present (`"openai"`), tells the kernel to route
  through the OpenAI-compatible client shape rather than a native adapter.
  Absence does not mean "unsupported" — native adapters (`gemini`) simply
  don't need it.
- **`capabilities.vision` / `capabilities.web_search` / `capabilities.skills`**
  — opt-in capability declarations, not proof the underlying model actually
  performs an image/search call correctly. A capability being wired only
  means the kernel *will attempt* the corresponding route with the
  declared provider/credential; whether the live call succeeds is a
  runtime fact, not a manifest fact.

## Distinct from credentials and live probes

These declaration fields answer "what will be called and how" — they are
**distinct from**:

- **Credentials** — `api_key_env` (env-var name, not a value),
  `codex_auth_path` (which bound OAuth token file, not its contents), or
  local CLI login state (`claude-agent-sdk`). See `ResolveRefsWithAuth`
  (`tui/internal/preset/preset.go:752-836`) for how credential *validity*
  (not the declaration) is judged per-provider — keyed providers check
  `existingKeys[envName]`, `codex`/`codex-pool` check OAuth state
  (per-account when `AuthState.CodexAuthDir` is set, else the global
  `CodexOAuthConfigured` bool), `claude-agent-sdk` checks
  `ClaudeCodeAuthConfigured`.
- **Live probes** — an actual HTTP call proving the declared endpoint,
  model, and credential currently work together. See
  `reference/operations/availability-save-gate/SKILL.md` for the editor's
  live validity gate and its 429/503/529 vs hard-block classification.

A preset can be declaration-complete and credential-valid yet still fail a
live probe (provider outage, model retired, quota exhausted) — route a
"why does saving/using this fail" question to the save-gate child, not this
one, and route "what value/env-var/endpoint does this template even use"
back to the specific provider child instead of guessing here.

## Codex OAuth quota inspection

This is proven, verified evidence about the installed Codex CLI's own quota
surface — **not** an assertion that a particular TUI/kernel adapter or
running session exposes it. The direct app-server operation below remains
the source route; verify any adapter-facing convenience surface against the
running implementation instead of inferring it from this manual.

### How an agent actually queries this — the exact operation sequence

An agent that needs live Codex OAuth quota/rate-limit data must:

1. Complete the app-server `initialize` handshake first (`clientInfo`
   required; `capabilities` optional) — `account/rateLimits/read` is only
   valid after initialize, not before.
2. Send the `account/rateLimits/read` **request**. Its params are
   structurally `null` — the JSON-RPC schema fixes
   `"params": {"type": "null"}` for this method, so the call carries no
   request body beyond `id`/`method`. Do not invent request fields for it.
3. Read the response as a `GetAccountRateLimitsResponse`: a required
   `rateLimits` (backward-compatible single-bucket `RateLimitSnapshot`),
   optional `rateLimitsByLimitId` (the same snapshot shape keyed by
   `limit_id`, e.g. `"codex"`), and optional `rateLimitResetCredits`.
   Each `RateLimitSnapshot` carries `primary`/`secondary` `RateLimitWindow`
   objects (`usedPercent` required; `windowDurationMins`/`resetsAt`
   nullable), plus `planType`, `limitId`, `limitName`,
   `rateLimitReachedType`, `credits` (`CreditsSnapshot`: `hasCredits`,
   `unlimited`, nullable `balance`), and `individualLimit`
   (`SpendControlLimitSnapshot`).
4. Optionally also subscribe to the **`account/rateLimits/updated`**
   server notification for the same `AccountRateLimitsUpdatedNotification`
   shape (a `rateLimits` snapshot) — but treat it as a **sparse rolling
   merge signal, not a substitute for step 2**: per its own schema
   description, a client should merge available values into the most
   recent `account/rateLimits/read` response (or refetch), and a nullable
   field being absent in one notification does not mean it cleared a
   previously observed value.
5. Interactively, the same account-level facts are surfaced by the
   official `/status` slash command inside the Codex TUI — useful for a
   human to cross-check, not a programmatic substitute for step 2.
6. **`codex doctor --json` does NOT expose any of this.** It emits a
   redacted installation/config/auth/runtime health report with no
   rate-limit, quota, or `usedPercent`-shaped field. Do not route an agent
   there for quota.

### Secret-safe fields and limitations

- Every field above (`usedPercent`, `windowDurationMins`, `resetsAt`,
  `planType`, `limitId`, `limitName`, `rateLimitReachedType`, the
  `CreditsSnapshot`/`SpendControlLimitSnapshot` fields) is account-level
  usage/plan metadata — **none of it is a token or credential**, and none
  of it should ever be paired with the OAuth token file's contents or
  absolute path when reported back to a user.
- The response schema carries no field for the account's identity beyond
  what the existing `codex`/`codex-pool` display rules already allow
  (label → email → default/slug) — do not read account identity out of
  this response; it is a rate-limit/credits payload, not an identity one.
- `rateLimitResetCredits.credits` can be `null` (only `availableCount`
  known) even when credits exist — do not infer "no reset credits" from a
  missing `credits` array.

### Official vs. measured context-window capacity — do not conflate them

Two different numbers answer two different questions; keep them
separately qualified and never let one stand in for the other:

- **Official current metadata/changelog figure: 272K tokens.** This is
  what to cite as the vendor-published number, and it is the only number
  that may be presented as "current official Codex context window" without
  further qualification.
- **Measured live A/B boundary: ~372,000 total tokens (date/route/model/CLI-qualified).**
  This is **not** an official published limit — it is one synthetic
  probe's observed acceptance boundary and must always carry its
  qualifiers: OAuth Codex route, model `gpt-5.6-sol`, codex-cli `0.144.3`,
  low reasoning effort, synthetic filler payload, measured 2026-07-19.
  Evidence (tokenizer `o200k_base 0.12.0`, all values from live
  `turn.completed`/error events, not estimated):
  - 300,000 user tokens → **PASS** (`turn.completed.input_tokens=312684`,
    `cached_input_tokens=9984`, `output_tokens=12`,
    `reasoning_output_tokens=0`).
  - 359,000 user tokens → **PASS** (agent reply
    `CONTEXT_PROBE_359K_OK`; total input `371684`, cached `9984`, output
    `12`, reasoning `0`).
  - 360,000 user tokens → **FAIL** — "Codex ran out of room in the
    model's context window." (same-setup predicted total, if per-request
    overhead stayed constant at 12,684: `372684`).
  - 500,000 user tokens → **FAIL** — same context-window error.
  - Reading: the empirical bracket for this exact setup is **371,684 total
    tokens accepted** (359K user payload) and approximately **372,684 total
    tokens rejected** (360K user payload plus the same 12,684-token
    overhead). The shorthand **~372,000 total-token boundary** locates that
    1,000-token interval; it is not an exact measured ceiling. Subtracting
    the measured **12,684-token setup overhead** from the rounded 372,000
    shorthand yields the correction's roughly **359,316-token same-setup
    user-payload** reading, while the exact empirical claim remains only
    "359,000 passed; 360,000 failed." LingTai's 300K working default is
    safely below this measured bracket and therefore underuses the observed
    route capacity.
- **Never present 272K as live proof of anything measured, and never
  present 372K (or the ~359,316 user-payload derivation) as timeless or
  universal.** The 372K figure is one dated, route/model/CLI-qualified
  synthetic A/B result, not a vendor commitment, and it can change on any
  Codex release, account plan, or route change. Always carry the
  qualifiers when citing it; never round it into "the Codex context
  window" unqualified.

### Discovery method (read-only, no tokens touched)

The app-server request/response/notification shapes above come from the
Codex app-server's own generated JSON Schema definitions
(`GetAccountRateLimitsResponse`, `AccountRateLimitsUpdatedNotification`,
`InitializeParams`, and the `ClientRequest`/`ServerNotification` protocol
union listing the exact `account/rateLimits/read` /
`account/rateLimits/updated` method names and the request's `params: null`
shape) — read directly, not reconstructed from binary strings. The
context-window PASS/FAIL evidence above comes from a targeted, read-only
synthetic probe directory (JSON-RPC event logs plus prompt manifests
recording `tokenizer`, `verified_user_tokens`, and `sha256` per probe);
its exact machine-local path is intentionally not reproduced in this
skill's portable frontmatter/prose — describe it as "the 2026-07-19
Codex OAuth context-window probe evidence" if a pointer is needed.

For the installed-CLI facts (`codex --version` → `codex-cli 0.144.3`,
`codex doctor --json`'s actual redacted-report scope), read-only discovery
was run directly against the locally installed `@openai/codex` package —
`codex doctor --json` was executed and its output inspected for any
rate-limit/quota/`usedPercent`-shaped key (none present); `codex doctor
--help` confirms `--json` is scoped to "a redacted machine-readable
report" about installation/config/auth/runtime, not usage/quota.

Re-verify both the schema shapes and the measured boundary against the
actually-installed Codex version and current date before relying on exact
field names or the 372K figure in new code — the schema is
app-server-internal surface, not a versioned public API, and the measured
boundary is a snapshot, not a specification.

### Display contract if this is ever implemented

If an adapter/runtime consumer of this data is built later, per-OAuth
account:

- Show **`remaining = max(0, 100 - usedPercent)`** as the headline number.
- **Also retain** — do not collapse away — the reset time, window
  duration, and limit-id fields explicitly; a bare "remaining %" without
  its window/reset context is not enough to act on.
- **Never expose auth paths or tokens** while displaying this — the same
  discipline as `reference/codex-pool/SKILL.md`'s display rules (label →
  email → default/slug, never the raw path or token).

This section documents the direct Codex source operation independently of
any adapter convenience surface. If the running adapter exposes equivalent
quota telemetry, verify its fields and version against this source contract;
otherwise use the exact app-server sequence above. See
`reference/codex/SKILL.md` and `reference/codex-pool/SKILL.md` for the
provider-specific routing and account-safety boundaries.

## Operations

For the live probe that actually tests these declarations against a real
provider call, see `reference/operations/availability-save-gate/SKILL.md`.
