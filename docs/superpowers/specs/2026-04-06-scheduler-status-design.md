# Email Scheduler Status — Explicit Lifecycle Refactor

**Date:** 2026-04-06
**Status:** Draft
**Scope:** `lingtai-kernel/src/lingtai/capabilities/email.py` — schedule cancel/recover semantics
**Supersedes (partially):** `2026-03-27-schedule-service-and-refresh-restart-design.md` — replaces the `.cancel` sentinel-file mechanism with an explicit `status` field on the schedule record.

---

## Problem

The email scheduler currently encodes schedule lifecycle implicitly across two channels:

1. **`sent < count`** — running
2. **Presence of `.cancel` files** — cancelled (either `mailbox/schedules/.cancel` for agent-level, or `mailbox/schedules/{id}/.cancel` per-schedule)

There is no explicit "paused" state. Two real-world consequences:

- **Auto-resume on restart is unconditional.** When a new `EmailManager` starts up, any in-flight schedule (`sent < count`, no `.cancel` file) immediately starts firing again. There is no opportunity for a human to inspect, decide whether the schedule is still wanted, and consciously resume it. After a multi-hour downtime the agent can suddenly send a barrage of catch-up emails to a recipient who may no longer want them.
- **Cancel and resume are asymmetric.** A user can cancel a schedule by touching a file, but there is no way to undo a cancellation other than to delete the `.cancel` file by hand. There is no "I changed my mind, restart this schedule" path through the agent's tool surface.

This refactor introduces an explicit `status` field on each schedule record, replaces the sentinel-file mechanism with status mutations, and adds a `reactivate` action to the email tool. On every agent startup, all non-completed schedules are automatically flipped to `inactive`, forcing a deliberate reactivation.

---

## Background — relationship to the 2026-03-27 design

The `2026-03-27-schedule-service-and-refresh-restart-design.md` spec introduced the `.cancel` sentinel-file approach as a way to make schedule state durable across in-process refresh and out-of-process restart. That design solved a real bug (duplicate scheduler threads after refresh) and is still load-bearing in its core insight: **disk is the only coordination mechanism**.

This refactor preserves that core insight. The scheduler is still a single disk-driven polling thread. The disk record is still the single source of truth. The only change is **how** that disk record encodes lifecycle state: a `status` string instead of the presence/absence of sentinel files.

The two `.cancel` file paths defined in the prior spec (`mailbox/schedules/.cancel` and `mailbox/schedules/{id}/.cancel`) are removed from the protocol. Stale `.cancel` files left behind on disk by the old code are not cleaned up — they become dead files that nothing reads.

---

## State Machine

```
        ┌─────── reactivate ───────┐
        v                          │
   ┌────────┐                 ┌──────────┐
   │ active │ ──── pause ──>  │ inactive │
   └───┬────┘                 └──────────┘
       │
       │ sent == count
       v
   ┌──────────┐
   │completed │
   └──────────┘
```

**Three states only.** There is no `cancelled` state — cancel is the same operation as startup reconciliation: flip the record to `inactive`. A user who cancels a schedule today can reactivate it tomorrow.

### States

- **`active`** — scheduler ticks this record and fires sends when due. Only state from which a send can occur.
- **`inactive`** — scheduler skips this record. Reversible via the `reactivate` action.
- **`completed`** — terminal. Set when `sent >= count` after a successful send inside the tick.

### Transitions

| From | To | Trigger | Side effect |
|---|---|---|---|
| `active` | `inactive` | Startup reconciliation | Sweep at agent startup, before scheduler thread starts |
| `active` | `inactive` | User `cancel` action | Per-schedule or cancel-all |
| `inactive` | `active` | User `reactivate` action | **Resets `last_sent_at = now`** so next send fires one full interval after reactivation |
| `active` | `completed` | Successful send inside tick | Set when `seq >= count` after the increment |

### Reactivate guard rules

