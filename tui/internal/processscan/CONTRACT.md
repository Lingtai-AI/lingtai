---
name: agent-process-observation
contract_version: 1
root_contract: CONTRACT.md
related_files:
  - tui/internal/processscan/ANATOMY.md
  - tui/internal/processscan/check.go
  - tui/internal/processscan/check_test.go
  - tui/internal/process/CONTRACT.md
  - tui/internal/process/check.go
  - tui/internal/inventory/inventory.go
  - tui/internal/migrate/m036_sqlite_log_backfill.go
  - tui/list_common.go
  - tui/list_unix.go
  - tui/list_windows.go
  - tui/purge_common.go
  - tui/purge_unix.go
  - tui/purge_windows.go
maintenance: |
  This component contract is governed by the root CONTRACT.md. Keep
  related_files complete and repo-relative: the paired ANATOMY.md, Port, every
  production Adapter, contract tests, and directly relevant component contracts
  belong here. Re-read this contract whenever a linked boundary changes. Update
  the Port, affected Adapters, contract tests, and this contract in the same
  change; update the paired Anatomy when structure or composition also changes;
  bump contract_version for a breaking Port-contract change. If code and contract
  disagree, treat the disagreement as a defect—do not silently rewrite the
  normative contract to match the implementation.
---
# Agent Process Observation

## Purpose

This contract governs advisory observation of LingTai agent processes in the host process table. It converts supported command lines into typed process records for launch duplicate checks, inventory, purge, and offline-migration safety. It does not own process launch, termination, heartbeat truth, kernel workdir locking, or a user-facing lifecycle state machine.

The package is deliberately dependency-light concrete code. Its exported functions are the current behavioral Port; this contract does not claim an injected operating-system adapter migration is complete.

## Behavior

LingTai and coding agents MUST preserve full agent-directory argument identity across Unix and Windows command forms. They MUST keep one-agent observation distinct from authoritative state: an empty result can mean no visible match or a scan failure. All-process callers MUST surface returned scan errors rather than render an empty machine as a stopped/healthy conclusion.

Agents MUST NOT use process-table observation alone for race-sensitive mutation. They must combine it with the hard gate owned by the relevant use case, such as the kernel workdir lock for offline SQLite rebuild.

## Port

The current observation Port is the exported API in `tui/internal/processscan`:

- `AgentProcess` contains `PID`, optional `Uptime`, the extracted full `AgentDir`, and original `Command` text.
- `ExtractAgentDir(command string) (string, bool)` recognizes supported LingTai launch markers and extracts one final agent-directory argument.
- `ParsePSOutput(out, abs string) []AgentProcess`, `ParsePSListOutput(out string) []AgentProcess`, and `ParseWMICOutput(out, abs string) []AgentProcess` expose deterministic parser seams.
- `FindAgentProcesses(agentDir string) []AgentProcess` normalizes one requested directory and returns visible exact matches. Its legacy signature intentionally cannot distinguish scan failure from no match.
- `FindAllAgentProcesses() ([]AgentProcess, error)` lists every visible recognized process and returns a host-scan error explicitly.
- `IsAgentRunning(agentDir string) bool` is a boolean convenience over `FindAgentProcesses`; false is advisory only.

## Adapters

- On Unix-like systems, the one-agent adapter invokes `ps -eo pid=,command=` and the all-agent adapter invokes `ps -eo pid=,etime=,command=`.
- On Windows, the adapter first invokes WMIC process listing and falls back to Windows PowerShell `Get-CimInstance Win32_Process`; both feed the same `CommandLine`/`ProcessId` parser.
- `tui/internal/process/check.go` is the lifecycle package's compatibility adapter over this Port.
- `list` and `purge` are fail-loud consumers of `FindAllAgentProcesses`; inventory enriches returned rows with filesystem state.

## Contract rules

1. Recognized command forms are `python -m lingtai run <agentDir>`, `lingtai run <agentDir>`, and `lingtai-agent run <agentDir>`, including `.exe` command names on Windows.
2. Parsers MUST preserve the full final agent-directory argument, including spaces and Windows path separators, and MUST reject unrelated module/command markers and malformed PIDs.
3. One-agent matching compares the exact cleaned absolute requested directory. Raw prefix siblings such as `<agentDir>-sibling` or ambiguous unquoted spaced suffixes MUST NOT match.
4. An unspaced directory may be followed by an unambiguous extra argument. A quoted spaced directory may also be followed by an extra argument. An unquoted spaced directory matches only as the exact command suffix; trailing text is ambiguous and MUST be rejected.
5. Parser output preserves process-table order. No parser or scanner may mutate agent or project filesystem state.
6. `FindAgentProcesses` and `IsAgentRunning` retain their legacy collapsed-error shape. Consumers MUST interpret empty/false as "no visible match" rather than a guaranteed stopped state.
7. `FindAllAgentProcesses` MUST return host command failure. List, purge, or inventory callers MUST fail loud or report unknown; they MUST NOT convert that error into an empty process table.
8. Observation is advisory and race-prone. It is insufficient as the sole authorization for destructive, offline, or migration work.
9. This package MUST remain free of imports back into higher-level `process`, `migrate`, inventory, presentation, or TUI packages.

## Contract tests

`tui/internal/processscan/check_test.go` covers supported and unrelated command markers, exact and prefix-sibling path matching, quoted/unquoted paths with spaces, trailing-argument ambiguity, malformed rows, Windows `.exe` forms, WMIC records, list-all behavior, uptime preservation, and fail-loud all-process scan errors.

`tui/internal/process/check_test.go` covers the compatibility parser path used by lifecycle callers. `tui/architecture_documents_test.go` validates this Contract's root registration, paired Anatomy link, required related files, maintenance marker, and body headings.

## Maintenance

Keep parsers, Unix/Windows host commands, caller-visible error meaning, conformance cases, the lifecycle contract link, and the paired Anatomy synchronized. A future richer `running / stopped / unknown` Port is a breaking successor, not permission to reinterpret the current boolean API silently.
