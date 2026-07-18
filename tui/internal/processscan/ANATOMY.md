---
related_files:
  - tui/ANATOMY.md
  - tui/internal/process/ANATOMY.md
  - tui/internal/processscan/CONTRACT.md
  - tui/internal/inventory/ANATOMY.md
  - tui/internal/migrate/ANATOMY.md
  - tui/internal/processscan/check.go
  - tui/internal/processscan/check_test.go
  - tui/internal/processscan/scan_unix.go
  - tui/internal/processscan/scan_windows.go
  - tui/internal/processscan/scan_windows_test.go
  - tui/go.mod
  - tui/go.sum
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

`processscan` is the small subprocess detector for running LingTai agents. It stays outside `tui/internal/process` so lifecycle, inventory, list, purge, and retained historical migration code can reuse one tested command-matching core with narrow Unix and Windows process-table adapters, without importing the full launch/termination boundary. Its observable behavior is governed by the nearby `CONTRACT.md`.

## Components

| Component | File | Purpose |
|---|---|---|
| `AgentProcess` | `tui/internal/processscan/check.go:13` | parsed process-table record with PID, optional uptime, full agent dir, and command text |
| `processRecord` / `agentProcessesFromRecords` | `tui/internal/processscan/check.go:24`, `tui/internal/processscan/check.go:118` | technology-neutral PID/nullable-command rows converted through the shared launch-marker and exact-path matcher |
| `ParsePSOutput` | `tui/internal/processscan/check.go:39` | deterministic parser for Unix `ps -eo pid=,command=` output scoped to one agent dir |
| `ParsePSListOutput` | `tui/internal/processscan/check.go:66` | deterministic parser for Unix `ps -eo pid=,etime=,command=` output listing every agent process |
| `ParseWMICOutput` | `tui/internal/processscan/check.go:91` | retained legacy deterministic parser; Windows production observation no longer consumes command output |
| `ExtractAgentDir` | `tui/internal/processscan/check.go:155` | launch-marker parser that takes the final run argument intact so spaces survive |
| `FindAgentProcesses` | `tui/internal/processscan/check.go:295` | normalizes one requested agent dir, delegates to the platform adapter, and preserves collapsed query errors |
| `FindAllAgentProcesses` | `tui/internal/processscan/check.go:315` | delegates to the platform adapter and returns all visible processes or the host-query error |
| Unix adapter | `tui/internal/processscan/scan_unix.go:10` | invokes the two existing `ps` shapes and feeds their deterministic parsers |
| Windows adapter | `tui/internal/processscan/scan_windows.go:24` | creates a per-call WMI client, queries PID plus nullable command line in-process, and feeds shared matching |
| Windows query seam | `tui/internal/processscan/scan_windows.go:29` | accepts a query function parameter for provider-error and nullable-command tests without global mutable state |
| `IsAgentRunning` | `tui/internal/processscan/check.go:335` | boolean convenience wrapper used by launch/migration boundaries |

## Connections

- **Upstream callers:** `tui/internal/process/check.go` re-exports this package for launch/refresh callers; `tui/internal/migrate/m036_sqlite_log_backfill.go` calls it directly to skip running agents before attempting offline SQLite rebuilds; `internal/inventory` consumes all-process scans for `lingtai-tui list` and `/projects` (`tui/internal/inventory/inventory.go:105-167`); `purge` uses the same agent-dir filtering boundary through inventory helpers.
- **Platform dependencies:** Unix uses host `ps -eo pid=,command=` for one-dir checks and `ps -eo pid=,etime=,command=` for all-process list/purge. Windows uses `github.com/yusufpapurcu/wmi` with a local `Client{PtrNil:true}` to query `Win32_Process` in-process; it spawns no WMIC, PowerShell, shell, or process-table helper. One-dir query errors collapse to “no visible match” (advisory boundary); all-process query errors are returned from `FindAllAgentProcesses` so `list`/`purge` fail loud instead of reporting an empty process table. The kernel workdir lock remains the authoritative safety gate for SQLite rebuilds.

## Composition

- **Parent:** `tui/` (`tui/ANATOMY.md`).
- **Neighbor:** `tui/internal/process/` (`tui/internal/process/ANATOMY.md`) owns launch and termination while consuming this observation boundary.
- **Consumers:** inventory, list, purge, and retained offline migration checks depend on this package without importing higher-level TUI process policy.

## State

- **Reads:** the current user's host process table through Unix `ps` or an in-process Windows WMI query.
- **Writes:** none. Parsing and scanning do not mutate project, agent, or global TUI state.
- **Derived values:** PID, optional uptime, original command, and extracted full agent directory.

## Notes

1. Match supported launch forms (`python -m lingtai run`, `lingtai run`, `lingtai-agent run`) and preserve the full final agent-dir argument, including spaces.
2. For one-dir checks, match only exact `<absAgentDir>` command arguments. Extra trailing args are accepted only when the boundary is unambiguous (quoted dir or no-space dir); unquoted dirs with spaces must be exact so prefix siblings such as `<absAgentDir> beta` do not match.
3. Keep this package free of imports back into TUI logic packages (`migrate`, `process`, `tui`) so it remains safe for low-level reuse.
4. Treat process-table detection as advisory. Callers that need correctness under races must also rely on their own hard gate (for example, the kernel offline workdir lock in SQLite rebuilds).
5. Keep `ParseWMICOutput` only as a legacy exported deterministic parser until a separately authorized breaking Port removes it; production Windows observation must remain in-process and must not add an external-command fallback.
