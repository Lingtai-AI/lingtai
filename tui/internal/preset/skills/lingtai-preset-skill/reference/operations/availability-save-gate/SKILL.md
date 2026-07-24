---
name: preset-skill-op-availability-save-gate
description: Save on the preset editor performs local structural validation only — it never makes a live provider/model network call. Real availability diagnosis is owned by /doctor and actual runtime execution.
version: 2.0.0
last_changed_at: "2026-07-24T00:00:00Z"
related_files:
  - tui/internal/tui/preset_editor.go
  - tui/internal/tui/model_validity.go
  - tui/internal/tui/doctor.go
  - tui/internal/preset/preset_skill_router_test.go
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# Availability / save gate

Evidence: `tui/internal/tui/preset_editor.go`'s `commit()`.

## Save is structural-only

`commit()` runs the preset's structural `Validate()` and, if it passes,
saves immediately. There is no live HTTP call, no pending/checking state,
and no per-tuple credential fingerprinting gate on Save — for any
provider, including Codex, Codex-pool, and API-key providers like
DeepSeek. See `reference/operations/saved-presets/SKILL.md` for what
`Validate()` checks.

## History

An earlier revision of this editor ran a real live provider/model call
on every Save and hard-blocked the save on a non-2xx response (a
"real-availability gate"). That gate false-positived broadly — every
provider, including working Codex and API-key configurations, could be
rejected by transient network/provider conditions unrelated to whether
the configuration was actually usable — and was removed (2026-07-23,
reported by Jason). It has not been replaced by another probe, whitelist, or
retry loop.

## Where live availability diagnosis actually lives

`/doctor` (`tui/internal/tui/doctor.go`'s `probeLLM`) and real runtime
execution remain the places that diagnose whether a provider/model is
actually reachable. Save does not duplicate that check.

## Operations

For structural `Validate()` and the Load/Save/Delete file contract, see
`reference/operations/saved-presets/SKILL.md`. For what happens after a
successful save — activation and `/refresh` — see
`reference/operations/activation-session-refresh/SKILL.md`.