| Current state | Result |
|---|---|
| not found | `{"error": "Schedule not found: {id}"}` |
| `completed` | `{"error": "Cannot reactivate a completed schedule"}` |
| `active` | `{"status": "already_active", "schedule_id": id}` (noop) |
| `inactive` BUT `sent >= count` (crash-mid-completion case) | self-heal: set status to `completed`, return `{"error": "Cannot reactivate a completed schedule"}` |
| `inactive` (or missing-status legacy) | flip to `active`, reset `last_sent_at = now`, return `{"status": "reactivated", "schedule_id": id}` |

### Cancel rules

**`cancel` with `schedule_id`:**

| Current state | Result |
|---|---|
| not found | `{"error": "Schedule not found: {id}"}` |
| `inactive` | `{"status": "already_inactive", "schedule_id": id}` (noop) |
| `completed` | `{"status": "already_completed", "schedule_id": id}` (noop) |
| `active` (or missing-status legacy) | flip to `inactive`, return `{"status": "paused", "schedule_id": id}` |

**`cancel-all` (no `schedule_id`):**
Iterate every schedule directory. For each record: skip if `status` is `inactive` or `completed`; otherwise (active or missing) set to `inactive`. Return `{"status": "paused", "message": "All active schedules paused"}`.

**Note on response vocabulary:** The action name is still `cancel` (for backward compatibility with the existing tool schema), but the response uses `"paused"` rather than `"cancelled"` to reflect the actual state transition. There is no `cancelled` state — the operation moves a schedule to `inactive`, which is reversible. The agent's prompt-level understanding of "cancel" still works (it's still a valid synonym for "stop sending"), but any agent that inspects the response status code will see `"paused"` and learn that the operation is reversible via `reactivate`.

### Reactivation timing — `last_sent_at = now`

When a schedule is reactivated, `last_sent_at` is reset to the current time. This means the next send fires **one full interval after the reactivation moment**, not immediately, and not on the original cadence. Rationale: the user who reactivates an old schedule should not get a surprise burst of catch-up sends, and the cadence should be predictable from the reactivation event. The "missed window" (sends that would have happened during the inactive period) is silently dropped — they were never wanted, that's the whole point of the inactive state.

---

## On-Disk Schema

**Schedule record (`mailbox/schedules/{id}/schedule.json`):**

```json
{
  "schedule_id": "abc123def456",
  "send_payload": { ... },
  "interval": 3600,
  "count": 10,
  "sent": 3,
  "created_at": "2026-04-06T10:00:00Z",
  "last_sent_at": "2026-04-06T13:00:00Z",
  "status": "active"
}
```

The only schema change is the new `status` string field, with values `"active" | "inactive" | "completed"`.

**Default for missing status:** Readers MUST treat a missing `status` key as if it were `"active"`. This is the legacy migration path for records created by the old code. After one boot, all legacy records have an explicit `status: "inactive"` written by reconciliation, and the missing-key fallback is dormant. We leave it in place as defense-in-depth.

**No schema version field.** The presence/absence of `status` is itself the version signal, and it self-heals on first boot.

**Files removed from the protocol:**

- `mailbox/schedules/.cancel` — gone. No reads, no writes.
- `mailbox/schedules/{id}/.cancel` — gone. No reads, no writes.

Stale `.cancel` files left behind on disk by the old code are **not** cleaned up. Harmless leftover.

---

## Concurrency — known race, accepted

