# DuplicateLaunchCheck · Contract

Policy interface: decide `allow` / `block` / `unknown` for launching one
agent into one working directory. Callers: the `process.LaunchAgent` gate and
Windows lifecycle lock-clear probes before controlled refresh/revival mutation.

## Authority
- Sole authority: this package's private, non-creating probe of the kernel's
  `.agent.lock` lease inside the target workdir.
- Process observation (process lists, PIDs, command lines) is never authority
  and is never consulted.
- `allow` covers only the duplicate-launch question; it promises nothing
  about spawn success, readiness, or ownership.

## Inputs / results
- Input: one agent working directory path.
- Result: `Decision{Verdict, Reason}`; verdict is `allow`, `block`, or
  `unknown`; `Reason` names the lock evidence observed.

## Unknown / error
- Any probe failure yielding no usable lock evidence (unreadable lease,
  unexpected errno/last-error) is `unknown`, never `allow`.
- Callers must treat `unknown` exactly like `block`: refuse the launch.

## Side effects
- Never creates, truncates, writes, or deletes the lock file or anything
  else; a missing lease stays missing after the probe.
- The probe may momentarily acquire and immediately release the lock on an
  existing lease file; it never holds it past the call.

## POSIX behavior (adapter_posix.go)
- Open `.agent.lock` read-only, no `O_CREAT`. Missing file → `allow`;
  unreadable → `unknown`.
- Non-blocking `flock(LOCK_EX)`: `EWOULDBLOCK`/`EAGAIN` → `block`; success →
  release and `allow` (stale lease); other errno → `unknown`.

## Windows behavior (adapter_windows.go)
- Open `.agent.lock` without creation. Missing → `allow`; sharing violation →
  `block`; other open failure → `unknown`.
- `LockFileEx` (exclusive, fail-immediately) on byte 0 length 1 — the byte a
  running kernel's msvcrt region lock holds: lock violation → `block`;
  success → `UnlockFileEx` and `allow`; other last-error → `unknown`.

## Forbidden inferences
- No verdict from process scans, PID hints, record age, or lease existence
  alone — an unheld lease is stale, not `block`.
