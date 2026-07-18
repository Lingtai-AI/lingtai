---
related_files:
  - tui/ANATOMY.md
  - tui/internal/process/ANATOMY.md
  - tui/internal/processscan/CONTRACT.md
  - tui/internal/inventory/ANATOMY.md
  - tui/internal/migrate/ANATOMY.md
  - tui/internal/processscan/check.go
  - tui/list_common.go
  - tui/list_unix.go
  - tui/list_windows.go
  - tui/purge_common.go
  - tui/purge_unix.go
  - tui/purge_windows.go
  - tui/internal/process/check.go
  - tui/internal/process/check_test.go
  - tui/internal/migrate/m036_sqlite_log_backfill.go
maintenance: |
  Keep related_files as repo-relative paths to real files. Include neighboring
  ANATOMY.md files so the anatomy graph stays connected rather than isolated;
  anatomy links must be bidirectional. If you create a new ANATOMY.md, copy this
  maintenance field. If you notice drift between this anatomy and the code,
  report it. See lingtai-dev-guide for details.
---

# processscan

> **Maintenance:** see `lingtai-tui-anatomy` (at `~/.lingtai-tui/utilities/lingtai-tui-anatomy/SKILL.md`). Update this file in the same commit as code changes.

`processscan` is the small, dependency-light subprocess detector for running LingTai agents. It exists outside `tui/internal/process` so packages that cannot import `process` (notably `migrate`, because `process` imports `migrate` during fresh-project stamping) can still reuse one tested `ps` matching implementation instead of copying it. Its observable behavior is governed by the nearby `CONTRACT.md`.

## Components

| Component | File | Purpose |
|---|---|---|
| `AgentProcess` | `tui/internal/processscan/check.go:15` | parsed process-table record with PID, optional uptime, full agent dir, and command text |
| `ParsePSOutput` | `tui/internal/processscan/check.go:29` | unit-testable parser for `ps -eo pid=,command=` output scoped to one agent dir |
| `ParsePSListOutput` | `tui/internal/processscan/check.go:56` | unit-testable parser for `ps -eo pid=,etime=,command=` output listing every agent process |
| `ParseWMICOutput` | `tui/internal/processscan/check.go:81` | Windows WMIC/PowerShell list parser with the same agent-dir extraction rules |
| `ExtractAgentDir` | `tui/internal/processscan/check.go:122` | launch-marker parser that takes the final run argument intact so spaces survive |
| `FindAgentProcesses` | `tui/internal/processscan/check.go:262` | shells out to process listing, normalizes one requested agent dir, and parses matches |
| `FindAllAgentProcesses` | `tui/internal/processscan/check.go:281` | shells out to process listing and returns all visible agent processes for list/purge, or the scan-command error |
| `IsAgentRunning` | `tui/internal/processscan/check.go:333` | boolean convenience wrapper used by launch/migration boundaries |

## Connections

- **Upstream callers:** `tui/internal/process/check.go` re-exports this package for launch/refresh callers; `tui/internal/migrate/m036_sqlite_log_backfill.go` calls it directly to skip running agents before attempting offline SQLite rebuilds; `internal/inventory` consumes all-process scans for `lingtai-tui list` and `/projects` (`tui/internal/inventory/inventory.go:105-167`); `purge` uses the same agent-dir filtering boundary through inventory helpers.
- **External dependency:** host `ps -eo pid=,command=` for one-dir checks, `ps -eo pid=,etime=,command=` for all-process list/purge, and WMIC/PowerShell on Windows. One-dir check errors collapse to “no visible match” (advisory boundary); all-process scan errors are returned from `FindAllAgentProcesses` so `list`/`purge` fail loud instead of reporting an empty process table. The kernel workdir lock remains the authoritative safety gate for SQLite rebuilds.

## Composition

- **Parent:** `tui/` (`tui/ANATOMY.md`).
- **Neighbor:** `tui/internal/process/` (`tui/internal/process/ANATOMY.md`) owns launch and termination while consuming this observation boundary.
- **Consumers:** inventory, list, purge, and retained offline migration checks depend on this package without importing higher-level TUI process policy.

## State

- **Reads:** the current user's host process table through platform commands.
- **Writes:** none. Parsing and scanning do not mutate project, agent, or global TUI state.
- **Derived values:** PID, optional uptime, original command, and extracted full agent directory.

## Notes

1. Match supported launch forms (`python -m lingtai run`, `lingtai run`, `lingtai-agent run`) and preserve the full final agent-dir argument, including spaces.
2. For one-dir checks, match only exact `<absAgentDir>` command arguments. Extra trailing args are accepted only when the boundary is unambiguous (quoted dir or no-space dir); unquoted dirs with spaces must be exact so prefix siblings such as `<absAgentDir> beta` do not match.
3. Keep this package free of imports back into TUI logic packages (`migrate`, `process`, `tui`) so it remains safe for low-level reuse.
4. Treat process-table detection as advisory. Callers that need correctness under races must also rely on their own hard gate (for example, the kernel offline workdir lock in SQLite rebuilds).
