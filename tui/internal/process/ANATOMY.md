---
related_files:
  - tui/ANATOMY.md
  - tui/internal/process/CONTRACT.md
  - tui/internal/process/launcher.go
  - tui/internal/process/launcher_test.go
  - tui/internal/process/check.go
  - tui/internal/process/check_test.go
  - tui/internal/process/kill_unix.go
  - tui/internal/process/kill_windows.go
  - tui/internal/processscan/ANATOMY.md
  - tui/internal/processscan/CONTRACT.md
  - tui/internal/config/venv.go
  - tui/internal/fs/signal.go
  - tui/internal/headless/spawn.go
maintenance: |
  Keep related_files as repo-relative paths to real files. Include neighboring
  ANATOMY.md files so the anatomy graph stays connected rather than isolated;
  anatomy links must be bidirectional. If you create a new ANATOMY.md, copy this
  maintenance field. If you notice drift between this anatomy and the code,
  report it. See lingtai-dev-guide for details.
---

# process

`process` is the TUI's concrete agent-process lifecycle boundary. It initializes the minimal project filesystem, starts one Python kernel process for an agent directory, re-exports advisory process observation, and performs explicit platform-specific termination. Its behavioral promise is defined by the nearby `CONTRACT.md`.

## Components

| Component | File | Purpose |
|---|---|---|
| `InitProject` | `tui/internal/process/launcher.go:20` | initialize the human mailbox and migration-owned project state before the TUI starts |
| `resolvePython` | `tui/internal/process/launcher.go:70` | prefer an existing agent-specific venv Python and otherwise use the caller-supplied managed-runtime command |
| `LaunchAgent` | `tui/internal/process/launcher.go:96` | best-effort duplicate guard followed by one `python -m lingtai run <agentDir>` start |
| `ForceLaunchAgent` | `tui/internal/process/launcher.go:109` | bypass duplicate observation only for a caller-controlled refresh/restart boundary |
| `AgentProcess` / `FindAgentProcesses` / `IsAgentRunning` | `tui/internal/process/check.go:9-31` | compatibility exports over the lower-level processscan boundary |
| `TerminateAgentProcesses` (Unix) | `tui/internal/process/kill_unix.go:18` | SIGTERM matching processes, then SIGKILL survivors after a bounded wait |
| `TerminateAgentProcesses` (Windows) | `tui/internal/process/kill_windows.go:18` | terminate matching Windows processes and verify that they disappear within a bounded wait |

## Connections

- **Upstream callers:** interactive startup, first-run creation, refresh/restart, and `tui/internal/headless/spawn.go` use this package to start agents; explicit refresh cleanup uses termination.
- **Observation dependency:** `tui/internal/processscan` owns command-line parsing and host process-table adapters. This package re-exports its one-agent view instead of duplicating platform parsers.
- **Filesystem/runtime dependencies:** launch cleans signal files through `tui/internal/fs`, resolves the Python executable through `tui/internal/config`, verifies configured addons, and writes best-effort output to `logs/agent.log`.

## Composition

- **Parent:** `tui/` (`tui/ANATOMY.md`).
- **Neighbor boundary:** `tui/internal/processscan/` (`tui/internal/processscan/ANATOMY.md`) is the dependency-light process observation component shared with inventory and migrations.
- **Platform adapters:** `kill_unix.go` and `kill_windows.go` expose the same termination signature behind build tags.

## State

- **Reads:** the agent's resolved init manifest and optional agent-specific venv path; the host process table through processscan.
- **Writes:** initial human mailbox files, signal cleanup, and best-effort `logs/agent.log` redirection.
- **External side effects:** starts or terminates operating-system processes. A successful start means only that the child process was created; readiness remains a caller-owned heartbeat/process confirmation step.

## Notes

- The agent directory is passed as one argument, never interpolated through a shell, so Windows paths and paths containing spaces retain their boundary.
- Process-table observation is advisory. Absence of a match is not authoritative proof that an agent is stopped.
- `InitProject` remains co-located legacy bootstrap code; it is mapped here but is not part of the process-lifecycle Port governed by the paired Contract.
