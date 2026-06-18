# Headless Runtime Contract

This document describes the public controller contract for `lingtai-tui spawn`
and `lingtai-tui list`.

## Spawn Readiness

`lingtai-tui spawn <directory> --preset <name>` creates the project and launches
the agent, then waits for both conditions:

- an inspectable `lingtai run <agent-dir>` process exists
- `.agent.heartbeat` exists and is fresh

Success returns JSON with:

- `status: "ready"`
- `readiness_status: "ready"`
- `inspectable_process_confirmed: true`
- `heartbeat_confirmed: true`
- `pid` from `.status.json.runtime.pid` when available

`spawn` no longer treats `cmd.Start()` as success. Timeout or early exit returns
typed JSON errors such as `readiness_timeout` or
`process_exited_before_ready`. On readiness failure, the TUI attempts best-effort
cleanup of the launched agent processes.

## List JSON

`lingtai-tui list --json [project-dir]` emits structured process data for
controllers. Each process entry includes agent identity, `agent_dir`,
heartbeat freshness, and `lock_exists`.

On Windows, process discovery uses WMIC when available and PowerShell CIM as a
fallback. When a venv parent process and runtime child process both appear for
the same `agent_dir`, list output deduplicates by `agent_dir` and prefers
`.status.json.runtime.pid`.

## Mailbox Probe

Use the kernel runtime probe contract documented in `lingtai-kernel`. A
successful controller probe should observe:

- probe folder moved from `human/mailbox/outbox/<id>/` to
  `human/mailbox/sent/<id>/`
- probe copied to `<agent>/mailbox/inbox/<id>/message.json`
- structured `runtime_probe_ack` written to `human/mailbox/inbox/`

## Cleanup

Stop is verified by writing `.suspend` or using existing lifecycle controls,
then confirming `lingtai-tui list --json <project-dir>` returns `count: 0`.

Runtime state, `.lingtai/`, mailbox files, logs, temp dirs, env files, secrets,
prompts, hidden prompts, chain-of-thought, raw reasoning, and daemon raw logs
must remain untracked.
