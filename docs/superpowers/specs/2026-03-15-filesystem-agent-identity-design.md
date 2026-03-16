# Filesystem-based Agent Identity

**Date:** 2026-03-15
**Status:** Approved

## Principle

An agent's identity is its working directory. Folder = agent, agent = folder.

- **New agent** = new folder created at `base_dir / agent_id`
- **Resume agent** = same constructor call, finds existing folder, restores state
- **All agents are peers** — delegate children are siblings, not nested

## Constructor Changes

### Remove
- `working_dir` parameter

### Add
- `base_dir` (required, `str | Path`) — shared root for all agents

### Computed
- `working_dir` = property returning `base_dir / agent_id`

### Resume Behavior
- `role` and `ltm` optional — if omitted and `.agent.json` exists, auto-restored from manifest
- Explicit `role`/`ltm` values override manifest

```python
# Fresh
agent = BaseAgent(agent_id="alice", service=llm, base_dir="./workspace")

# Resume (reads role/ltm from ./workspace/alice/.agent.json)
agent = BaseAgent(agent_id="alice", service=llm, base_dir="./workspace")

# Resume with override
agent = BaseAgent(agent_id="alice", service=llm, base_dir="./workspace", role="new role")
```

## Folder Layout

```
base_dir/
  alice/
    .agent.json       ← metadata (role, ltm, address, started_at)
    .agent.lock        ← OS file lock (alive = locked)
    mailbox/
      inbox/
      sent/
      read.json
  bob/
    .agent.json
    .agent.lock
    mailbox/
  alice_child_8302/
    .agent.json
    .agent.lock
    mailbox/
```

## `.agent.json` — Identity

Read on resume, written at construction, updated on stop. Lives at `working_dir/.agent.json`.

```json
{
  "agent_id": "alice",
  "address": "127.0.0.1:8301",
  "started_at": "2026-03-15T17:25:00Z",
  "role": "You are a climate science researcher.",
  "ltm": "User prefers bullet points."
}
```

No PID — liveness is handled by `.agent.lock`.

### Lifecycle
- **Construction**: read role/ltm if present, overwrite with new `started_at` and `address`.
- **Stop**: update `ltm` with latest value from prompt manager, keep file for future resume.

## `.agent.lock` — Liveness

OS-managed file lock. The lock IS the heartbeat — if you can acquire it, the previous agent is dead.

### Mechanism
- **Construction**: open `working_dir/.agent.lock`, acquire exclusive non-blocking lock. If acquisition fails → `RuntimeError("Working directory already in use by another agent")`.
- **Stop**: close the file descriptor → OS releases the lock.
- **Crash**: OS releases the lock automatically when the process dies.

### Cross-platform
```python
import sys

if sys.platform == "win32":
    import msvcrt
    def _lock(fd): msvcrt.locking(fd.fileno(), msvcrt.LK_NBLCK, 1)
    def _unlock(fd): msvcrt.locking(fd.fileno(), msvcrt.LK_UNLCK, 1)
else:
    import fcntl
    def _lock(fd): fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
    def _unlock(fd): fcntl.flock(fd, fcntl.LOCK_UN)
```

Two files, clean separation: `.agent.json` = who am I, `.agent.lock` = am I alive.

## Resume Flow

1. `BaseAgent(agent_id="alice", service=llm, base_dir="./workspace")`
2. Compute `working_dir = base_dir / "alice"`, create directory if needed
3. Try to acquire `.agent.lock` → succeeds (previous agent dead or first run) or raises
4. If `.agent.json` exists → read `role` and `ltm`
5. Caller-passed `role`/`ltm` override manifest values (if provided)
6. Write `.agent.json` with new `started_at`, `address`, current `role`/`ltm`
7. Existing `mailbox/` is intact — agent resumes with full email history

## Delegate Changes

Delegate uses parent's `base_dir` to spawn children as peers:

```python
def _spawn(self, args):
    child_id = f"{parent.agent_id}_child_{port}"
    child = BaseAgent(
        agent_id=child_id,
        service=parent.service,
        base_dir=parent._base_dir,
        mail_service=mail_svc,
    )
```

Creates `base_dir/alice_child_8302/` with its own `.agent.json`, `.agent.lock`, `mailbox/`.

Role and ltm inherited from parent by default, overridable via delegate tool args.

## Affected Code

### `src/stoai/agent.py`
- Replace `working_dir` param with `base_dir`
- Add `working_dir` as computed property (`self._base_dir / self.agent_id`)
- Add `_acquire_lock()` / `_release_lock()` methods using file lock
- Add `.agent.json` read/write in constructor and `stop()`
- Resume logic: read manifest → apply role/ltm if not overridden by caller

### `src/stoai/capabilities/delegate.py`
- Pass `base_dir=parent._base_dir` instead of `working_dir=parent._working_dir`
- Child gets its own peer folder

### `src/stoai/services/mail.py`
- `TCPMailService(working_dir=...)` — no change needed, receives the computed `working_dir`

### Examples
- `examples/two_agents.py`, `examples/three_agents.py`, `examples/chat_agent.py`, `examples/chat_web.py`
- Replace `working_dir="."` with `base_dir="."` (each agent already has a unique `agent_id`)
- Remove the manual per-agent directory creation we added earlier

### Tests
- All `working_dir="/tmp"` → `base_dir=tmp_path`
- ~50 tests across `test_agent.py`, `test_layers_*.py`, `test_three_agent_email.py`
- Mechanical changes, no logic changes
- Add new tests for: lock acquisition, lock conflict, resume from manifest, delegate peer folders

## Backward Compatibility

This is a breaking API change: `working_dir` is removed, `base_dir` is required. All callers must update. No backward-compatibility shim — clean migration per project conventions.
