# Confirm kernel upgrade on `lingtai-tui` launch (PR 3 of 3)

**Date:** 2026-06-23
**Status:** Design approved
**Repo:** `lingtai` (TUI, Go)
**Depends on:** PR 1 (`InspectKernel` / `RunKernelUpdate` in `internal/config/venv.go`) — see `2026-06-23-update-command-design.md`

## Context

On launch, for a returning user, `main.go` (around lines 254-267) ensures the Python kernel:

```go
if config.NeedsVenv(globalDir) {            // first install: no venv yet
    fmt.Println("Setting up Python environment...")
}
if upgraded, err := config.EnsureRuntime(globalDir); err != nil {
    ...
} else if upgraded {
    fmt.Println("Upgraded lingtai to latest version.")
}
```

`EnsureRuntime` → `ensureRuntimeWithOptions` does two things:
- If `NeedsVenv` (no venv) → `EnsureVenv` builds it (first install).
- Always then `CheckUpgrade` → `UpgradePythonRuntime(force:false)`, which **silently auto-upgrades** the kernel to the latest PyPI version when one is available.

So today, every launch silently upgrades an existing kernel. That is surprising and couples "start the TUI" with "mutate the runtime." This PR adds a confirmation for the **upgrade-an-existing-kernel** case while leaving the **first-install** case automatic.

## Goal

On `lingtai-tui` launch:

- **First install (no venv yet):** unchanged — build the runtime automatically, no prompt. (Old behavior preserved.)
- **Kernel already exists AND an upgrade is available:** prompt the user to confirm before upgrading.
  - Confirm (`y`) → apply the upgrade, print result.
  - Decline (`n`/Enter) → skip; launch with the current kernel.
- **Editable dev install, or already at latest:** no prompt, no change (matches existing skip logic in `UpgradePythonRuntime`).

### Decisions

- **Non-interactive launch (stdout is not a TTY — scripted/CI/cron):** **skip the upgrade and launch as-is.** Never block an automated launch; never upgrade without consent. (No notice line.)
- **Declining is per-launch only:** no persisted "skip this version" state. If still behind on the next launch, prompt again.

## Behavior

In the returning-user branch of `main.go`:

```
NeedsVenv? ──yes──> EnsureVenv (first install, automatic) ──> [no upgrade prompt]
   │no
   ▼
InspectKernel (read-only: installed vs latest, editable-aware)
   │
   ├─ not NeedsUpdate (latest / editable / lookup failed) ──> continue launch, no prompt
   └─ NeedsUpdate
         ├─ stdout not a TTY ──> skip, continue launch
         └─ TTY ──> print "lingtai kernel <installed> → <latest>. Upgrade now? [y/N] "
                      ├─ y ──> ApplyKernelUpgrade (RunKernelUpdate force:true), print result
                      └─ otherwise ──> skip, continue launch
```

The prompt is a plain stdin y/N read at the terminal, consistent with the existing pre-TUI stdout phase ("Setting up Python environment...", "Upgraded lingtai..."). It is NOT a Bubble Tea view — it runs before the TUI program starts.

## Architecture

### `internal/config/venv.go`

- Reuse PR 1's `InspectKernel(globalDir) KernelStatus` (read-only; `NeedsUpdate`, `Installed`, `Latest`, `Editable`).
- Add `ApplyKernelUpgrade(globalDir) DoctorReport` = `RunKernelUpdate(globalDir, force:true)` (PR 1), or expose `RunKernelUpdate` directly.
- Leave `EnsureRuntime` / `CheckUpgrade` intact for any other caller (they keep auto-upgrade semantics). The launch path stops calling the auto-upgrading `EnsureRuntime` for the existing-kernel case and instead does: ensure-venv-if-missing, then `InspectKernel` + gated `ApplyKernelUpgrade`.
  - Concretely: add `EnsureRuntimeNoUpgrade(globalDir) error` that only builds the venv when missing (no `CheckUpgrade`), so first-install still works; the upgrade decision moves to `main.go`.

### `main.go`

- Replace the returning-user `EnsureRuntime` call with:
  1. `EnsureRuntimeNoUpgrade(globalDir)` — builds venv only if missing (first-install path, automatic; preserves "Setting up Python environment..." print).
  2. If a venv already existed (not first install): `status := config.InspectKernel(globalDir)`. If `status.NeedsUpdate`:
     - If `isInteractive()` (stdout is a TTY) → print `lingtai kernel <installed> → <latest>. Upgrade now? [y/N]`, read stdin; on `y` run `config.ApplyKernelUpgrade(globalDir)` and print result.
     - Else → skip silently.
- Add a small `isInteractive()` helper (`term.IsTerminal(os.Stdout.Fd())` or equivalent already used in the codebase).
- First-install detection for "did a venv already exist": capture `NeedsVenv(globalDir)` BEFORE ensuring, so the prompt is only offered when a kernel was already present.

### i18n (en/zh/wen, flat dotted keys)

Add: `launch.kernel_upgrade_prompt` (formatted: installed → latest), `launch.kernel_upgrading`, `launch.kernel_upgraded`, `launch.kernel_upgrade_skipped`.

## Error handling

- `InspectKernel` lookup failure (offline): treat as "no upgrade available" → no prompt, launch proceeds on current kernel.
- Upgrade apply failure: print the FAIL lines; still launch with the existing kernel (do not abort the TUI over a failed optional upgrade).
- Stdin EOF / empty input at the prompt: treat as decline (default N).

## Testing

- First install (`NeedsVenv` true): venv built automatically, `InspectKernel`/prompt NOT reached.
- Existing kernel + upgrade available + TTY: prompt shown; `y` calls `ApplyKernelUpgrade`; `n`/empty does not.
- Existing kernel + upgrade available + non-TTY: no prompt, no upgrade call, launch proceeds.
- Editable/at-latest: no prompt regardless of TTY.
- Use injectable seams (the existing `RuntimeEnsureOptions`-style hooks and a stdin/TTY override) so tests don't touch the real network or terminal.

## Rollout

PR 3 lands after PR 1 (needs `InspectKernel`). Independent of PR 2. Net effect: starting the TUI no longer silently upgrades an existing kernel — the user confirms — while first installs and automated/non-TTY launches keep working without interruption.
