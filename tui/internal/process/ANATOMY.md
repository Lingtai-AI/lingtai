---
related_files:
  - tui/ANATOMY.md
  - tui/internal/processscan/ANATOMY.md
  - tui/internal/inventory/ANATOMY.md
  - tui/internal/config/ANATOMY.md
  - tui/internal/fs/ANATOMY.md
  - tui/internal/process/check.go
  - tui/internal/process/check_test.go
  - tui/internal/process/kill_unix.go
  - tui/internal/process/kill_windows.go
  - tui/internal/process/launcher.go
  - tui/internal/process/launcher_test.go
maintenance: |
  Keep related_files as repo-relative paths to real files. Include neighboring
  ANATOMY.md files so the anatomy graph stays connected rather than isolated;
  anatomy links must be bidirectional. If you create a new ANATOMY.md, copy this
  maintenance field. If you notice drift between this anatomy and the code,
  report it. See lingtai-dev-guide for details.
---

# process

> **Maintenance:** see the `lingtai-tui-anatomy` skill at `tui/internal/preset/skills/lingtai-tui-anatomy/SKILL.md`. Coding agents update this file in the same commit as code changes.

`process` is the TUI's subprocess boundary with the Python kernel: the one place that turns "start this agent" into a real `python -m lingtai run <dir>` child under the managed runtime venv, and the one place that terminates such a child. It is the *only* direct process-level coupling between the two languages — after launch, the TUI observes the agent purely through its working directory (`tui/internal/fs/`). Detection logic itself lives one layer down in `tui/internal/processscan/`, which this package re-exports so low-level callers that cannot import `process` still share one tested implementation.

## Components

| Symbol | Citation | Purpose |
|---|---|---|
| `AgentProcess` | `tui/internal/process/check.go:9` | type alias re-exporting `processscan.AgentProcess` so callers need only one import |
| `FindAgentProcesses` | `tui/internal/process/check.go:18` | delegates to `processscan` for the running processes bound to one agent dir |
| `IsAgentRunning` | `tui/internal/process/check.go:30` | the boolean launch/refresh gate built on that scan |
| `ErrAgentAlreadyRunning` | `tui/internal/process/launcher.go:18` | the sentinel `LaunchAgent` returns instead of starting a second kernel in one workdir |
| `InitProject` | `tui/internal/process/launcher.go:20` | creates/stamps a project's `.lingtai/` skeleton before the first launch |
| `LaunchAgent` | `tui/internal/process/launcher.go:96` | the guarded launch: refuses when an agent is already running in the workdir, then spawns the kernel with the resolved venv interpreter, log redirection, and PID tracking |
| `ForceLaunchAgent` | `tui/internal/process/launcher.go:109` | the same spawn with the already-running guard deliberately skipped, for explicit operator override |
| `TerminateAgentProcesses` | `tui/internal/process/kill_unix.go:18`, `tui/internal/process/kill_windows.go:18` | per-OS termination of every process matched for an agent dir; `terminateError` (`kill_unix.go:54`, `kill_windows.go:43`) names the PIDs that survived |

## Connections

- **Calls `tui/internal/processscan/`** for all process-table matching — this package adds launch/terminate policy, not parsing.
- **Calls `tui/internal/config/`** to resolve the runtime venv interpreter (`~/.lingtai-tui/runtime/venv`) the kernel is launched from.
- **Called by `tui/main.go`** (interactive start, `purge`, `suspend`), by `tui/internal/tui/` (`launcher.go`, the network home's start/stop actions), and by `tui/internal/headless/spawn.go` for the non-interactive path.
- **Launches the kernel only.** It never speaks to a running agent afterward; all further communication is filesystem-only through `tui/internal/fs/`.

## Composition

- **Parent:** `tui/` (`tui/ANATOMY.md`)
- **Subfolders:** none — flat package with per-OS `kill_{unix,windows}.go` build-tagged files
- **Siblings:** `tui/internal/processscan/` (detection primitives), `tui/internal/inventory/` (typed running-agent inventory built on the same scans)

## Notes

- **One agent per workdir.** `LaunchAgent` fails with `ErrAgentAlreadyRunning` rather than starting a second kernel against the same `.lingtai/<agent>/`. `ForceLaunchAgent` is the explicit, operator-visible override — do not make it the default path.
- **Process-table detection is advisory.** Same caveat as `processscan`: matching is best-effort and racy. The kernel's own workdir lock is the authoritative gate; never rely on `IsAgentRunning` alone for correctness under concurrency.
- **Keep the import direction clean.** `processscan` exists precisely because `process` imports `migrate`; packages below that line must import `processscan`, never `process`.
- **Termination reports survivors.** `TerminateAgentProcesses` returns a `terminateError` naming the PIDs it could not stop instead of silently reporting success — callers surface that to the user.
