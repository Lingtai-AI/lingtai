---
name: agent-process-lifecycle
contract_version: 1
root_contract: CONTRACT.md
related_files:
  - tui/internal/process/ANATOMY.md
  - tui/internal/process/launcher.go
  - tui/internal/process/launcher_test.go
  - tui/internal/process/check.go
  - tui/internal/process/check_test.go
  - tui/internal/process/kill_unix.go
  - tui/internal/process/kill_windows.go
  - tui/internal/processscan/CONTRACT.md
  - tui/internal/config/venv.go
  - tui/internal/fs/signal.go
  - tui/internal/headless/spawn.go
  - tui/internal/headless/spawn_test.go
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
# Agent Process Lifecycle

## Purpose

This contract governs the TUI's outbound lifecycle boundary for one LingTai agent process: observe a possible duplicate, start the Python CLI for one exact agent directory, and explicitly terminate matching processes during controlled refresh cleanup. It does not define Python-kernel internals, agent readiness, or the shared `.lingtai/` disk protocol.

The package is still concrete/mixed code. Its exported functions are the current behavioral Port; this contract does not claim that an injected Core-owned Go interface migration is complete.

## Behavior

LingTai and coding agents MUST preserve the exact process/argument boundary and MUST distinguish "child process started" from "agent ready." They MUST read the neighboring process-observation contract before using a missing process-table match as evidence. They MUST NOT report an observation failure as an authoritative stopped state, restore the retired Postman transport, or add a hidden socket/RPC path to the agent.

A change to launch argv, executable selection, duplicate handling, signal-cleanup ordering, termination escalation, error identity, or start/readiness meaning is a contract change and requires synchronized implementation and conformance tests.

## Port

The current lifecycle Port is the exported API in `tui/internal/process`:

- `LaunchAgent(lingtaiCmd, agentDir string) (*exec.Cmd, error)` starts one agent unless advisory observation already finds a matching process. It returns `ErrAgentAlreadyRunning` for that duplicate case.
- `ForceLaunchAgent(lingtaiCmd, agentDir string) (*exec.Cmd, error)` performs the same start while bypassing only the duplicate-observation guard. Callers may use it only after they own the refresh/restart cleanup decision.
- `FindAgentProcesses(agentDir string) []AgentProcess` and `IsAgentRunning(agentDir string) bool` expose the one-agent advisory observation boundary defined by `tui/internal/processscan/CONTRACT.md`.
- `TerminateAgentProcesses(agentDir string) error` terminates every process that the observation adapter matches to the exact agent directory and returns nil when none match.

`InitProject` is adjacent legacy bootstrap behavior, not part of this lifecycle Port.

## Adapters

- `launcher.go` is the cross-platform process-start adapter built on `os/exec`.
- `check.go` adapts the lower-level process-observation contract into compatibility exports for lifecycle callers.
- `kill_unix.go` is the Unix termination adapter: SIGTERM all matches, wait up to two seconds, SIGKILL survivors, wait up to one additional second, then fail if matches remain.
- `kill_windows.go` is the Windows termination adapter: terminate each matched PID, wait up to two seconds, then fail if matches remain.
- `tui/internal/headless/spawn.go` is a principal consumer: after launch it owns process release, heartbeat/process readiness confirmation, timeout classification, and cleanup on readiness failure.

## Contract rules

1. `LaunchAgent` MUST perform the duplicate-observation check before signal cleanup, addon verification, log creation, or process start. A found match returns `ErrAgentAlreadyRunning` without starting another child.
2. Duplicate observation is advisory, not a race-free lock. A missing match or collapsed one-agent scan error MUST NOT be described as authoritative proof that the agent is stopped.
3. The launcher MUST prefer the agent manifest's existing venv Python when present; otherwise it uses the caller-supplied command. Failure to find or start a usable executable is an error, not backend readiness.
4. Before start, the launcher MUST clean stale signal files and MUST complete configured-addon verification. An addon-verification error prevents process start.
5. The process command is exactly the selected Python executable plus argument vector `-m`, `lingtai`, `run`, `<agentDir>`. No shell is interposed. `<agentDir>` is one final argument and is preserved verbatim, including Windows separators and spaces.
6. `logs/agent.log` redirection is best-effort. Failure to create/open that log does not by itself prevent process start; when open succeeds, both stdout and stderr go to that file.
7. A nil launch error means only that `cmd.Start()` succeeded. It does not promise a fresh heartbeat, an inspectable process-table row, a PID in status state, or a usable backend. The caller owns `Process.Release()` and readiness/cleanup policy.
8. Unix and Windows termination MUST target only processes matched to the exact normalized agent directory, wait for disappearance, and return an error when matching processes remain after the adapter's bounded escalation.
9. Force launch MUST remain an explicit, narrow bypass for a caller-controlled restart boundary; it MUST NOT become the default way to ignore duplicate evidence.

## Contract tests

- `launcher_test.go` fixes the exact four-element Python module argument vector and proves that a Windows-style agent directory containing spaces remains one argument.
- `check_test.go` covers exact-directory matching, end-of-line boundaries, malformed PIDs, unrelated commands, and prefix-sibling rejection through the compatibility API.
- `tui/internal/processscan/check_test.go` supplies the shared Unix/Windows observation conformance cases.
- `tui/internal/headless/spawn_test.go` covers the consumer-owned readiness, timeout, JSON error, and cleanup contract after launch.
- `tui/architecture_documents_test.go` validates this Contract's root registration, paired Anatomy link, required related files, maintenance marker, and body headings.

## Maintenance

Keep lifecycle behavior, both platform adapters, the processscan dependency, focused tests, and the paired Anatomy synchronized. Do not broaden this contract to runtime installation, backend semantics, or the disk protocol; those interfaces require their own nearby contracts.
