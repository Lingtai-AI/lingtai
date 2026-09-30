---
name: preset-skill-op-activation-session-refresh
description: How a saved preset becomes the running default, first-run/setup choice semantics, propagation, and what /refresh actually switches.
version: 1.0.3
last_changed_at: "2026-09-29T00:00:00Z"
related_files:
  - tui/internal/tui/firstrun.go
  - tui/internal/tui/app.go
  - tui/internal/preset/preset_skill_router_test.go
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# Activation / session refresh

Evidence: `tui/internal/tui/firstrun.go:2889-3073`,
`tui/internal/tui/firstrun.go:3615`,
`tui/internal/tui/firstrun.go:3697`, `tui/internal/tui/app.go:971-1020`,
`tui/internal/tui/app.go:1240-1342`, `tui/internal/tui/app.go:1407-1491`, and
`tui/internal/tui/app.go:1702-1728`.

## The agent-preset page is saved-only

`enterAgentPresets()` (`firstrun.go:2899-3000`) lists only **saved**
presets (`Source == SourceSaved` via `preset.IsTemplate`), never raw
templates. A built-in template isn't "endorsed" onto this surface until
the user has edited and saved it — which materializes a saved preset. See
`reference/operations/saved-presets/SKILL.md` for the save mechanics that
put a preset here in the first place.

## Selected becomes default; default is always allowed and first

The wizard defaults to "nothing allowed except the one the user's cursor
was on when they entered this step" — the schema invariant is that
`default` must be a member of `allowed`, and the default row is always
forced into `presetAllowed`. `allowedPresetRefs()`
(`firstrun.go:3042-3057`) writes the default preset first, then the rest
of the user-checked rows in row order, into
`manifest.preset.{default,allowed}`.

## Setup re-edit preserves existing configuration

Re-running `/setup` on an agent that already has an `init.json` hydrates
`presetAllowed`/`presetDefaultIdx` from the **existing**
`manifest.preset.{default,allowed}` (`firstrun.go:2945-2997`) rather than
resetting to "nothing allowed" — so re-running setup only changes what the
user explicitly changes. Path comparison is normalized (`~/...` vs
absolute) via `presetRefMatches` (`firstrun.go:3014-3035`) so the same file
referenced two different ways still matches. A preset the wizard's cursor
now points at, but which isn't in the existing allowed list (e.g. just
created in the editor), is auto-checked so it doesn't silently stay
unauthorized — the user can still uncheck it.

## Propagation is best-effort, network-wide

`propagatePresetPolicyToNetwork()` (`firstrun.go:3068-3073`; call sites at
`firstrun.go:3615` and `firstrun.go:3697`) treats `/setup`
as a network-wide preset-policy reset: the wizard's chosen
`{default, allowed}` surface is pushed to every other agent in the
project, not just the one being edited. This is **best-effort** — failures
are not surfaced as a user-visible error because the primary agent's save
already succeeded.

## `/refresh` — three forms, one thing they don't do

`app.go:971-1020` handles three `/refresh` invocations:

- **`/refresh all`** — for every non-human agent, attempts a best-effort
  reset of `manifest.preset.active` to that agent's configured
  `manifest.preset.default`, then hard-refreshes it. A reset failure is silent,
  so the prior active preset may remain. Returned relaunch/heartbeat failures
  are collected per agent; the operation does not stop on the first one.
- **`/refresh <preset>`** — resolves `<preset>` against the target agent's
  `manifest.preset.allowed` list first (`resolvePresetInAllowed`); an
  unresolvable name surfaces a clear status-bar error and does nothing
  destructive. It then attempts to write that active preset before the hard
  refresh. Current source continues when the write fails, so a successful
  relaunch alone does not prove the requested switch happened.
- **bare `/refresh`** — attempts the same best-effort reset to the configured
  default before hard-refreshing the target. It can switch an agent that was
  running a non-default preset, but a reset failure leaves the prior active
  value in place while relaunch continues.

**Saving a preset alone does not switch a running session.** A saved or
even newly-activated (`default`) preset only takes effect on the *next*
launch/refresh of a given agent — an explicit `/refresh [preset]`,
relaunch, or new service construction is required to pick it up.

## Operations

For why Save no longer requires a live-probe pass before a preset becomes
eligible here (structural validation only), see
`reference/operations/availability-save-gate/SKILL.md`. For the
Load/Save/Delete contract, see
`reference/operations/saved-presets/SKILL.md`.
