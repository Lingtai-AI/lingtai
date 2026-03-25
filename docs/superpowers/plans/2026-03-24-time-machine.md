# Time Machine — Periodic Git Snapshots for Agent Working Directory

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace individual `diff_and_commit` calls with periodic whole-directory git snapshots, like Apple Time Machine. Track everything — no `.gitignore` exclusions. Prune old snapshots to keep repo size manageable.

**Architecture:** `WorkingDir.snapshot()` does `git add -A && git commit`. The heartbeat thread calls it every 5 minutes. `WorkingDir.prune_snapshots()` thins out old commits (keep all within last hour, hourly for last day, daily beyond). `.gitignore` is simplified to track everything. Individual `diff_and_commit` calls throughout the codebase are removed — the snapshot catches all changes.

**Tech Stack:** Python 3.11+, git. No new dependencies.

---

## File Structure

| File | Action | Responsibility |
|------|--------|----------------|
| `lingtai-kernel: workdir.py` | Modify | Add `snapshot()`, `prune_snapshots()`, simplify `init_git()` `.gitignore` |
| `lingtai-kernel: base_agent.py` | Modify | Add snapshot call to heartbeat, remove individual `diff_and_commit` calls |

---

### Task 1: Simplify `.gitignore` — track everything

**Files:**
- Modify: `../lingtai-kernel/src/lingtai_kernel/workdir.py`

Currently `.gitignore` uses `*` (ignore all) then whitelists specific directories. Change to track everything — only ignore `.git/` (implicit) and transient files that cause commit noise.

- [ ] **Step 1: Update `init_git()` `.gitignore` content**

In `workdir.py`, replace the `.gitignore` content in `init_git()`:

```python
gitignore = self._path / ".gitignore"
gitignore.write_text(
    "# Transient process files\n"
    ".agent.lock\n"
)
```

That's it. Everything else is tracked. The lock file is excluded because it's held via `flock()` and its content is meaningless.

- [ ] **Step 2: Smoke-test**

Run: `python -c "import lingtai_kernel.workdir; print('ok')"`
Expected: `ok`

- [ ] **Step 3: Commit**

```bash
cd ../lingtai-kernel
git add src/lingtai_kernel/workdir.py
git commit -m "refactor: simplify .gitignore — track everything (Time Machine)"
cd ../lingtai
```

---

### Task 2: Add `WorkingDir.snapshot()`

**Files:**
- Modify: `../lingtai-kernel/src/lingtai_kernel/workdir.py`

A single method that commits the entire working directory state.

- [ ] **Step 1: Implement `snapshot()`**

Add to `WorkingDir`:

```python
def snapshot(self) -> str | None:
    """Commit entire working directory state. Returns commit hash or None.

    No-op if nothing changed. Like Apple Time Machine — captures everything.
    """
    try:
        # Stage everything
        subprocess.run(
            ["git", "add", "-A"],
            cwd=self._path, capture_output=True, check=True,
        )

        # Check if there's anything to commit
        status = subprocess.run(
            ["git", "diff", "--cached", "--quiet"],
            cwd=self._path, capture_output=True,
        )
        if status.returncode == 0:
            return None  # nothing staged

        # Commit with ISO timestamp
        from datetime import datetime, timezone
        ts = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        subprocess.run(
            ["git", "commit", "-m", f"snapshot {ts}"],
            cwd=self._path, capture_output=True, check=True,
        )

        result = subprocess.run(
            ["git", "rev-parse", "--short", "HEAD"],
            cwd=self._path, capture_output=True, text=True,
        )
        return result.stdout.strip()

    except (FileNotFoundError, subprocess.CalledProcessError):
        return None
```

- [ ] **Step 2: Smoke-test**

Run: `python -c "from lingtai_kernel.workdir import WorkingDir; print('ok')"`
Expected: `ok`

- [ ] **Step 3: Commit**

```bash
cd ../lingtai-kernel
git add src/lingtai_kernel/workdir.py
git commit -m "feat: WorkingDir.snapshot() — whole-directory git commit"
cd ../lingtai
```

---

### Task 3: Add `WorkingDir.prune_snapshots()`

**Files:**
- Modify: `../lingtai-kernel/src/lingtai_kernel/workdir.py`

