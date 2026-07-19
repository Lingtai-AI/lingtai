# AgentActionState · Policy

## Authority
Computes start/restart/refresh enablement for one agent working directory
from this interface's own heartbeat observation. Advisory policy only: it
authorizes nothing else, and it never launches, stops, signals, or writes.

## Inputs
`Evaluate(dir, thresholdSec)` — one agent working directory plus the
freshness threshold in seconds.

## Results
`Decision{Observation, CanStart, CanRestart, CanRefresh, Reason}`.
Observation is exactly one of Running / Stopped / Unknown; Reason states
the evidence in one line.

- Running (heartbeat fresh): only CanRefresh.
- Stopped (heartbeat file proven absent, or valid timestamp older than
  threshold): CanStart and CanRestart.
- Unknown: every enablement false.

## Unknown / error
Unreadable heartbeat (any error other than proven absence), malformed
timestamp, and future timestamp are Unknown. Malformed or failed
observation is never reported as Stopped and never enables any action.

## Side effects
None. Read-only single file read; no process creation, signaling, locks,
or writes.

## POSIX behavior
`adapter_posix.go`: absence is proven only by ENOENT/ENOTDIR (or an
`fs.ErrNotExist`-classified error) from reading `.agent.heartbeat`.

## Windows behavior
`adapter_windows.go`: absence is proven only by ERROR_FILE_NOT_FOUND /
ERROR_PATH_NOT_FOUND (or `fs.ErrNotExist`). All other errors are Unknown.

## Forbidden inferences
Enablement must not be inferred from process scans, PIDs, locks, or any
other interface. Running does not imply health; Stopped does not imply a
clean exit or that a later start will succeed; Unknown must not be
downgraded to Stopped to unblock an action.
