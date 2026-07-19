# AgentCounter · Query — Contract

## Authority
- Counts running semantic agents from this interface's own file evidence:
  the project registry `<globalDir>/registry.jsonl`, per-agent manifests
  `<project>/.lingtai/<agent>/.agent.json`, and heartbeats
  `<project>/.lingtai/<agent>/.agent.heartbeat`.
- Process observation (ps/WMIC/handles/PIDs) is NOT authority and is never consulted.
- Read-only: counting never creates, repairs, prunes, or deletes any file.

## Inputs / Results
- Input: `globalDir`, the TUI global config directory holding `registry.jsonl`.
- Result `Count{State, Agents, Issues}`:
  - `StateKnown`: `Agents` = manifests whose `admin` field is present and
    non-null (semantic agent, not the human mailbox) with a heartbeat younger
    than 2 seconds. `Issues` may still list skipped or degraded evidence.
  - `StateUnknown`: the count is untrustworthy; `Agents` carries no meaning.

## Unknown / error
- Registry unreadable for any reason other than "does not exist" →
  `StateUnknown` plus an issue naming the failure.
- Registry file absent → `StateKnown` with 0 (nothing registered is a real zero).
- Malformed registry row, unreadable/malformed manifest, or malformed
  heartbeat → recorded in `Issues`, that row skipped, `State` stays Known.
- Failure never passes silently as zero: every degradation surfaces as
  `StateUnknown` or an `Issues` entry.

## Side effects
- None. Pure file reads.

## POSIX behavior
- `adapter_posix.go` reads registry/manifest/heartbeat with POSIX file semantics.

## Windows behavior
- `adapter_windows.go` duplicates the same reads privately with Windows file
  semantics; no WMIC, PowerShell, or process enumeration of any kind.

## Forbidden inferences
- A process count may not substitute for, correct, or veto this count.
- A missing heartbeat means "not running now", never "exited" or "safe to act".
- The count authorizes nothing; it is display advice for the startup reminder.
- `StateUnknown` must not be rendered as zero without surfacing the failure.