Thin out old snapshot commits to keep repo size manageable. Uses `git rebase` to squash old snapshots while keeping the content of the most recent one in each time bucket.

Actually — git rebase on automated commits is fragile and dangerous. Simpler approach: **just let git accumulate commits and rely on `git gc` for storage efficiency.** Git pack files are very efficient at storing incremental changes to text files. A commit is ~200 bytes of overhead. 288 commits/day (every 5 min) = ~57KB/day of commit overhead. Negligible.

If repo size ever becomes a concern, the operator can run `git gc --aggressive` or we can add a `gc()` call to the heartbeat (much less frequently, e.g., daily).

- [ ] **Step 1: Add `gc()` method to WorkingDir**

```python
def gc(self) -> None:
    """Run git garbage collection to optimize repo storage."""
    try:
        subprocess.run(
            ["git", "gc", "--auto"],
            cwd=self._path, capture_output=True,
            timeout=60,
        )
    except (FileNotFoundError, subprocess.CalledProcessError, subprocess.TimeoutExpired):
        pass
```

- [ ] **Step 2: Commit**

```bash
cd ../lingtai-kernel
git add src/lingtai_kernel/workdir.py
git commit -m "feat: WorkingDir.gc() — periodic git garbage collection"
cd ../lingtai
```

---

### Task 4: Wire snapshot into heartbeat

**Files:**
- Modify: `../lingtai-kernel/src/lingtai_kernel/base_agent.py`

Call `snapshot()` every 5 minutes from the heartbeat loop. Call `gc()` once per day.

- [ ] **Step 1: Add snapshot tracking state to `__init__`**

Add to `BaseAgent.__init__`, near the heartbeat fields:

```python
self._last_snapshot: float = 0.0
self._last_gc: float = 0.0
```

- [ ] **Step 2: Add snapshot call to `_heartbeat_loop`**

Add at the end of the heartbeat loop body, before `time.sleep(1.0)`:

```python
# Periodic snapshot — every 5 minutes
now_mono = time.monotonic()
if now_mono - self._last_snapshot >= 300:  # 5 minutes
    self._workdir.snapshot()
    self._last_snapshot = now_mono

# Periodic GC — every 24 hours
if now_mono - self._last_gc >= 86400:  # 24 hours
    self._workdir.gc()
    self._last_gc = now_mono
```

- [ ] **Step 3: Remove individual `diff_and_commit` calls**

Search for all `diff_and_commit` calls in `base_agent.py` and remove them. These are now redundant — the periodic snapshot captures everything.

Known locations:
- `_persist_chat_history()` — already removed in the deep refresh plan (Task 5)
- Any other calls in `base_agent.py` (search and remove)

Also search in lingtai's capabilities and intrinsics:
- `psyche.py` uses `diff_and_commit` for character updates — remove, snapshot catches it
- Any other capability that calls `_workdir.diff_and_commit` — remove

**Do NOT remove `diff_and_commit()` from `WorkingDir` itself** — it may still be used by capabilities that want an immediate commit with a meaningful label (e.g., after a memory edit). The snapshot system is additive.

Actually — keep `diff_and_commit` calls that have meaningful labels (like `"character"`, `"memory"`). These create semantically meaningful commits in the git log. The snapshot commits are mechanical backups. Both can coexist. The only calls to remove are the ones in `_persist_chat_history()` (already handled in deep refresh plan).

- [ ] **Step 4: Smoke-test**

Run: `python -c "import lingtai_kernel.base_agent; print('ok')"`
Expected: `ok`

- [ ] **Step 5: Commit**

```bash
cd ../lingtai-kernel
git add src/lingtai_kernel/base_agent.py
git commit -m "feat: periodic git snapshots every 5 minutes via heartbeat"
cd ../lingtai
```

---

### Task 5: Final verification

- [ ] **Step 1: Run kernel tests**

Run: `cd ../lingtai-kernel && python -m pytest tests/ -v`
Expected: All tests pass.

- [ ] **Step 2: Run lingtai tests**

Run: `cd ../lingtai && python -m pytest tests/ -v`
Expected: All tests pass.

- [ ] **Step 3: Smoke-test full import chain**

Run: `python -c "import lingtai; import lingtai_kernel.workdir; print('ok')"`
Expected: `ok`
