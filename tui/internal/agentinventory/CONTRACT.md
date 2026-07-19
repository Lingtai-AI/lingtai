# AgentInventory · Contract

## Authority
AgentInventory answers one advisory query: which agent directories exist under
one project `.lingtai` root, and what each directory's own on-disk record plus
one native PID existence probe say about it. Records are advisory only and
never authorize launch, stop, migration, purge, or any destructive action.

## Inputs / results
`Query(root)` takes one project `.lingtai` directory and returns one sorted
`Record` per subdirectory holding `.agent.json`:
- `running` — the recorded PID exists right now (identity not proven);
- `absent` — no recorded claim of running and the recorded PID is gone;
- `unknown` — manifest unreadable/invalid, no recorded PID, or probe
  undecided; `Detail` says why;
- `conflict` — record and probe disagree (recorded running but PID gone, or
  recorded stopped but PID alive); `Detail` says which.

## Unknown / error
Query failure (root unreadable) returns a typed `*QueryError` and no records —
never a silent empty list. "No agents" is nil error with an empty list.

## Side effects
None. Read-only: one directory listing, two file reads per agent directory,
one non-signalling PID existence probe.

## POSIX behavior
`kill(pid, 0)`: success or `EPERM` → PID exists; `ESRCH` → gone; else unknown.

## Windows behavior
`OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION)` + `GetExitCodeProcess`:
still-active or `ERROR_ACCESS_DENIED` → PID exists; open failing with
`ERROR_INVALID_PARAMETER` or a non-active exit code → gone; else unknown.

## Forbidden inferences
`running` is not agent health, readiness, or identity (PIDs are reused).
`absent` is not permission to reuse, clean, or relaunch anything. `unknown`
and `conflict` must never be coerced to `running` or `absent`.