The scheduler thread (`_scheduler_loop`) and the `handle()` thread (which services tool calls from the agent's main message loop) both read and write `schedule.json`. The current code has no per-record lock; it relies on atomic rename for write atomicity but accepts lost-update risk for read-modify-write sequences.

**This refactor does not change the concurrency model.** A `cancel` racing against an in-flight `_scheduler_tick` send-completion can produce one of two outcomes:

1. **Cancel wins** — record is `inactive`, but the in-flight send still completes (its `_write_schedule` already happened).
2. **Tick wins** — tick reads record (status=active, sent=3), starts a send, completes, writes record with sent=4 and status=active (because it's working from its stale in-memory copy). The cancel is silently lost.

In outcome 2, the agent can detect the lost cancel by calling `schedule.list` and observing that the record is still `active`. The agent then re-issues `cancel`. At most one extra send slips through per failed cancel attempt. This is self-correcting from the agent's perspective and acceptable for the use case (low-frequency tool actions, not a high-throughput pipeline).

A proper fix would require per-record locking or a re-read-and-merge pattern in the tick. Both are larger than this refactor's scope and are explicitly out of scope.

---

## Code Changes

All changes are in **`lingtai-kernel/src/lingtai/capabilities/email.py`**. No new files.

### 1. `get_schema()` (lines 122-143)

- Extend the `schedule.action` enum: `["create", "cancel", "list"]` → `["create", "cancel", "list", "reactivate"]`.
- The `schedule_id` parameter already exists; only its description string changes (handled via i18n).

### 2. `_handle_schedule()` (line 350)

Add one branch:

```python
elif action == "reactivate":
    return self._schedule_reactivate(schedule)
```

### 3. `_schedule_create()` (line 361)

Add `"status": "active"` to the record dict (around line 397).

### 4. `_set_schedule_status()` — NEW helper (after `_read_schedule`, around line 500)

```python
def _set_schedule_status(self, schedule_id: str, status: str) -> bool:
    """Update the status of a schedule record on disk. Returns True on success."""
    record = self._read_schedule(schedule_id)
    if record is None:
        return False
    record["status"] = status
    sched_file = self._schedules_dir / schedule_id / "schedule.json"
    self._write_schedule(sched_file, record)
    return True
```

### 5. `_schedule_reactivate()` — NEW method

Implements the guard rules above. Does NOT use `_set_schedule_status` internally because it needs to mutate both `status` and `last_sent_at` in one write. Reads the record once, mutates both fields, writes once.

Includes a `sent >= count` self-healing guard: if the record has already finished sending but its status was not properly set to `completed` (due to a process crash between the at-most-once increment and the completion write), reactivate refuses AND self-heals the record by setting `status = "completed"` on disk.

```python
def _schedule_reactivate(self, schedule: dict) -> dict:
    schedule_id = schedule.get("schedule_id")
    if not schedule_id:
        return {"error": "schedule_id is required for reactivate"}
    record = self._read_schedule(schedule_id)
    if record is None:
        return {"error": f"Schedule not found: {schedule_id}"}
    status = record.get("status", "active")
    if status == "completed":
        return {"error": "Cannot reactivate a completed schedule"}
    if status == "active":
        return {"status": "already_active", "schedule_id": schedule_id}
    # Self-heal crashed-mid-completion records
    sent = record.get("sent", 0)
    count = record.get("count", 0)
    if sent >= count:
        record["status"] = "completed"
        sched_file = self._schedules_dir / schedule_id / "schedule.json"
        self._write_schedule(sched_file, record)
        return {"error": "Cannot reactivate a completed schedule"}
    # status is "inactive" (or any non-terminal value, including legacy missing)
    now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    record["status"] = "active"
    record["last_sent_at"] = now
    sched_file = self._schedules_dir / schedule_id / "schedule.json"
    self._write_schedule(sched_file, record)
    return {"status": "reactivated", "schedule_id": schedule_id}
```

### 6. `_reconcile_schedules_on_startup()` — NEW method

Sweeps `self._schedules_dir`. For each `schedule.json`:

- Skip if status is `"completed"` (terminal, leave alone).
- Skip if status is `"inactive"` (already in safe state, no spurious write).
- Otherwise (status is `"active"`, missing, or any unknown value) → set to `"inactive"`.

Per-record errors (corrupt JSON, missing file, OS errors) are silently swallowed via `continue` — corrupt records should not block agent startup.

### 7. `setup()` (line 1091) — call reconciliation BEFORE scheduler starts

```python
mgr = EmailManager(agent, private_mode=private_mode)
agent.override_intrinsic("mail")
agent._mailbox_name = "email box"
agent._mailbox_tool = "email"
agent.add_tool("email", schema=get_schema(lang), handler=mgr.handle, ...)
mgr._reconcile_schedules_on_startup()   # NEW — before start_scheduler
mgr.start_scheduler()
return mgr
```

**Decision D1:** Reconciliation runs **before** `start_scheduler()`, not after. Reason: avoids a window where the scheduler thread is alive and could tick a record before reconciliation flips it to inactive. The original plan from `docs/plans/scheduler-status-plan.md` had this in the wrong order.

### 8. `_scheduler_tick()` (line 510)

Three changes:

- Add a status guard after reading the record (around line 537):
  ```python
  if record.get("status", "active") != "active":
      continue
  ```
  The `"active"` default handles legacy records that survived reconciliation (defense in depth).
- Remove the `.cancel` file checks at lines 517 (agent-level) and 527 (per-schedule).
- After the successful send and the `sent` increment when `seq >= count`, set `record["status"] = "completed"` before the final `_write_schedule` at line 618. This is a small inline addition, not a refactor — the existing 3-write pattern (lines 561, 584, 618) stays as-is.

**KEEP the existing `if sent >= count: continue` guard at line 541.** This is the at-most-once safety net for the rare case where the process crashes between the increment-write at line 561 and the status-completion-write at line 618. After such a crash, the record on disk has `sent == count` but `status == "active"`. On the next boot, reconciliation flips it to `inactive` (because status is still active). The `sent >= count` reactivate guard (see Section 5 below) catches this case at the reactivate step and self-heals the record to `completed`. The tick-level guard remains as a final defense-in-depth — both are cheap and they cover different failure modes.

### 9. `_schedule_cancel()` (line 406)

Replace the `.cancel`-file logic with status mutations:

- **`cancel-all`** (no `schedule_id`): iterate every schedule directory, read each record, skip if status is `inactive` or `completed`, else set to `inactive`. Return `{"status": "cancelled", "message": "All active schedules paused"}`.
- **`cancel` with id**: read record, branch on status (already_inactive / already_completed / not_found / set-to-inactive). See cancel rules table above.

### 10. `_schedule_list()` (line 429)

- Add `"status": record.get("status", "active")` to each entry.
- **Drop the legacy `.cancel` compat:** remove the `cancelled = (sched_dir / ".cancel").is_file() or (schedules_dir / ".cancel").is_file()` line at 453.
- **Drop the `cancelled` and `active` boolean fields entirely** from each entry. Replace with the canonical `status` string. (Decision D3.)

**Decision D3 — verified safe:** I grepped both `tui/` and `portal/` Go source for any consumer of these fields. **No Go code reads `entry["cancelled"]`, `entry["active"]`, `entry["status"]`, or any other field of the schedule list output.** The only consumer of `_schedule_list()` is the LLM reading its own tool output JSON, and switching from `{"active": true, "cancelled": false}` to `{"status": "active"}` is a prompt-comprehension change, not a code-breakage change. Safe to drop.

### 11. i18n updates (en.json, zh.json, wen.json — all three locales)

- Update `email.schedule_action` description to mention `reactivate` (currently lists "create / cancel / list").
- Update `email.schedule_id` description from "Schedule ID (for cancel)" to "Schedule ID (for cancel/reactivate)".
- No new keys needed.

Per the `feedback_i18n_three_locales` memory note, all three of `en.json`, `zh.json`, `wen.json` must be updated together.

### Net code change

| # | Location | Type | Lines (approx) |
|---|----------|------|----|
| 1 | `get_schema` | edit | 2 |
| 2 | `_handle_schedule` | edit | +2 |
| 3 | `_schedule_create` | edit | +1 |
| 4 | `_set_schedule_status` | NEW | +8 |
| 5 | `_schedule_reactivate` | NEW | +28 |
| 6 | `_reconcile_schedules_on_startup` | NEW | +20 |
| 7 | `setup()` | edit | +1 |
| 8 | `_scheduler_tick` | edit | -10 / +5 |
| 9 | `_schedule_cancel` | edit | -8 / +20 |
| 10 | `_schedule_list` | edit | -5 / +3 |
| 11 | `i18n/{en,zh,wen}.json` | edit | 2 strings × 3 files |

Net code change to `email.py`: roughly +70 / -25 lines.

---

## Test Plan

All test changes in **`lingtai-kernel/tests/test_layers_email.py`**.

### Tests to rewrite (assert behavior, not sentinel files)

| Existing test | Was asserting | Now asserts |
|---|---|---|
| `test_scheduler_cancel_via_sentinel_file` (L1347) | `(sched_dir / ".cancel").exists()` after cancel | `record["status"] == "inactive"` after cancel; scheduler stops sending |
| `test_scheduler_agent_level_cancel` (L1374) | `(schedules_dir / ".cancel").exists()` after cancel-all | every record has `status == "inactive"` after cancel-all; scheduler stops sending |
| `test_schedule_cancel_action_creates_sentinel` (L1440) | sentinel file written | record on disk has `status == "inactive"` |
| `test_schedule_cancel_all_creates_agent_sentinel` (L1462) | agent sentinel file written | every active record flipped to `inactive` on disk |
| `test_email_schedule_recovery_on_setup` (L1179) | interrupted schedule auto-resumes after restart | interrupted schedule becomes `inactive` after restart; sends do **not** resume; `reactivate` causes resumption |
| `test_email_schedule_recovery_skips_cancelled` (L1224) | `.cancel` file → no resume | record with `status: "inactive"` → no resume (and no auto-flip back); test setup writes `status` field instead of touching `.cancel` |

If any of the above tests turn out to be near-duplicates after rewriting, merge them. Estimated 4-6 net rewritten tests.

### Tests to add (new)

| Test | Asserts |
|---|---|
| `test_schedule_reactivate_inactive_resumes` | create → cancel → reactivate → status is `active`, `last_sent_at` reset to ~now, scheduler fires next send after one full interval (not immediately) |
| `test_schedule_reactivate_active_is_noop` | reactivate on `active` returns `{"status": "already_active"}` and does NOT mutate `last_sent_at` |
| `test_schedule_reactivate_completed_errors` | reactivate on completed returns `{"error": ...}`, no state change |
| `test_schedule_reactivate_not_found_errors` | reactivate with bogus id returns `{"error": "Schedule not found: ..."}` |
| `test_schedule_legacy_record_reconciled_to_inactive` | manually write a `schedule.json` with NO `status` field; new EmailManager → record becomes `status: "inactive"`; reactivate works normally afterward |
| `test_schedule_completion_sets_status_completed` | create schedule with count=2 → wait for both sends → record on disk has `status: "completed"`; subsequent reactivate fails |
| `test_schedule_reactivate_self_heals_crash_mid_completion` | manually write a schedule with `sent == count` and `status == "inactive"` (simulating a crash); reactivate returns `Cannot reactivate a completed schedule` AND the on-disk record has been updated to `status: "completed"` |
| `test_schedule_list_returns_status_field` | create + cancel a few schedules in different states; `list` returns each entry with a `status` field; the `cancelled`/`active` boolean fields are gone |

### Tests to update (small additions only)

- `test_email_schedule_in_schema` (L874) — extend assertion to check `"reactivate"` is in the `schedule.action` enum.

### Tests left untouched

The other ~16 schedule tests (`test_email_schedule_create_basic`, the `_create_*` validation tests, the `_list_*` tests, `test_schedule_sends_inbox_notification`, etc.) test orthogonal behavior (parameter validation, send mechanics, notification messages) and need no changes.

### Test infrastructure notes

- Time-sensitive tests use the existing sleep-based style (e.g. `interval=1` + `time.sleep(1.5)` as at L1219) for consistency with the rest of the file.
- Reconciliation tests reuse the existing pattern of "create agent → release lock → create new agent at same dir → observe state" (L1213-1220).

---

## Verification Gate

Before claiming the implementation is complete:

1. **Smoke import:**
   ```bash
   cd ~/Documents/Github/lingtai-kernel && python -c "import lingtai.capabilities.email"
   ```
2. **Full test run on the touched file:**
   ```bash
   cd ~/Documents/Github/lingtai-kernel && pytest tests/test_layers_email.py -v
   ```
3. **Visual diff of `email.py`** to confirm zero `.cancel` references remain.
4. **Visual diff of `i18n/{en,zh,wen}.json`** to confirm all three locales updated.

Per the global CLAUDE.md note: smoke-test imports and run tests, do not rely on diff review alone.

---

## Rollout / Behavior Break

This refactor introduces **one user-visible behavior break**: agents that previously auto-resumed in-flight schedules across restarts will no longer do so. After upgrading and restarting an agent, any running scheduled email will be paused and require an explicit `reactivate` call to resume.

This is intentional — it is the entire point of the refactor — but should be flagged in:

- The kernel changelog / commit message: `BREAKING: scheduled emails now pause on agent restart and require explicit reactivate`
- The 灵台日报 Chinese user-facing changelog (per `reference_daily_ribao.md`): a short note saying "重启 agent 后定期发信不再自动续发，需用 reactivate 重启之"

The changelog entries are follow-up steps, not part of this spec's implementation work.

---

## Out of Scope

- **Cancel/tick race fix.** Known limitation; agent mitigates via list-then-cancel cycle. Documented under "Concurrency."
- **Stale `.cancel` file cleanup.** Old files left in place, harmless.
- **Splitting `email.py` into smaller modules.** This is P0-2 in the structural bug report (`docs/plans/structural-bug-report.md`) and a separate refactor.
- **Fixing the mailbox-name string inconsistency** (P1-1 in the bug report) — same file, but unrelated.
- **Promoting `_list_inbox`/`_load_message`/`_mark_read`/`_save_read_ids`** from white-box internals to a public interface (P1-2) — same file, also unrelated.
- **TUI/portal UI for managing schedule status.** No Go code consumes the schedule list today (verified). Reactivation happens by talking to the agent, which calls the tool. A portal panel is a future enhancement.

---

## Decisions Index

All design decisions made during brainstorming, indexed for traceability:

- **Q1 / Legacy migration:** Records with no `status` field become `"inactive"` on first boot. No `.cancel` file inspection during migration.
- **Q2 / Cancel-all:** Iterates every schedule, sets each `active`-or-missing record to `"inactive"`. Skips records already `inactive` or `completed`.
- **Q3 / Reactivate timing:** Sets `last_sent_at = now`. Next send fires one full interval after reactivation. No catch-up.
- **Q4 / Completion:** Set inside `_scheduler_tick` after the increment. Existing 3-write pattern in the tick is left as-is.
- **Q5 / Tests:** Rewrite obsolete sentinel-asserting tests to behavior-level. Add new tests for reactivate semantics and legacy migration.
- **No `cancelled` state.** Cancel = inactive. Cancel and startup reconciliation are the same operation.
- **D1:** Reconciliation runs **before** `start_scheduler()` in `setup()`.
- **D3:** `_schedule_list` drops the `cancelled` and `active` boolean fields. Verified that no Go code consumes them.
- **D4:** `_schedule_reactivate` does its own combined `status` + `last_sent_at` write, does not call `_set_schedule_status`.
- **D5:** Stale `.cancel` files left on disk are not cleaned up.
- **D6:** Verification runs the full `test_layers_email.py`, not just the schedule subset.
- **D7:** `_schedule_reactivate` self-heals records where `sent >= count` but status is not yet `completed` (the crash-mid-completion case). The record is updated to `completed` on disk and the reactivate request is refused.
