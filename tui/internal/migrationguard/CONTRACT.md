# MigrationGuard · Contract

## Authority
MigrationGuard answers exactly one question: may one offline
migration/destructive action proceed against one agent working directory
right now? Its only admissible evidence is a non-creating probe of the
kernel working-directory lock `<agent dir>/.agent.lock`. It owns no other
lifecycle decision and grants nothing beyond the single probed action.

## Inputs and results
- Input: one agent working-directory path.
- Result: `Decision{Verdict, Reason}`, `Verdict` ∈ Allow | Block | Unknown.
- `Decision.Permitted()` is true only for an explicit Allow.

## Unknown and errors
Any probe outcome that is not positive lock evidence (permission errors,
invalid paths, unexpected syscall errors) yields Unknown with a reason.
Unknown blocks; it is never coerced to Allow or Block. The zero value of
`Verdict` is Unknown, so an uninitialized decision cannot permit.

## Side effects
The probe never creates, truncates, writes, or deletes the lock file. On a
free lock it holds the probe lock only for the instant needed to test it
and releases it before returning. A verdict is evidence only for the moment
of the probe: callers must recheck immediately before acting.

## POSIX behavior
Open `.agent.lock` read-only without O_CREAT. Missing file → Allow (the
kernel creates it on acquire). `flock(LOCK_EX|LOCK_NB)` success → Allow
(released at once); EWOULDBLOCK/EAGAIN → Block; anything else → Unknown.

## Windows behavior
Open `.agent.lock` with OPEN_EXISTING, GENERIC_READ, full sharing. Missing
file → Allow. `LockFileEx(EXCLUSIVE|FAIL_IMMEDIATELY)` on byte 0 (the byte
the kernel range-locks) success → Allow (unlocked at once);
ERROR_LOCK_VIOLATION → Block; anything else → Unknown.

## Forbidden inferences
- Process-table observation, PID files, heartbeats, or record age never
  affect the verdict.
- Allow does not mean the agent is "stopped"; it means only that the lock
  was free at probe time.
- Block or Unknown must not be "repaired" by deleting the lock file.
