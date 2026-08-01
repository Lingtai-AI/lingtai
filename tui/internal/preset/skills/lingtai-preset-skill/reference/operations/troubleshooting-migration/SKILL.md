---
name: preset-skill-op-troubleshooting-migration
description: Bounded first-pass triage for preset problems; routes deeper runtime/update/migration questions elsewhere instead of guessing.
version: 1.0.0
last_changed_at: "2026-07-19T00:00:00Z"
related_files:
  - tui/internal/preset/preset.go
  - tui/internal/tui/preset_editor.go
  - tui/internal/tui/app.go
  - tui/internal/tui/codex_pool_store.go
  - tui/internal/preset/preset_skill_router_test.go
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# Troubleshooting / migration triage

This child is intentionally **bounded**: it covers the handful of
preset-specific symptoms this skill tree can explain from its own evidence,
and routes everything else rather than guessing at runtime, update, or
migration internals it doesn't own.

## In scope here

- **"My saved preset didn't update after a TUI upgrade."** Expected —
  `RefreshTemplates` only rewrites `templates/`, never `saved/`. See
  `reference/operations/saved-presets/SKILL.md`. If the user wants the new
  template's changes, they re-create the saved preset from the refreshed
  template.
- **"A template I used before is gone."** Check whether it was pruned by
  `RefreshTemplates` because it's no longer in `BuiltinPresets()` (a
  retired template) — see `reference/operations/saved-presets/SKILL.md`,
  Bootstrap section. A saved clone of a retired template is untouched and
  still loads.
- **"Save is stuck / won't go through."** Save no longer runs a live
  provider check, so there is no pending/blocked save state to diagnose
  here — a save failure now means the structural `Validate()` rejected
  the preset (see `reference/operations/saved-presets/SKILL.md`) or a
  file-write error. If the user is instead asking whether the *provider*
  actually works, that is a `/doctor` question, not a save-time one — see
  `reference/operations/availability-save-gate/SKILL.md`.
- **"I edited the preset/pool file but the running agent didn't change."**
  Expected — see `reference/operations/activation-session-refresh/SKILL.md`:
  saving alone never switches a running session; an explicit refresh,
  relaunch, or new service construction is required.
- **"codex-pool file looks wrong / refuses my edit."** See
  `reference/codex-pool/SKILL.md` — check whether the pool is
  model-classified (a `models` key, even `{}`) before assuming a flat edit
  should have worked.

## Out of scope — route, don't guess

- Deeper **runtime** questions (kernel adapter registration, request-scoped
  failover behavior beyond what's cited in `reference/codex-pool/SKILL.md`,
  live provider request/response shapes) belong to kernel-side skills or
  direct kernel source reading — do not extrapolate kernel internals from
  TUI-side evidence.
- **Update/upgrade mechanics** (how a TUI version bump itself is delivered,
  migration numbering, `tui/internal/migrate/`) belong to the migration
  package's own ANATOMY.md and ownership, not this preset skill tree.
- **A preset that looks structurally fine but the provider still rejects
  every request** is a live operational issue, not a documentation gap —
  point at `/doctor` (real availability diagnosis; see
  `reference/operations/availability-save-gate/SKILL.md` for why Save
  itself no longer probes), but do not invent a root cause this skill's
  evidence doesn't support.

When a question falls outside the "in scope" list above, say so explicitly
and point at the narrower, correctly-owned skill or source location instead
of speculating from preset-package evidence alone.
