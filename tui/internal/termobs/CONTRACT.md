# TerminationObservation · Contract

Package `tui/internal/termobs`. Answers exactly one question: has one
exact, previously identified process incarnation exited?

## Authority
Query only. No result grants lifecycle authority: no launch, stop,
signal, escalation, or cleanup right follows from any observation.
The caller supplies the PID; `Identify` never searches or selects.

## Inputs and results
- `Identify(pid)` pins the incarnation currently holding `pid` (PID +
  OS start token), or fails with `ErrNotFound` / a typed error.
- `Observe(inc)` returns exactly one of:
  - `running` — a process with the same PID and start token holds the PID;
  - `exited` — the exact incarnation has certainly exited (PID free,
    held under a different start token, or reported terminated);
  - `unknown` — the OS refused the observation (e.g. access denied);
  - `error` — the observation itself failed; no conclusion.

`unknown` and `error` are terminal, honest answers: never converted
into `exited`, retried internally, or escalated.

## Side effects
None. No signal is delivered on any path: POSIX observation reads
procfs (Linux) or sysctl (macOS) only; Windows requests only query and
wait rights, never the terminate right. Nothing is written.

## POSIX behavior
Linux reads start time from `/proc/<pid>/stat` field 22; macOS reads
`kern.proc.pid` via sysctl. PID absent → `exited`; PID present with a
different start time → `exited` (reused); permission failure →
`unknown`. An exited-but-unreaped (zombie) process still holds its PID
and start time and reports `running` until reaped.

## Windows behavior
`OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION|SYNCHRONIZE)`:
invalid-parameter (PID absent) → `exited`; access denied → `unknown`;
creation-time mismatch → `exited`; else a zero-timeout wait on the
handle: signaled → `exited`, timeout → `running`.

## Forbidden inferences
- `running` is not proof of health, readiness, or single instance.
- `unknown`/`error` must never be read as `exited` or as permission to act.
- `exited` says nothing about exit cause, code, or restart safety;
  observation results never authorize or deliver termination.
