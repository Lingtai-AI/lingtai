# `/doctor` diagnostic-only + gated heal (PR 2 of 2)

> **Superseded — historical record (as of 2026-08-07).** The kernel-version example in this design (`0.14.1 -> 0.15.0`) is illustrative and now far behind the current kernel version (0.19.5 in `pyproject.toml`); the `/doctor` diagnostic-only behavior it specifies still matches `tui/internal/config/venv.go`.

**Date:** 2026-06-23
**Status:** Design approved
**Repo:** `lingtai` (TUI, Go)
**Depends on:** PR 1 (`/update` command + `config.RunKernelUpdate` / `InspectKernel`) — see `2026-06-23-update-command-design.md`

## Context

Interactive `/doctor` currently force-heals before diagnosing. `runDoctor` (`internal/tui/doctor.go`) calls `config.RunDoctorUpdate(ForceTUI:true, ForcePython:true)` and then `preset.Bootstrap` + `PopulateBundledLibrary` + `ExportCommandsJSON` — mutating five surfaces (TUI brew upgrade, kernel, file-search, preset bootstrap/migration, library/commands) on a single `/doctor` invocation. The preset bootstrap is what triggers the kernel-side `version:2` migration that renames saved presets out from under running agents.

PR 1 introduced an explicit `/update` (kernel-only, confirm-gated). PR 2 finishes the split: make interactive `/doctor` **diagnostic-only**, and offer healing **only when a problem is found**, gated behind a confirmation that enumerates the concrete actions.

## Goal

- Interactive `/doctor` runs **read-only diagnostics only** — no brew, no kernel reinstall, no `preset.Bootstrap`, no library/commands rewrite up front.
- After diagnostics, **if and only if a fixable problem is detected**, show a gated heal action listing the concrete mutations; on confirm, run the heal, then re-run diagnostics.
- On a healthy system, **no heal prompt** — show green, `esc` exits.
- The headless CLI `lingtai-tui doctor` (`main.go:doctorMain`) is **unchanged** — it remains the non-interactive force-heal escape hatch for scripts/CI.

Non-goals:

- The kernel-side `version:2` preset migration itself (separate repo).
- Changing `/update` (PR 1) or the CLI `doctor`.

## Behavior

`DoctorModel` gains a small state machine:

```
stateReport ──(no issues)──> idle (esc exits)
            └─(issues found)─> stateHealPrompt ──(confirm)──> stateHealing ──> stateReport (re-run)
                                                └──(cancel)──> stateReport (issues remain, esc exits)
```

- **stateReport**: run read-only diagnostics — existing checks that do NOT mutate (portal presence, kernel-health probe, LLM connectivity, MCP validation, version-skew notice) PLUS the read-only `InspectKernel`/inspect-only update classification from PR 1. Build a `healPlan`.
- **stateHealPrompt** (only if `healPlan.NeedsHeal`): render an `Apply fixes / Cancel` selector (existing index-selector idiom, `swapConfirmIdx`-style) that **enumerates the concrete actions** the heal will perform (e.g. "Update kernel 0.14.1 → 0.15.0", "Refresh bundled templates", "Re-export commands.json").
- **stateHealing** (on confirm): run the heal cascade — `RunKernelUpdate` (from PR 1) + `preset.Bootstrap` + `PopulateBundledLibrary` + `ExportCommandsJSON`, and the brew TUI upgrade only if a TUI update was among the detected issues. Stream result lines.
- After healing, return to **stateReport** and re-run diagnostics so the user sees the post-heal state.

"Fixable problem detected" = any of: kernel missing/unimportable/out-of-date (non-editable), TUI binary behind latest, portal missing, file-search native dep missing, bootstrap assets stale/absent.

## Architecture

### Dry-run classification (shared inspect/apply)

To keep diagnosis and heal from drifting, each healable surface exposes a read-only inspector returning `{needsFix bool, action string, lines []DoctorLine}` and a separate apply step. PR 1 already split the kernel surface (`InspectKernel` / `RunKernelUpdate`). PR 2 adds the same split for the remaining surfaces it classifies (TUI version, file-search, bootstrap assets), or — minimally — reuses the existing `check*` functions in a read-only "report" mode by passing `Force:false` and not executing brew/pip. The heal step is the existing mutating path.

### `internal/tui/doctor.go`

- Split `runDoctor` into `runDiagnostics(orchDir, globalDir) (lines, healPlan)` (read-only) and `runHeal(orchDir, globalDir, healPlan) lines` (mutating).
- Remove the unconditional forced-update + `preset.Bootstrap` + library/commands block from the diagnostic path; move it into `runHeal`.
- Add `DoctorModel` fields: `state`, `healPlan`, `confirmIdx`; handle the prompt/heal keys in `Update`; render the prompt and action list in `View`.

### `internal/config/venv.go`

- Reuse `InspectKernel` + `RunKernelUpdate` (PR 1).
- Optionally add `InspectUpdate(globalDir) UpdatePlan` aggregating the read-only classification of all surfaces, returning `NeedsHeal` and a human-readable `Actions []string` for the confirmation list. `RunDoctorUpdate` stays for the CLI path.

### `main.go`

- `doctorMain` (headless CLI) unchanged — keeps `RunDoctorUpdate(ForceTUI:true, ForcePython:true)` + Bootstrap. This is intentional: non-interactive callers can't answer a prompt, so the CLI stays force-heal.
- Confirm `preset.Bootstrap` / `PopulateBundledLibrary` / `ExportCommandsJSON` still run at normal startup (`main.go:213/268/272`) so fresh installs are unaffected by removing them from interactive `/doctor`.

### i18n (en/zh/wen, flat dotted keys)

Add: `doctor.heal_prompt`, `doctor.heal_actions_header`, `doctor.heal_apply`, `doctor.heal_cancel`, `doctor.healthy_ok`, `doctor.healing`, `doctor.heal_done`.

## Error handling

- Heal step failure: surfaced as FAIL lines in the post-heal report; user can re-run `/doctor` or use the CLI doctor.
- Cancel: diagnostics remain on screen with the unresolved issues; `esc` exits without mutation.
- Diagnostics themselves never mutate, so a flaky LLM/network probe cannot cause file changes.

## Testing

- `doctor_test.go`:
  - Healthy system → model reaches `stateReport` with `healPlan.NeedsHeal == false`; no prompt; no install/bootstrap commands issued (assert via test `CommandRunner`).
  - Out-of-date kernel → `stateHealPrompt` appears and lists the kernel action; "Cancel" issues no commands; "Apply" transitions to `stateHealing` and invokes `RunKernelUpdate`.
- `venv_test.go`: `InspectUpdate` issues no mutating commands and `NeedsHeal` reflects the injected state.
- Regression: a test asserting interactive `runDiagnostics` performs zero brew/pip/Bootstrap calls.

## Rollout

PR 2 lands after PR 1. Net effect: `/update` = explicit kernel update; `/doctor` = diagnose, then heal only with consent; `lingtai-tui doctor` (CLI) = unchanged force-heal for automation. Saved presets are no longer mutated as a side effect of running interactive `/doctor`.
