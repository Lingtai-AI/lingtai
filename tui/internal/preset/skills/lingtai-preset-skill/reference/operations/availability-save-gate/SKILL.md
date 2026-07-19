---
name: preset-skill-op-availability-save-gate
description: Why Save on the preset editor blocks or warns — the live validity probe, its fingerprint, and the 429/503/529 retryable-vs-hard-block split.
version: 1.0.0
last_changed_at: "2026-07-19T00:00:00Z"
related_files:
  - tui/internal/tui/preset_editor.go
  - tui/internal/tui/model_validity.go
  - tui/internal/tui/doctor.go
  - tui/internal/preset/preset_skill_router_test.go
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# Availability / save gate

Evidence: `tui/internal/tui/preset_editor.go:1263-1369`, `tui/internal/tui/model_validity.go`.

## `Validate()` runs first

`commit()` (`preset_editor.go:1263-1333`) runs the preset's structural
`Validate()` before anything else; a structural failure blocks Save
immediately with no live call. See
`reference/operations/saved-presets/SKILL.md` for what `Validate()` checks
and why it isn't auto-invoked elsewhere.

## The tuple fingerprint and the live probe

Once structurally valid, Save checks the exact current tuple fingerprint,
not a result from a different configuration. For non-OAuth providers, that
check is a **real live HTTP call**. `currentValidityKey()`
(`preset_editor.go:1341-1351`) fingerprints provider, model, the live API-key
buffer, `base_url`, `api_compat`, and the bound `codex_auth_path`. Any edit
to one of those fields invalidates the cached validity result because it
changes what a provider call or account binding would hit.

- If the fingerprint doesn't match the last-checked key, or validity is
  still unknown, Save triggers `startModelValidityCheck()` and returns —
  the user sees a pending message and must retry Save once the check lands.
- Providers currently present in `oauthProviders` (`codex` and the legacy
  `codex_oauth` alias) are **not probed** here — this process does not own
  their token file, so the async check returns `validityValid` instead of a
  bogus "no key" failure.
- **Current-source gap:** the shipped `codex-pool` provider string is not in
  `oauthProviders` (`doctor.go:836-840`). It therefore follows the ordinary
  `probeLLM` path with no API key and can hard-block Save as `probeNoKey`.
  Do not claim pool presets share the `codex` bypass until source and live
  behavior actually prove it; this manual reports the gap but does not fix it.
- Other non-OAuth providers get the real HTTP call via `probeLLM`
  (`doctor.go`).

## Pending / checking block

While a check is in flight (`validityChecking`), Save is blocked with a
pending message — there is no partial or optimistic save.

## Valid save

`probeOK` (2xx, or 404/405 which still prove connectivity+auth) → Save
proceeds.

## Deterministic invalid — hard block

`probeAuthError` (401/403), `probeNetworkError`, `probeNoKey`,
`probeEmptyResponse`, and any other non-2xx/429/503/529 status all map to
`validityInvalid`. **This is a hard block cached for the current editor and
fingerprint**: pressing Save again with the unchanged tuple does not
re-probe. After an external condition genuinely recovers, reopen the editor
(or make an intentional fingerprint-changing edit) before testing again.
Transport timeouts remain in this category — a timeout is a
`probeNetworkError`, not a retryable provider-reached result.

## Provider-reached rate limit / overload — save with warning, then reset

HTTP `429` (`probeRateLimit`) and `503`/`529` (`probeOverloaded`) mean the
provider was actually **reached** — connectivity and (usually) auth are
fine, the provider is just temporarily unable to serve the request. These
map to `validityRetryable`: Save is **allowed to proceed** with a factual
warning describing what the live probe returned (provider, model, sanitized
detail — never the credential). Immediately after accepting the save, the
in-memory validity state resets to `validityUnknown` so the **next** Save
for the same tuple re-probes live rather than permanently caching this
operational failure as either valid or invalid.

## Operations

For structural `Validate()` and the Load/Save/Delete file contract, see
`reference/operations/saved-presets/SKILL.md`. For what happens after a
successful save — activation and `/refresh` — see
`reference/operations/activation-session-refresh/SKILL.md`.
