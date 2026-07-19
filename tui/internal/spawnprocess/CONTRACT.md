# SpawnProcess · Command

## Authority
Create at most one child process from one typed specification. Authority
covers only this single creation event. This interface never retries,
polls, signals, stops, waits on, or observes processes.

## Input
`Spec`: executable path, argument list, environment (nil inherits the
parent's), working directory (empty inherits the parent's), and optional
stdout/stderr files (nil means the OS null device).

## Result
On success, a `Child`: the OS-assigned PID plus the `*os.Process` handle
returned by the creation call. On failure, a typed `*Error` with kind
`invalid_spec` (rejected before any OS call), `not_found` (executable does
not exist), or `start` (the OS refused the creation call). A failure means
no child was created by this call.

## Side effects
Exactly one OS process-creation call per successful `Spawn`. No files,
locks, records, or signals are written or read by this interface.

## POSIX behavior
`adapter_posix.go` creates the child via fork/exec (through `os/exec`).
The child joins the parent's process group and session. The parent does
not wait; reaping the child is the caller's concern.

## Windows behavior
`adapter_windows.go` creates the child via `CreateProcess` (through
`os/exec`). No creation flags are set, so the child shares the parent's
console. The returned handle stays open until the caller releases it.

## Forbidden inferences
- Success is not readiness: the child may not have initialized, and may
  already have exited.
- Success is not ownership or exclusivity: this interface does not know
  or care whether a similar process already runs.
- Success is not duplicate-launch safety: callers needing that must
  obtain it elsewhere, before calling Spawn.
- `Child.PID` is identity at creation time only; the OS may reuse it
  after exit. Holding `Child` proves nothing about liveness.
