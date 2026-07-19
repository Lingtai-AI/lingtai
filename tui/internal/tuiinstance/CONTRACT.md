# TUIInstanceGuard · Contract

## Authority
Sole authority over the TUI singleton ownership token for one scope
directory. Only a token returned as `Acquired` permits protected TUI
startup. This interface has no authority over processes, launches,
migrations, or any other decision.

## Inputs / results
`Acquire(dir)` attempts to take the token scoped to `dir` and returns an
`Acquisition` with exactly one of:
- `Acquired` — `Token` is non-nil; caller may start the protected TUI and
  must call `Token.Release()` when the TUI ends.
- `Contended` — a live holder already owns the token; caller must refuse
  startup. `Token` is nil.
- `Unknown` — the attempt could not prove either outcome (open/lock
  failure, missing scope dir, ...); caller must refuse startup, with a
  message distinct from Contended. `Token` is nil, `Detail` says why.

## Unknown / error
Unknown is a first-class refusal, never downgraded to Acquired or
Contended. There is no retry, wait, or steal path.

## Side effects
Acquire may create the lock file `tui-instance.lock` inside `dir`.
Release drops the kernel lock and closes the handle; it never deletes
the file. Process death releases the kernel lock automatically.

## POSIX behavior
`open(O_CREAT|O_RDWR|O_CLOEXEC)` then `flock(LOCK_EX|LOCK_NB)`.
`EWOULDBLOCK`/`EAGAIN` → Contended; any other failure → Unknown.

## Windows behavior
`CreateFileW` with share mode 0 (no sharing); the held handle itself is
the lock. `ERROR_SHARING_VIOLATION`/`ERROR_LOCK_VIOLATION` → Contended;
any other failure → Unknown.

## Forbidden inferences
- Process scans, PID hints, or process names are never evidence.
- Lock-file existence, age, or content is never evidence; only the live
  kernel lock decides.
- Deleting the lock file is never a bypass and must not be offered.
- Contended/Unknown must never be treated as "probably free".
