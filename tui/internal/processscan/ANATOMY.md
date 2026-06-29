---
related_files:
  - tui/ANATOMY.md
  - tui/internal/migrate/ANATOMY.md
  - tui/internal/processscan/check.go
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

`processscan` is the small, dependency-light subprocess detector for running LingTai agents. It exists outside `tui/internal/process` so packages that cannot import `process` (notably `migrate`, because `process` imports `migrate` during fresh-project stamping) can still reuse one tested `ps` matching implementation instead of copying it.

## Components

| Component | File | Purpose |
|---|---|---|
| `AgentProcess` | `tui/internal/processscan/check.go:10` | parsed `ps` record for a matching `lingtai run <agentDir>` interpreter |
| `ParsePSOutput` | `tui/internal/processscan/check.go:20` | unit-testable parser for `ps -eo pid=,command=` output; guards against prefix-sibling false positives |
| `FindAgentProcesses` | `tui/internal/processscan/check.go:60` | shells out to `ps`, normalizes the requested agent dir, and parses matches |
| `IsAgentRunning` | `tui/internal/processscan/check.go:75` | boolean convenience wrapper used by launch/migration boundaries |

## Composition

- **Upstream callers:** `tui/internal/process/check.go` re-exports this package for existing launch/refresh callers; `tui/internal/migrate/m036_sqlite_log_backfill.go` calls it directly to skip running agents before attempting offline SQLite rebuilds.
- **External dependency:** host `ps -eo pid=,command=`. Errors fail closed to “no match”; the kernel workdir lock remains the authoritative safety gate for SQLite rebuilds.

## Invariants

1. Match only exact `lingtai run <absAgentDir>` command arguments, allowing extra trailing args but rejecting prefix siblings such as `<absAgentDir>-old`.
2. Keep this package free of imports back into TUI logic packages (`migrate`, `process`, `tui`) so it remains safe for low-level reuse.
3. Treat `ps` detection as advisory. Callers that need correctness under races must also rely on their own hard gate (for example, the kernel offline workdir lock in SQLite rebuilds).
