# Email Scheduler Status — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the email scheduler's `.cancel` sentinel-file mechanism with an explicit `status` field on each schedule record, add a `reactivate` action, and force all in-flight schedules to pause on agent restart.

**Architecture:** Add a `status: "active" | "inactive" | "completed"` field to `schedule.json`. The scheduler tick gates on status. A new `_reconcile_schedules_on_startup()` flips every non-completed record to `inactive` at agent boot, before the scheduler thread starts. Cancel and startup reconciliation become the same operation. There is no `cancelled` state.

**Tech Stack:** Python 3.11+, pytest, atomic file writes via temp-and-rename.

**Spec:** `docs/superpowers/specs/2026-04-06-scheduler-status-design.md`

**Target repo:** `~/Documents/Github/lingtai-kernel` (sibling to this repo). All Python edits live there.

---

## File Inventory

| File | Action | Purpose |
|---|---|---|
| `lingtai-kernel/src/lingtai/capabilities/email.py` | Modify | All scheduler logic changes |
| `lingtai-kernel/src/lingtai/i18n/en.json` | Modify | Update `email.schedule_action` and `email.schedule_id` descriptions |
| `lingtai-kernel/src/lingtai/i18n/zh.json` | Modify | Same, zh translation |
| `lingtai-kernel/src/lingtai/i18n/wen.json` | Modify | Same, classical-Chinese translation |
| `lingtai-kernel/tests/test_layers_email.py` | Modify | Rewrite ~6 sentinel-file tests, add ~8 new tests |

No new files.

---

## Task Order

The plan is TDD-first: each task writes failing tests, then the implementation, then verifies the tests pass, then commits. Tasks are sequenced so each commit leaves the codebase in a buildable state.

1. **Task 1 — Schema and dispatch wiring** (lays groundwork — adds `reactivate` to enum and `_handle_schedule` branch, no behavior yet)
2. **Task 2 — `_set_schedule_status` helper + `_schedule_create` writes status**
3. **Task 3 — `_schedule_reactivate` (with self-heal)**
4. **Task 4 — `_reconcile_schedules_on_startup` + setup() wiring**
5. **Task 5 — `_scheduler_tick` status guard + completion write + remove `.cancel` checks**
6. **Task 6 — `_schedule_cancel` rewrite (no `.cancel` files)**
7. **Task 7 — `_schedule_list` returns `status` field, drops boolean flags**
8. **Task 8 — i18n updates (en/zh/wen)**
9. **Task 9 — Final verification: full test file pass + smoke import**

---

## Task 1: Schema and dispatch wiring

**Files:**
- Modify: `lingtai-kernel/src/lingtai/capabilities/email.py:127` (schema enum)
- Modify: `lingtai-kernel/src/lingtai/capabilities/email.py:350` (dispatch)
- Test: `lingtai-kernel/tests/test_layers_email.py:874` (extend existing schema test) and add new dispatch test

This task adds the `reactivate` action to the schema enum and the `_handle_schedule` dispatch table, but does NOT yet implement `_schedule_reactivate`. The dispatch will route to a stub that returns a placeholder error. This lets us land the schema change atomically and verify the routing in isolation.

- [ ] **Step 1: Read the current schema test to understand the assertion style**

Run: `pytest lingtai-kernel/tests/test_layers_email.py::test_email_schedule_in_schema -v`
Expected: PASS (it's testing the current 3-action enum)

- [ ] **Step 2: Update `test_email_schedule_in_schema` to require `reactivate` in the enum**

Edit `lingtai-kernel/tests/test_layers_email.py` around line 874. Find the test and update the assertion to additionally check that `"reactivate"` is in the schedule action enum.

```python
def test_email_schedule_in_schema(tmp_path):
    """schedule object should be in the email schema."""
    from lingtai.capabilities.email import get_schema
    schema = get_schema("en")
    assert "schedule" in schema["properties"]
    sched_props = schema["properties"]["schedule"]["properties"]
    assert "action" in sched_props
    actions = sched_props["action"]["enum"]
    assert "create" in actions
    assert "cancel" in actions
    assert "list" in actions
    assert "reactivate" in actions  # NEW
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `cd ~/Documents/Github/lingtai-kernel && pytest tests/test_layers_email.py::test_email_schedule_in_schema -v`
Expected: FAIL with `AssertionError: assert 'reactivate' in ['create', 'cancel', 'list']`

- [ ] **Step 4: Add `reactivate` to the schema enum in email.py**

Edit `lingtai-kernel/src/lingtai/capabilities/email.py` around line 127. Find the schedule action enum and add `"reactivate"`:

```python
"action": {
    "type": "string",
    "enum": ["create", "cancel", "list", "reactivate"],
    "description": t(lang, "email.schedule_action"),
},
```

- [ ] **Step 5: Run the schema test to verify it passes**

Run: `cd ~/Documents/Github/lingtai-kernel && pytest tests/test_layers_email.py::test_email_schedule_in_schema -v`
Expected: PASS

- [ ] **Step 6: Write a failing test for the dispatch routing**

Add this test to `lingtai-kernel/tests/test_layers_email.py` near the other schedule tests (e.g., after `test_email_schedule_unknown_action` around line 893):

```python
def test_email_schedule_reactivate_routes_to_handler(tmp_path):
    """reactivate action should be dispatched (not return 'Unknown schedule action')."""
    agent = Agent(service=make_mock_service(), agent_name="test", working_dir=tmp_path / "test",
                       capabilities=["email"])
    mgr = agent.get_capability("email")
    result = mgr.handle({"schedule": {"action": "reactivate", "schedule_id": "nonexistent"}})
    # Should NOT return the dispatch fallback error
    assert "error" in result
    assert "Unknown schedule action" not in result["error"]
    # Should route to reactivate handler, which errors on missing record
    assert "Schedule not found" in result["error"]
```

- [ ] **Step 7: Run the new dispatch test to verify it fails**

Run: `cd ~/Documents/Github/lingtai-kernel && pytest tests/test_layers_email.py::test_email_schedule_reactivate_routes_to_handler -v`
Expected: FAIL with `assert 'Unknown schedule action' not in 'Unknown schedule action: reactivate'`

- [ ] **Step 8: Add the dispatch branch in `_handle_schedule`**

Edit `lingtai-kernel/src/lingtai/capabilities/email.py` around line 350. Find `_handle_schedule` and add a branch for `reactivate` BEFORE the unknown-action fallback:

```python
def _handle_schedule(self, args: dict, schedule: dict) -> dict:
    action = schedule.get("action")
    if action == "create":
        return self._schedule_create(args, schedule)
    elif action == "cancel":
        return self._schedule_cancel(schedule)
    elif action == "list":
        return self._schedule_list()
    elif action == "reactivate":
        return self._schedule_reactivate(schedule)
    else:
        return {"error": f"Unknown schedule action: {action}"}
```

- [ ] **Step 9: Add a temporary stub `_schedule_reactivate` so the file imports cleanly**

Edit `lingtai-kernel/src/lingtai/capabilities/email.py`. Add this stub method to the `EmailManager` class, near the other schedule methods (e.g., right after `_schedule_cancel`):

```python
def _schedule_reactivate(self, schedule: dict) -> dict:
    """STUB — full implementation in Task 3."""
    schedule_id = schedule.get("schedule_id")
    if not schedule_id:
        return {"error": "schedule_id is required for reactivate"}
    record = self._read_schedule(schedule_id)
    if record is None:
        return {"error": f"Schedule not found: {schedule_id}"}
    return {"error": "Reactivate not yet implemented"}
```

This stub is intentional — it returns the right "not found" error for the dispatch test, and Task 3 will replace its body with the real implementation.

- [ ] **Step 10: Run both new tests to verify they pass**

Run: `cd ~/Documents/Github/lingtai-kernel && pytest tests/test_layers_email.py::test_email_schedule_in_schema tests/test_layers_email.py::test_email_schedule_reactivate_routes_to_handler -v`
Expected: Both PASS

- [ ] **Step 11: Smoke import to catch any unrelated breakage**

Run: `cd ~/Documents/Github/lingtai-kernel && python -c "import lingtai.capabilities.email; print('ok')"`
Expected: prints `ok`, no traceback

- [ ] **Step 12: Commit**

```bash
cd ~/Documents/Github/lingtai-kernel
git add src/lingtai/capabilities/email.py tests/test_layers_email.py
git commit -m "feat(email): add reactivate action to schedule schema and dispatch (stub)"
```

---

## Task 2: `_set_schedule_status` helper and `_schedule_create` writes status

**Files:**
- Modify: `lingtai-kernel/src/lingtai/capabilities/email.py` (add helper around line 500, modify `_schedule_create` around line 397)
- Test: `lingtai-kernel/tests/test_layers_email.py`

This task adds the `_set_schedule_status` helper used by cancel/reconciliation, and makes `_schedule_create` write `"status": "active"` into new records. Both changes are observable via the on-disk record contents.

- [ ] **Step 1: Write a failing test for new schedule records having status="active"**

Add this test to `lingtai-kernel/tests/test_layers_email.py` near the other create tests (e.g., after `test_email_schedule_create_basic` around line 906):

```python
def test_email_schedule_create_writes_status_active(tmp_path):
    """Newly created schedules should have status='active' on disk."""
    agent = Agent(service=make_mock_service(), agent_name="test", working_dir=tmp_path / "test",
                       capabilities=["email"])
    mail_svc = MagicMock()
    mail_svc.address = "me"
    mail_svc.send.return_value = None
    agent._mail_service = mail_svc
    mgr = agent.get_capability("email")
    result = mgr.handle({
        "address": "someone",
        "message": "x",
        "schedule": {"action": "create", "interval": 60, "count": 5},
    })
    sid = result["schedule_id"]
    sched = json.loads(
        (agent.working_dir / "mailbox" / "schedules" / sid / "schedule.json").read_text()
    )
    assert sched["status"] == "active"
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd ~/Documents/Github/lingtai-kernel && pytest tests/test_layers_email.py::test_email_schedule_create_writes_status_active -v`
Expected: FAIL with `KeyError: 'status'`

- [ ] **Step 3: Add `"status": "active"` to the record dict in `_schedule_create`**

Edit `lingtai-kernel/src/lingtai/capabilities/email.py` around line 390. Find `_schedule_create` and add the field to the record dict:

```python
record = {
    "schedule_id": schedule_id,
    "send_payload": send_payload,
    "interval": interval,
    "count": count,
    "sent": 0,
    "created_at": now,
    "last_sent_at": None,
    "status": "active",
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd ~/Documents/Github/lingtai-kernel && pytest tests/test_layers_email.py::test_email_schedule_create_writes_status_active -v`
Expected: PASS

- [ ] **Step 5: Write a failing test for `_set_schedule_status` helper**

Add this test to `lingtai-kernel/tests/test_layers_email.py` near the other schedule tests:

```python
def test_set_schedule_status_helper_updates_record(tmp_path):
    """_set_schedule_status should update the on-disk status field."""
    agent = Agent(service=make_mock_service(), agent_name="test", working_dir=tmp_path / "test",
                       capabilities=["email"])
    mail_svc = MagicMock()
    mail_svc.address = "me"
    mail_svc.send.return_value = None
    agent._mail_service = mail_svc
    mgr = agent.get_capability("email")
    result = mgr.handle({
        "address": "someone", "message": "x",
        "schedule": {"action": "create", "interval": 60, "count": 5},
    })
    sid = result["schedule_id"]
    # Use the helper directly
    ok = mgr._set_schedule_status(sid, "inactive")
    assert ok is True
    sched = json.loads(
        (agent.working_dir / "mailbox" / "schedules" / sid / "schedule.json").read_text()
    )
    assert sched["status"] == "inactive"
    # Returns False on missing record
    assert mgr._set_schedule_status("nonexistent", "inactive") is False
```

- [ ] **Step 6: Run the test to verify it fails**

Run: `cd ~/Documents/Github/lingtai-kernel && pytest tests/test_layers_email.py::test_set_schedule_status_helper_updates_record -v`
Expected: FAIL with `AttributeError: 'EmailManager' object has no attribute '_set_schedule_status'`

- [ ] **Step 7: Add the `_set_schedule_status` helper**

Edit `lingtai-kernel/src/lingtai/capabilities/email.py`. Add this method to the `EmailManager` class right after `_read_schedule` (around line 500):

```python
def _set_schedule_status(self, schedule_id: str, status: str) -> bool:
    """Update the status of a schedule record on disk. Returns True on success, False if missing."""
    record = self._read_schedule(schedule_id)
    if record is None:
        return False
    record["status"] = status
    sched_file = self._schedules_dir / schedule_id / "schedule.json"
    self._write_schedule(sched_file, record)
    return True
```

- [ ] **Step 8: Run the test to verify it passes**

Run: `cd ~/Documents/Github/lingtai-kernel && pytest tests/test_layers_email.py::test_set_schedule_status_helper_updates_record -v`
Expected: PASS

- [ ] **Step 9: Smoke import**

Run: `cd ~/Documents/Github/lingtai-kernel && python -c "import lingtai.capabilities.email; print('ok')"`
Expected: `ok`

- [ ] **Step 10: Commit**

```bash
cd ~/Documents/Github/lingtai-kernel
git add src/lingtai/capabilities/email.py tests/test_layers_email.py
git commit -m "feat(email): add status field to schedule records and _set_schedule_status helper"
```

---

## Task 3: `_schedule_reactivate` (with self-heal)

**Files:**
- Modify: `lingtai-kernel/src/lingtai/capabilities/email.py` (replace stub from Task 1)
- Test: `lingtai-kernel/tests/test_layers_email.py`

This task implements the full reactivate logic: guard rules for missing/completed/active records, the `last_sent_at = now` reset, and the self-heal for crashed-mid-completion records.

- [ ] **Step 1: Write a failing test for reactivate on inactive (happy path)**

Add this test to `lingtai-kernel/tests/test_layers_email.py`:

```python
def test_schedule_reactivate_inactive_resumes(tmp_path):
    """reactivate on an inactive schedule should flip status and reset last_sent_at."""
    agent = Agent(service=make_mock_service(), agent_name="test", working_dir=tmp_path / "test",
                       capabilities=["email"])
    mail_svc = MagicMock()
    mail_svc.address = "me"
    mail_svc.send.return_value = None
    agent._mail_service = mail_svc
    mgr = agent.get_capability("email")

    # Manually create an inactive schedule
    sched_id = "reactivate1234"
    sched_dir = agent.working_dir / "mailbox" / "schedules" / sched_id
    sched_dir.mkdir(parents=True, exist_ok=True)
    record = {
        "schedule_id": sched_id,
        "send_payload": {"address": "someone", "subject": "", "message": "x", "cc": [], "bcc": [], "type": "normal"},
        "interval": 60,
        "count": 5,
        "sent": 1,
        "created_at": "2026-04-06T10:00:00Z",
        "last_sent_at": "2026-04-06T10:00:00Z",
        "status": "inactive",
    }
    (sched_dir / "schedule.json").write_text(json.dumps(record))

    result = mgr.handle({"schedule": {"action": "reactivate", "schedule_id": sched_id}})
    assert result["status"] == "reactivated"
    assert result["schedule_id"] == sched_id

    sched = json.loads((sched_dir / "schedule.json").read_text())
    assert sched["status"] == "active"
    # last_sent_at should be ~now (not the old "10:00:00Z")
    assert sched["last_sent_at"] != "2026-04-06T10:00:00Z"
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd ~/Documents/Github/lingtai-kernel && pytest tests/test_layers_email.py::test_schedule_reactivate_inactive_resumes -v`
Expected: FAIL with `assert 'Reactivate not yet implemented' ...` or similar (the stub from Task 1)

- [ ] **Step 3: Write a failing test for reactivate on already-active (noop)**

```python
def test_schedule_reactivate_active_is_noop(tmp_path):
    """reactivate on an active schedule should return already_active without mutation."""
    agent = Agent(service=make_mock_service(), agent_name="test", working_dir=tmp_path / "test",
                       capabilities=["email"])
    mail_svc = MagicMock()
    mail_svc.address = "me"
    mail_svc.send.return_value = None
    agent._mail_service = mail_svc
    mgr = agent.get_capability("email")

    create_result = mgr.handle({
        "address": "someone", "message": "x",
        "schedule": {"action": "create", "interval": 60, "count": 5},
    })
    sid = create_result["schedule_id"]
    original = json.loads(
        (agent.working_dir / "mailbox" / "schedules" / sid / "schedule.json").read_text()
    )

    result = mgr.handle({"schedule": {"action": "reactivate", "schedule_id": sid}})
    assert result["status"] == "already_active"
    assert result["schedule_id"] == sid

    after = json.loads(
        (agent.working_dir / "mailbox" / "schedules" / sid / "schedule.json").read_text()
    )
    # last_sent_at should be unchanged
    assert after["last_sent_at"] == original["last_sent_at"]
```

- [ ] **Step 4: Write a failing test for reactivate on completed**

```python
def test_schedule_reactivate_completed_errors(tmp_path):
    """reactivate on a completed schedule should error."""
    agent = Agent(service=make_mock_service(), agent_name="test", working_dir=tmp_path / "test",
                       capabilities=["email"])
    mgr = agent.get_capability("email")

    sched_id = "completed1234"
    sched_dir = agent.working_dir / "mailbox" / "schedules" / sched_id
    sched_dir.mkdir(parents=True, exist_ok=True)
    record = {
        "schedule_id": sched_id,
        "send_payload": {"address": "x", "subject": "", "message": "y", "cc": [], "bcc": [], "type": "normal"},
        "interval": 60, "count": 3, "sent": 3,
        "created_at": "2026-04-06T10:00:00Z",
        "last_sent_at": "2026-04-06T10:02:00Z",
        "status": "completed",
    }
    (sched_dir / "schedule.json").write_text(json.dumps(record))

    result = mgr.handle({"schedule": {"action": "reactivate", "schedule_id": sched_id}})
    assert "error" in result
    assert "completed" in result["error"].lower()
```

- [ ] **Step 5: Write a failing test for reactivate on missing record**

```python
def test_schedule_reactivate_not_found_errors(tmp_path):
    """reactivate on a missing schedule should return Schedule not found."""
    agent = Agent(service=make_mock_service(), agent_name="test", working_dir=tmp_path / "test",
                       capabilities=["email"])
    mgr = agent.get_capability("email")
    result = mgr.handle({"schedule": {"action": "reactivate", "schedule_id": "nonexistent"}})
    assert "error" in result
    assert "Schedule not found" in result["error"]
```

- [ ] **Step 6: Write a failing test for the self-heal case**

```python
def test_schedule_reactivate_self_heals_crash_mid_completion(tmp_path):
    """If sent>=count but status==inactive (crash mid-completion), reactivate should self-heal to completed and refuse."""
    agent = Agent(service=make_mock_service(), agent_name="test", working_dir=tmp_path / "test",
                       capabilities=["email"])
    mgr = agent.get_capability("email")

    sched_id = "crashed12345"
    sched_dir = agent.working_dir / "mailbox" / "schedules" / sched_id
    sched_dir.mkdir(parents=True, exist_ok=True)
    record = {
        "schedule_id": sched_id,
        "send_payload": {"address": "x", "subject": "", "message": "y", "cc": [], "bcc": [], "type": "normal"},
        "interval": 60, "count": 3, "sent": 3,  # sent==count
        "created_at": "2026-04-06T10:00:00Z",
        "last_sent_at": "2026-04-06T10:02:00Z",
        "status": "inactive",  # but status was never updated to completed
    }
    (sched_dir / "schedule.json").write_text(json.dumps(record))

    result = mgr.handle({"schedule": {"action": "reactivate", "schedule_id": sched_id}})
    assert "error" in result
    assert "completed" in result["error"].lower()

    # The on-disk record should now be self-healed to completed
    sched = json.loads((sched_dir / "schedule.json").read_text())
    assert sched["status"] == "completed"
```

- [ ] **Step 7: Run all four reactivate tests to confirm they fail**

Run:
```bash
cd ~/Documents/Github/lingtai-kernel && pytest tests/test_layers_email.py -v -k "schedule_reactivate"
```
Expected: All 4 new tests FAIL (the stub returns "not yet implemented" or similar).

- [ ] **Step 8: Replace the `_schedule_reactivate` stub with the full implementation**

Edit `lingtai-kernel/src/lingtai/capabilities/email.py`. Find the stub `_schedule_reactivate` from Task 1 and replace it with:

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
    # Self-heal: crashed-mid-completion records have sent>=count but status!=completed
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

- [ ] **Step 9: Run all four reactivate tests to confirm they pass**

Run:
```bash
cd ~/Documents/Github/lingtai-kernel && pytest tests/test_layers_email.py -v -k "schedule_reactivate"
```
Expected: All 4 PASS.

- [ ] **Step 10: Smoke import**

Run: `cd ~/Documents/Github/lingtai-kernel && python -c "import lingtai.capabilities.email; print('ok')"`
Expected: `ok`

- [ ] **Step 11: Commit**

```bash
cd ~/Documents/Github/lingtai-kernel
git add src/lingtai/capabilities/email.py tests/test_layers_email.py
git commit -m "feat(email): implement _schedule_reactivate with self-heal for crash-mid-completion"
```

---

## Task 4: `_reconcile_schedules_on_startup` and `setup()` wiring

**Files:**
- Modify: `lingtai-kernel/src/lingtai/capabilities/email.py` (add method, modify `setup()` around line 1091)
- Test: `lingtai-kernel/tests/test_layers_email.py`

This task adds the startup reconciliation pass and wires it into `setup()` BEFORE the scheduler thread starts. After this task, the user-visible behavior change lands: in-flight schedules pause across restart instead of auto-resuming.

This task ALSO rewrites two existing tests that depended on the old auto-resume behavior:
- `test_email_schedule_recovery_on_setup` (L1179)
- `test_email_schedule_recovery_skips_cancelled` (L1224)

- [ ] **Step 1: Write a failing test for startup reconciliation flipping active → inactive**

Add this test to `lingtai-kernel/tests/test_layers_email.py`:

```python
def test_reconcile_flips_active_to_inactive_on_startup(tmp_path):
    """A new EmailManager should flip all active schedules to inactive on startup."""
    agent1 = Agent(service=make_mock_service(), agent_name="test", working_dir=tmp_path / "test",
                       capabilities=["email"])
    mail_svc = MagicMock()
    mail_svc.address = "me"
    mail_svc.send.return_value = None
    agent1._mail_service = mail_svc

    # Manually write an active schedule.json
    sched_id = "active1234"
    sched_dir = agent1.working_dir / "mailbox" / "schedules" / sched_id
    sched_dir.mkdir(parents=True, exist_ok=True)
    record = {
        "schedule_id": sched_id,
        "send_payload": {"address": "x", "subject": "", "message": "y", "cc": [], "bcc": [], "type": "normal"},
        "interval": 60, "count": 5, "sent": 1,
        "created_at": "2026-04-06T10:00:00Z",
        "last_sent_at": "2026-04-06T10:00:00Z",
        "status": "active",
    }
    (sched_dir / "schedule.json").write_text(json.dumps(record))
    agent1.stop(timeout=1.0)

    # Create a new agent at the same dir — reconciliation should flip to inactive
    agent2 = Agent(service=make_mock_service(), agent_name="test", working_dir=tmp_path / "test",
                       mail_service=mail_svc, capabilities=["email"])

    sched = json.loads((sched_dir / "schedule.json").read_text())
    assert sched["status"] == "inactive"
```

- [ ] **Step 2: Write a failing test for legacy records (no status field) becoming inactive**

```python
def test_reconcile_flips_legacy_record_to_inactive(tmp_path):
    """A schedule.json with NO status field should be flipped to inactive on startup."""
    agent1 = Agent(service=make_mock_service(), agent_name="test", working_dir=tmp_path / "test",
                       capabilities=["email"])
    mail_svc = MagicMock()
    mail_svc.address = "me"

    sched_id = "legacy12345"
    sched_dir = agent1.working_dir / "mailbox" / "schedules" / sched_id
    sched_dir.mkdir(parents=True, exist_ok=True)
    record = {  # NO status field
        "schedule_id": sched_id,
        "send_payload": {"address": "x", "subject": "", "message": "y", "cc": [], "bcc": [], "type": "normal"},
        "interval": 60, "count": 5, "sent": 1,
        "created_at": "2026-04-06T10:00:00Z",
        "last_sent_at": "2026-04-06T10:00:00Z",
    }
    (sched_dir / "schedule.json").write_text(json.dumps(record))
    agent1.stop(timeout=1.0)

    agent2 = Agent(service=make_mock_service(), agent_name="test", working_dir=tmp_path / "test",
                       mail_service=mail_svc, capabilities=["email"])

    sched = json.loads((sched_dir / "schedule.json").read_text())
    assert sched["status"] == "inactive"
```

- [ ] **Step 3: Write a failing test that completed records are NOT touched**

```python
def test_reconcile_leaves_completed_records_alone(tmp_path):
    """Completed schedules should NOT be flipped — they stay completed."""
    agent1 = Agent(service=make_mock_service(), agent_name="test", working_dir=tmp_path / "test",
                       capabilities=["email"])
    mail_svc = MagicMock()
    mail_svc.address = "me"

    sched_id = "completed5678"
    sched_dir = agent1.working_dir / "mailbox" / "schedules" / sched_id
    sched_dir.mkdir(parents=True, exist_ok=True)
    record = {
        "schedule_id": sched_id,
        "send_payload": {"address": "x", "subject": "", "message": "y", "cc": [], "bcc": [], "type": "normal"},
        "interval": 60, "count": 3, "sent": 3,
        "created_at": "2026-04-06T10:00:00Z",
        "last_sent_at": "2026-04-06T10:02:00Z",
        "status": "completed",
    }
    (sched_dir / "schedule.json").write_text(json.dumps(record))
    agent1.stop(timeout=1.0)

    agent2 = Agent(service=make_mock_service(), agent_name="test", working_dir=tmp_path / "test",
                       mail_service=mail_svc, capabilities=["email"])

    sched = json.loads((sched_dir / "schedule.json").read_text())
    assert sched["status"] == "completed"
```

- [ ] **Step 4: Run all 3 new reconcile tests to confirm they fail**

Run:
```bash
cd ~/Documents/Github/lingtai-kernel && pytest tests/test_layers_email.py -v -k "test_reconcile"
```
Expected: All 3 FAIL (the active record stays active because reconciliation doesn't exist; the legacy record stays statusless; the completed test may pass spuriously because nothing modifies it — but it's a sanity check for after the change).

- [ ] **Step 5: Add the `_reconcile_schedules_on_startup` method**

Edit `lingtai-kernel/src/lingtai/capabilities/email.py`. Add this method to the `EmailManager` class, near the other schedule helpers (e.g., right after `_set_schedule_status`):

```python
def _reconcile_schedules_on_startup(self) -> None:
    """Flip every non-completed schedule to inactive on agent startup.

    Forces deliberate reactivation after restart — no schedule auto-resumes.
    Called from setup() BEFORE start_scheduler().
    """
    schedules_dir = self._schedules_dir
    if not schedules_dir.is_dir():
        return
    for sched_dir in schedules_dir.iterdir():
        if not sched_dir.is_dir():
            continue
        sched_file = sched_dir / "schedule.json"
        if not sched_file.is_file():
            continue
        try:
            record = json.loads(sched_file.read_text())
        except (json.JSONDecodeError, OSError):
            continue
        status = record.get("status", "active")
        if status == "completed":
            continue
        if status == "inactive":
            continue  # already in safe state, no spurious write
        # active, missing, or unknown — flip to inactive
        record["status"] = "inactive"
        try:
            self._write_schedule(sched_file, record)
        except OSError:
            continue
```

- [ ] **Step 6: Wire reconciliation into `setup()` BEFORE `start_scheduler()`**

Edit `lingtai-kernel/src/lingtai/capabilities/email.py` around line 1091. Find `setup()` and insert the reconciliation call before `start_scheduler()`:

```python
def setup(agent: "BaseAgent", *, private_mode: bool = False) -> EmailManager:
    """Set up email capability — filesystem-based mailbox."""
    lang = agent._config.language
    mgr = EmailManager(agent, private_mode=private_mode)
    agent.override_intrinsic("mail")
    agent._mailbox_name = "email box"
    agent._mailbox_tool = "email"
    agent.add_tool(
        "email", schema=get_schema(lang), handler=mgr.handle, description=get_description(lang),
    )
    mgr._reconcile_schedules_on_startup()  # NEW: must come before start_scheduler
    mgr.start_scheduler()
    return mgr
```

- [ ] **Step 7: Run all 3 reconcile tests to confirm they pass**

Run:
```bash
cd ~/Documents/Github/lingtai-kernel && pytest tests/test_layers_email.py -v -k "test_reconcile"
```
Expected: All 3 PASS.

- [ ] **Step 8: Rewrite the existing `test_email_schedule_recovery_on_setup`**

This test currently asserts that interrupted schedules auto-resume. The new behavior is the opposite — they pause and require explicit reactivation. Find the test at `lingtai-kernel/tests/test_layers_email.py:1179` and replace its body:

```python
def test_email_schedule_recovery_on_setup(tmp_path):
    """After agent restart, in-flight schedules should pause (status=inactive), not auto-resume."""
    agent1 = Agent(service=make_mock_service(), agent_name="test", working_dir=tmp_path / "test",
                        capabilities=["email"])
    mail_svc = MagicMock()
    mail_svc.address = "me"
    mail_svc.send.return_value = None
    agent1._mail_service = mail_svc

    # Manually write a schedule.json that looks like it was interrupted at sent=1 of count=3
    sched_id = "recover12345"
    sched_dir = agent1.working_dir / "mailbox" / "schedules" / sched_id
    sched_dir.mkdir(parents=True, exist_ok=True)
    record = {
        "schedule_id": sched_id,
        "send_payload": {
            "address": "someone",
            "subject": "Resume",
            "message": "continued",
            "cc": [],
            "bcc": [],
            "type": "normal",
        },
        "interval": 1,
        "count": 3,
        "sent": 1,
        "created_at": "2026-03-18T10:00:00Z",
        "last_sent_at": "2026-03-18T10:00:00Z",
        "status": "active",
    }
    (sched_dir / "schedule.json").write_text(json.dumps(record, indent=2))

    agent1.stop(timeout=1.0)

    # Create a NEW agent at the same base_dir — reconciliation flips to inactive
    agent2 = Agent(service=make_mock_service(), agent_name="test", working_dir=tmp_path / "test",
                        mail_service=mail_svc, capabilities=["email"])
    mgr2 = agent2.get_capability("email")

    # Wait — sends should NOT happen
    time.sleep(2.5)
    sched = json.loads((sched_dir / "schedule.json").read_text())
    assert sched["sent"] == 1, "schedule should not have auto-resumed"
    assert sched["status"] == "inactive"

    # Now reactivate — sends should resume after one full interval
    result = mgr2.handle({"schedule": {"action": "reactivate", "schedule_id": sched_id}})
    assert result["status"] == "reactivated"

    # Wait for the remaining 2 sends (interval=1, so ~2 more seconds)
    time.sleep(3.0)
    final = json.loads((sched_dir / "schedule.json").read_text())
    assert final["sent"] == 3
```

- [ ] **Step 9: Rewrite `test_email_schedule_recovery_skips_cancelled`**

Find the test at `lingtai-kernel/tests/test_layers_email.py:1224` and replace it. The new behavior: a record with `status="inactive"` should NOT auto-resume (and should NOT auto-flip back to active either).

```python
def test_email_schedule_recovery_skips_inactive(tmp_path):
    """Inactive schedules should not be resumed and should not be flipped back to active."""
    mail_svc = MagicMock()
    mail_svc.address = "me"
    mail_svc.send.return_value = None

    agent1 = Agent(service=make_mock_service(), agent_name="test", working_dir=tmp_path / "test",
                       capabilities=["email"])

    sched_id = "inactive12345"
    sched_dir = agent1.working_dir / "mailbox" / "schedules" / sched_id
    sched_dir.mkdir(parents=True, exist_ok=True)
    record = {
        "schedule_id": sched_id,
        "send_payload": {"address": "someone", "message": "x", "subject": "", "cc": [], "bcc": [], "type": "normal"},
        "interval": 1, "count": 5, "sent": 2,
        "created_at": "2026-03-18T10:00:00Z",
        "last_sent_at": "2026-03-18T10:00:00Z",
        "status": "inactive",
    }
    (sched_dir / "schedule.json").write_text(json.dumps(record, indent=2))
    agent1.stop(timeout=1.0)

    agent2 = Agent(service=make_mock_service(), agent_name="test", working_dir=tmp_path / "test",
                       mail_service=mail_svc, capabilities=["email"])

    time.sleep(2.0)

    final = json.loads((sched_dir / "schedule.json").read_text())
    assert final["sent"] == 2, "inactive schedule should not have resumed"
    assert final["status"] == "inactive", "inactive should not be flipped back to active"
```

- [ ] **Step 10: Run the rewritten tests to confirm they pass**

Run:
```bash
cd ~/Documents/Github/lingtai-kernel && pytest tests/test_layers_email.py::test_email_schedule_recovery_on_setup tests/test_layers_email.py::test_email_schedule_recovery_skips_inactive -v
```
Expected: Both PASS.

- [ ] **Step 11: Run the full schedule test set to catch other tests broken by reconciliation**

Run:
```bash
cd ~/Documents/Github/lingtai-kernel && pytest tests/test_layers_email.py -v -k "schedule"
```
Expected: Most PASS. Some failures are expected from tests that still depend on `.cancel` files (those will be fixed in Tasks 5 and 6). Note any unexpected failures.

- [ ] **Step 12: Smoke import**

Run: `cd ~/Documents/Github/lingtai-kernel && python -c "import lingtai.capabilities.email; print('ok')"`
Expected: `ok`

- [ ] **Step 13: Commit**

```bash
cd ~/Documents/Github/lingtai-kernel
git add src/lingtai/capabilities/email.py tests/test_layers_email.py
git commit -m "feat(email): startup reconciliation pauses all in-flight schedules on restart

BREAKING: Scheduled emails no longer auto-resume after agent restart. Users
must explicitly call schedule.reactivate to resume a paused schedule."
```

---

## Task 5: `_scheduler_tick` status guard, completion write, remove `.cancel` checks

**Files:**
- Modify: `lingtai-kernel/src/lingtai/capabilities/email.py:510-619` (the `_scheduler_tick` method)
- Test: `lingtai-kernel/tests/test_layers_email.py`

This task makes `_scheduler_tick` honor the `status` field, set status to `completed` on the final send, and stop checking for `.cancel` sentinel files. After this task, the scheduler is fully driven by the explicit status field.

> **Note (added after Task 4 ran):** The status guard portion of this task was already pulled forward to Task 4 (commit `ff64b4e`) because the rewritten recovery test could not pass without it. The 4-line `if record.get("status", "active") != "active": continue` check is already present in `_scheduler_tick`. Task 5 now only needs to:
> 1. Add the `record["status"] = "completed"` mutation when `seq >= count` (the completion write).
> 2. Remove the two `.cancel` file checks (agent-level and per-schedule).
> 3. Verify the test for `test_scheduler_tick_skips_inactive_records` (added in Step 2) still passes — it should already pass because the guard is in place.
>
> The test in Step 1 (`test_schedule_completion_sets_status_completed`) will still need the new completion-write logic to pass.

- [ ] **Step 1: Write a failing test for completion setting status="completed"**

Add this test to `lingtai-kernel/tests/test_layers_email.py`:

```python
def test_schedule_completion_sets_status_completed(tmp_path):
    """When sent reaches count, the record's status should become 'completed'."""
    agent = Agent(service=make_mock_service(), agent_name="test", working_dir=tmp_path / "test",
                       capabilities=["email"])
    mail_svc = MagicMock()
    mail_svc.address = "me"
    mail_svc.send.return_value = None
    agent._mail_service = mail_svc
    mgr = agent.get_capability("email")

    result = mgr.handle({
        "address": "someone", "subject": "done", "message": "bye",
        "schedule": {"action": "create", "interval": 1, "count": 2},
    })
    sid = result["schedule_id"]
    time.sleep(3.0)  # both sends should complete

    sched = json.loads(
        (agent.working_dir / "mailbox" / "schedules" / sid / "schedule.json").read_text()
    )
    assert sched["sent"] == 2
    assert sched["status"] == "completed"
```

- [ ] **Step 2: Write a failing test that the status guard skips inactive records in the tick**

```python
def test_scheduler_tick_skips_inactive_records(tmp_path):
    """The scheduler tick should NOT send for records with status='inactive'."""
    agent = Agent(service=make_mock_service(), agent_name="test", working_dir=tmp_path / "test",
                       capabilities=["email"])
    mail_svc = MagicMock()
    mail_svc.address = "me"
    mail_svc.send.return_value = None
    agent._mail_service = mail_svc
    mgr = agent.get_capability("email")

    # Create a schedule with status=inactive directly on disk (skipping reconciliation)
    sched_id = "inactivetick1"
    sched_dir = agent.working_dir / "mailbox" / "schedules" / sched_id
    sched_dir.mkdir(parents=True, exist_ok=True)
    record = {
        "schedule_id": sched_id,
        "send_payload": {"address": "x", "subject": "", "message": "y", "cc": [], "bcc": [], "type": "normal"},
        "interval": 1, "count": 5, "sent": 0,
        "created_at": "2026-04-06T10:00:00Z",
        "last_sent_at": None,  # would be due immediately if active
        "status": "inactive",
    }
    (sched_dir / "schedule.json").write_text(json.dumps(record))

    time.sleep(2.0)

    sched = json.loads((sched_dir / "schedule.json").read_text())
    assert sched["sent"] == 0, "scheduler should not tick inactive records"
```

- [ ] **Step 3: Run both new tests to confirm they fail**

Run:
```bash
cd ~/Documents/Github/lingtai-kernel && pytest tests/test_layers_email.py::test_schedule_completion_sets_status_completed tests/test_layers_email.py::test_scheduler_tick_skips_inactive_records -v
```
Expected:
- `test_schedule_completion_sets_status_completed` FAIL: `KeyError: 'status'` (or `assert ... == 'completed'` with whatever's actually there)
- `test_scheduler_tick_skips_inactive_records` FAIL: assertion fails because the tick currently doesn't honor the status field, so sends will fire and `sched["sent"]` will be > 0.

- [ ] **Step 4: Update `_scheduler_tick` — add status guard, remove `.cancel` checks, set completion**

Edit `lingtai-kernel/src/lingtai/capabilities/email.py` starting around line 510. Replace the `_scheduler_tick` method body. The full new method:

```python
def _scheduler_tick(self) -> None:
    """One scan of all schedule folders."""
    schedules_dir = self._schedules_dir
    if not schedules_dir.is_dir():
        return

    now = datetime.now(timezone.utc)

    for sched_dir in schedules_dir.iterdir():
        if not sched_dir.is_dir():
            continue

        sched_file = sched_dir / "schedule.json"
        if not sched_file.is_file():
            continue

        try:
            record = json.loads(sched_file.read_text())
        except (json.JSONDecodeError, OSError):
            continue

        # Status guard — only active records get ticked.
        # Default to "active" for legacy records that escaped reconciliation (defense in depth).
        if record.get("status", "active") != "active":
            continue

        sent = record.get("sent", 0)
        count = record.get("count", 0)
        # Defense-in-depth: at-most-once safety for crash-mid-completion
        if sent >= count:
            continue

        # Check if due
        last_sent_at = record.get("last_sent_at")
        if last_sent_at is not None:
            try:
                last_dt = datetime.strptime(last_sent_at, "%Y-%m-%dT%H:%M:%SZ").replace(
                    tzinfo=timezone.utc
                )
            except ValueError:
                continue
            interval = record.get("interval", 0)
            due_at = last_dt + timedelta(seconds=interval)
            if now < due_at:
                continue

        # Due — at-most-once: increment before send
        seq = sent + 1
        record["sent"] = seq
        self._write_schedule(sched_file, record)

        # Build send args with _schedule metadata
        send_payload = record.get("send_payload", {})
        remaining = count - seq
        interval = record.get("interval", 0)
        estimated_finish = (now + timedelta(seconds=remaining * interval)).strftime(
            "%Y-%m-%dT%H:%M:%SZ"
        )
        schedule_meta = {
            "schedule_id": record.get("schedule_id", sched_dir.name),
            "seq": seq,
            "total": count,
            "interval": interval,
            "scheduled_at": now.strftime("%Y-%m-%dT%H:%M:%SZ"),
            "estimated_finish": estimated_finish,
        }
        send_args = {**send_payload, "_schedule": schedule_meta}
        result = self._send(send_args)

        # Skip notification if send failed
        if result.get("error") or result.get("status") == "blocked":
            record["last_sent_at"] = now.strftime("%Y-%m-%dT%H:%M:%SZ")
            self._write_schedule(sched_file, record)
            continue

        # Notify agent about schedule progress (no _wake_nap — routine update)
        to_label = send_payload.get("address", "")
        subj_label = send_payload.get("subject", "(no subject)")
        ts = now.strftime("%Y-%m-%dT%H:%M:%SZ")
        if seq < count:
            next_at = (now + timedelta(seconds=interval)).strftime(
                "%Y-%m-%dT%H:%M:%SZ"
            )
            note = (
                f"[schedule {seq}/{count}] sent to {to_label} "
                f"| subject: {subj_label} "
                f"| sent at {ts} "
                f"| next at {next_at} "
                f"| ends ~{estimated_finish}"
            )
        else:
            note = (
                f"[schedule {seq}/{count}] sent to {to_label} "
                f"| subject: {subj_label} "
                f"| sent at {ts} "
                f"| schedule complete"
            )
        self._agent._log(
            "schedule_send", schedule_id=schedule_meta["schedule_id"],
            seq=seq, total=count, to=to_label, subject=subj_label,
        )
        msg = _make_message(MSG_REQUEST, "system", note)
        self._agent.inbox.put(msg)

        # Update last_sent_at and (if final send) set status to completed
        record["last_sent_at"] = now.strftime("%Y-%m-%dT%H:%M:%SZ")
        if seq >= count:
            record["status"] = "completed"
        self._write_schedule(sched_file, record)
```

The key changes from the original method:
- Removed: agent-level `.cancel` check (was lines 517-518) and per-schedule `.cancel` check (was lines 527-528).
- Added: status guard `if record.get("status", "active") != "active": continue`.
- Added: at the final write (`record["last_sent_at"] = now ...`), if `seq >= count`, also set `record["status"] = "completed"` before the write.

- [ ] **Step 5: Run both new tests to confirm they pass**

Run:
```bash
cd ~/Documents/Github/lingtai-kernel && pytest tests/test_layers_email.py::test_schedule_completion_sets_status_completed tests/test_layers_email.py::test_scheduler_tick_skips_inactive_records -v
```
Expected: Both PASS.

- [ ] **Step 6: Run all tests with `scheduler_` or `tick` in the name**

Run:
```bash
cd ~/Documents/Github/lingtai-kernel && pytest tests/test_layers_email.py -v -k "scheduler or tick"
```
Expected: Mostly PASS. The `.cancel`-based tests (`test_scheduler_cancel_via_sentinel_file`, `test_scheduler_agent_level_cancel`) will FAIL — that's expected and they will be rewritten in Task 6.

- [ ] **Step 7: Smoke import**

Run: `cd ~/Documents/Github/lingtai-kernel && python -c "import lingtai.capabilities.email; print('ok')"`
Expected: `ok`

- [ ] **Step 8: Commit**

```bash
cd ~/Documents/Github/lingtai-kernel
git add src/lingtai/capabilities/email.py tests/test_layers_email.py
git commit -m "feat(email): scheduler tick honors status field and sets status=completed on final send"
```

---

## Task 6: `_schedule_cancel` rewrite (no `.cancel` files)

**Files:**
- Modify: `lingtai-kernel/src/lingtai/capabilities/email.py:406-427` (the `_schedule_cancel` method)
- Test: `lingtai-kernel/tests/test_layers_email.py`

This task replaces the `.cancel` file writes with status mutations. After this task, no code in `email.py` reads or writes any `.cancel` file.

This task ALSO rewrites 4-6 existing tests that asserted sentinel file behavior, and merges duplicates.

- [ ] **Step 1: Write a failing test for single cancel via the handle interface**

Add this test to `lingtai-kernel/tests/test_layers_email.py` (this is the new behavior-level replacement; the old `test_email_schedule_cancel` and `test_schedule_cancel_action_creates_sentinel` will be replaced by it):

```python
def test_schedule_cancel_sets_status_inactive(tmp_path):
    """schedule.cancel should flip the record's status to inactive (no .cancel file)."""
    agent = Agent(service=make_mock_service(), agent_name="test", working_dir=tmp_path / "test",
                       capabilities=["email"])
    mail_svc = MagicMock()
    mail_svc.address = "me"
    mail_svc.send.return_value = None
    agent._mail_service = mail_svc
    mgr = agent.get_capability("email")

    create_result = mgr.handle({
        "address": "someone", "message": "beat",
        "schedule": {"action": "create", "interval": 60, "count": 100},
    })
    sid = create_result["schedule_id"]

    cancel_result = mgr.handle({"schedule": {"action": "cancel", "schedule_id": sid}})
    assert cancel_result["status"] == "paused"
    assert cancel_result["schedule_id"] == sid

    # On-disk record should be inactive
    sched = json.loads(
        (agent.working_dir / "mailbox" / "schedules" / sid / "schedule.json").read_text()
    )
    assert sched["status"] == "inactive"

    # No .cancel file should exist
    assert not (agent.working_dir / "mailbox" / "schedules" / sid / ".cancel").exists()
```

- [ ] **Step 2: Write a failing test for cancel-all (no schedule_id)**

```python
def test_schedule_cancel_all_sets_all_to_inactive(tmp_path):
    """schedule.cancel without schedule_id should flip all active records to inactive."""
    agent = Agent(service=make_mock_service(), agent_name="test", working_dir=tmp_path / "test",
                       capabilities=["email"])
    mail_svc = MagicMock()
    mail_svc.address = "me"
    mail_svc.send.return_value = None
    agent._mail_service = mail_svc
    mgr = agent.get_capability("email")

    r1 = mgr.handle({
        "address": "a", "message": "x",
        "schedule": {"action": "create", "interval": 60, "count": 50},
    })
    r2 = mgr.handle({
        "address": "b", "message": "y",
        "schedule": {"action": "create", "interval": 60, "count": 50},
    })

    cancel_result = mgr.handle({"schedule": {"action": "cancel"}})
    assert cancel_result["status"] == "paused"

    for sid in [r1["schedule_id"], r2["schedule_id"]]:
        sched = json.loads(
            (agent.working_dir / "mailbox" / "schedules" / sid / "schedule.json").read_text()
        )
        assert sched["status"] == "inactive"
        assert not (agent.working_dir / "mailbox" / "schedules" / sid / ".cancel").exists()

    # No agent-level .cancel file either
    assert not (agent.working_dir / "mailbox" / "schedules" / ".cancel").exists()
```

- [ ] **Step 3: Write a failing test for already-inactive cancel (noop)**

```python
def test_schedule_cancel_already_inactive_returns_noop(tmp_path):
    """Cancelling an already-inactive schedule should return already_inactive."""
    agent = Agent(service=make_mock_service(), agent_name="test", working_dir=tmp_path / "test",
                       capabilities=["email"])
    mail_svc = MagicMock()
    mail_svc.address = "me"
    mail_svc.send.return_value = None
    agent._mail_service = mail_svc
    mgr = agent.get_capability("email")

    create_result = mgr.handle({
        "address": "someone", "message": "x",
        "schedule": {"action": "create", "interval": 60, "count": 5},
    })
    sid = create_result["schedule_id"]

    # First cancel — succeeds
    mgr.handle({"schedule": {"action": "cancel", "schedule_id": sid}})

    # Second cancel — already inactive
    second = mgr.handle({"schedule": {"action": "cancel", "schedule_id": sid}})
    assert second["status"] == "already_inactive"
    assert second["schedule_id"] == sid
```

- [ ] **Step 4: Write a failing test for already-completed cancel (noop)**

```python
def test_schedule_cancel_already_completed_returns_noop(tmp_path):
    """Cancelling a completed schedule should return already_completed."""
    agent = Agent(service=make_mock_service(), agent_name="test", working_dir=tmp_path / "test",
                       capabilities=["email"])
    mgr = agent.get_capability("email")

    sched_id = "completed1234"
    sched_dir = agent.working_dir / "mailbox" / "schedules" / sched_id
    sched_dir.mkdir(parents=True, exist_ok=True)
    record = {
        "schedule_id": sched_id,
        "send_payload": {"address": "x", "subject": "", "message": "y", "cc": [], "bcc": [], "type": "normal"},
        "interval": 60, "count": 3, "sent": 3,
        "created_at": "2026-04-06T10:00:00Z",
        "last_sent_at": "2026-04-06T10:02:00Z",
        "status": "completed",
    }
    (sched_dir / "schedule.json").write_text(json.dumps(record))

    result = agent.get_capability("email").handle({"schedule": {"action": "cancel", "schedule_id": sched_id}})
    assert result["status"] == "already_completed"
```

- [ ] **Step 5: Run all 4 new cancel tests to confirm they fail**

Run:
```bash
cd ~/Documents/Github/lingtai-kernel && pytest tests/test_layers_email.py -v -k "test_schedule_cancel_sets_status_inactive or test_schedule_cancel_all_sets_all_to_inactive or test_schedule_cancel_already_inactive or test_schedule_cancel_already_completed"
```
Expected: All FAIL (current code returns `"cancelled"` and writes `.cancel` files; the noop returns are `"already_stopped"`).

- [ ] **Step 6: Replace the `_schedule_cancel` method body**

Edit `lingtai-kernel/src/lingtai/capabilities/email.py` around line 406. Replace the entire `_schedule_cancel` method with:

```python
def _schedule_cancel(self, schedule: dict) -> dict:
    schedule_id = schedule.get("schedule_id")

    if not schedule_id:
        # Agent-level cancel — flip every active-or-missing record to inactive
        schedules_dir = self._schedules_dir
        if not schedules_dir.is_dir():
            return {"status": "paused", "message": "No schedules to cancel"}
        for sched_dir in schedules_dir.iterdir():
            if not sched_dir.is_dir():
                continue
            sched_file = sched_dir / "schedule.json"
            if not sched_file.is_file():
                continue
            try:
                record = json.loads(sched_file.read_text())
            except (json.JSONDecodeError, OSError):
                continue
            status = record.get("status", "active")
            if status in ("inactive", "completed"):
                continue  # already terminal-ish, skip
            record["status"] = "inactive"
            try:
                self._write_schedule(sched_file, record)
            except OSError:
                continue
        return {"status": "paused", "message": "All active schedules paused"}

    # Per-schedule cancel
    record = self._read_schedule(schedule_id)
    if record is None:
        return {"error": f"Schedule not found: {schedule_id}"}
    status = record.get("status", "active")
    if status == "inactive":
        return {"status": "already_inactive", "schedule_id": schedule_id}
    if status == "completed":
        return {"status": "already_completed", "schedule_id": schedule_id}
    # active (or missing/legacy) → flip to inactive
    self._set_schedule_status(schedule_id, "inactive")
    return {"status": "paused", "schedule_id": schedule_id}
```

- [ ] **Step 7: Run the 4 new cancel tests to confirm they pass**

Run:
```bash
cd ~/Documents/Github/lingtai-kernel && pytest tests/test_layers_email.py -v -k "test_schedule_cancel_sets_status_inactive or test_schedule_cancel_all_sets_all_to_inactive or test_schedule_cancel_already_inactive or test_schedule_cancel_already_completed"
```
Expected: All PASS.

- [ ] **Step 8: Delete the obsolete sentinel-asserting tests**

These tests test the old implementation detail (`.cancel` files) which no longer exists. They are replaced by the behavior-level tests added in Steps 1-4. Delete these tests from `lingtai-kernel/tests/test_layers_email.py`:

- `test_email_schedule_cancel` (around line 1040) — duplicate of new `test_schedule_cancel_sets_status_inactive`
- `test_email_schedule_cancel_missing_id` (around line 1078) — duplicate of new `test_schedule_cancel_all_sets_all_to_inactive`
- `test_email_schedule_cancel_already_stopped` (around line 1087) — replaced by `test_schedule_cancel_already_inactive_returns_noop` and `test_schedule_cancel_already_completed_returns_noop`
- `test_scheduler_cancel_via_sentinel_file` (around line 1347) — assertion is on `.cancel` file existence, no longer meaningful
- `test_scheduler_agent_level_cancel` (around line 1374) — same
- `test_schedule_cancel_action_creates_sentinel` (around line 1440) — same
- `test_schedule_cancel_all_creates_agent_sentinel` (around line 1462) — same

Use the Read tool first to find each test by name (the line numbers may have shifted due to earlier additions), then use Edit to remove the entire `def test_...` block including its docstring and body.

`test_email_schedule_cancel_not_found` (around line 1069) is NOT deleted — its assertion (`"error" in result`) still holds for the new code.

- [ ] **Step 9: Run the full schedule test set to confirm only schedule tests pass**

Run:
```bash
cd ~/Documents/Github/lingtai-kernel && pytest tests/test_layers_email.py -v -k "schedule or scheduler"
```
Expected: All PASS. No `.cancel`-based assertions should remain.

- [ ] **Step 10: Smoke import**

Run: `cd ~/Documents/Github/lingtai-kernel && python -c "import lingtai.capabilities.email; print('ok')"`
Expected: `ok`

- [ ] **Step 11: Verify no `.cancel` references remain in email.py**

Run: `cd ~/Documents/Github/lingtai-kernel && grep -n "\.cancel" src/lingtai/capabilities/email.py`
Expected: No output (or only matches inside string literals that are intentional, e.g., comments — but ideally none).

If any matches appear, investigate and remove them — this refactor should leave no `.cancel` file logic in `email.py`.

- [ ] **Step 12: Commit**

```bash
cd ~/Documents/Github/lingtai-kernel
git add src/lingtai/capabilities/email.py tests/test_layers_email.py
git commit -m "refactor(email): replace .cancel sentinel files with status field; cancel = pause"
```

---

## Task 7: `_schedule_list` returns `status` field, drops boolean flags

**Files:**
- Modify: `lingtai-kernel/src/lingtai/capabilities/email.py:429-470` (`_schedule_list`)
- Test: `lingtai-kernel/tests/test_layers_email.py`

This task makes `_schedule_list` include the canonical `status` field on each entry and drops the legacy `cancelled` / `active` boolean fields. Verified during brainstorming that no Go consumer reads these fields, so the change is safe.

- [ ] **Step 1: Read existing list tests to understand the assertion patterns**

Run: `cd ~/Documents/Github/lingtai-kernel && pytest tests/test_layers_email.py -v -k "schedule_list" --collect-only`
Expected: Lists tests like `test_email_schedule_list_empty`, `test_email_schedule_list_shows_active`, `test_email_schedule_list_shows_completed`.

- [ ] **Step 2: Write a failing test for `status` field in list output**

Add this test to `lingtai-kernel/tests/test_layers_email.py`:

```python
def test_schedule_list_returns_status_field(tmp_path):
    """list should return a status field on each entry, not the legacy active/cancelled booleans."""
    agent = Agent(service=make_mock_service(), agent_name="test", working_dir=tmp_path / "test",
                       capabilities=["email"])
    mail_svc = MagicMock()
    mail_svc.address = "me"
    mail_svc.send.return_value = None
    agent._mail_service = mail_svc
    mgr = agent.get_capability("email")

    # Create one schedule and cancel it; create another and leave it active
    r1 = mgr.handle({
        "address": "a", "message": "x",
        "schedule": {"action": "create", "interval": 60, "count": 5},
    })
    mgr.handle({"schedule": {"action": "cancel", "schedule_id": r1["schedule_id"]}})

    r2 = mgr.handle({
        "address": "b", "message": "y",
        "schedule": {"action": "create", "interval": 60, "count": 5},
    })

    listing = mgr.handle({"schedule": {"action": "list"}})
    assert listing["status"] == "ok"
    assert len(listing["schedules"]) == 2

    by_id = {s["schedule_id"]: s for s in listing["schedules"]}
    # New status field present
    assert by_id[r1["schedule_id"]]["status"] == "inactive"
    assert by_id[r2["schedule_id"]]["status"] == "active"
    # Legacy boolean fields gone
    for entry in listing["schedules"]:
        assert "cancelled" not in entry
        assert "active" not in entry
```

- [ ] **Step 3: Run the test to confirm it fails**

Run: `cd ~/Documents/Github/lingtai-kernel && pytest tests/test_layers_email.py::test_schedule_list_returns_status_field -v`
Expected: FAIL — likely with `KeyError: 'status'` or `assert 'cancelled' not in entry` (depending on which assertion fails first).

- [ ] **Step 4: Update `_schedule_list` to include `status` and drop the booleans**

Edit `lingtai-kernel/src/lingtai/capabilities/email.py` around line 429. Replace the `_schedule_list` method with:

```python
def _schedule_list(self) -> dict:
    schedules_dir = self._schedules_dir
    if not schedules_dir.is_dir():
        return {"status": "ok", "schedules": []}

    entries = []
    for sched_dir in schedules_dir.iterdir():
        if not sched_dir.is_dir():
            continue
        sched_file = sched_dir / "schedule.json"
        if not sched_file.is_file():
            continue
        try:
            record = json.loads(sched_file.read_text())
        except (json.JSONDecodeError, OSError):
            continue

        payload = record.get("send_payload", {})
        address = payload.get("address", "")
        if isinstance(address, list):
            address = ", ".join(address)

        sent = record.get("sent", 0)
        count = record.get("count", 0)

        entries.append({
            "schedule_id": record.get("schedule_id", sched_dir.name),
            "to": address,
            "subject": payload.get("subject", ""),
            "interval": record.get("interval", 0),
            "count": count,
            "sent": sent,
            "status": record.get("status", "active"),
            "created_at": record.get("created_at", ""),
            "last_sent_at": record.get("last_sent_at"),
        })

    entries.sort(key=lambda e: e.get("created_at", ""), reverse=True)
    return {"status": "ok", "schedules": entries}
```

The key changes:
- Added `"status": record.get("status", "active")` to the entry dict.
- Removed `cancelled = (sched_dir / ".cancel").is_file() or (schedules_dir / ".cancel").is_file()` line.
- Removed `active = sent < count and not cancelled` line.
- Removed `"cancelled": cancelled,` and `"active": active,` from the entry dict.

- [ ] **Step 5: Run the new list test to confirm it passes**

Run: `cd ~/Documents/Github/lingtai-kernel && pytest tests/test_layers_email.py::test_schedule_list_returns_status_field -v`
Expected: PASS.

- [ ] **Step 6: Run the existing list tests to catch any that depended on the booleans**

Run:
```bash
cd ~/Documents/Github/lingtai-kernel && pytest tests/test_layers_email.py -v -k "schedule_list"
```
Expected: All PASS. If any fail because they asserted `entry["active"]` or `entry["cancelled"]`, update those tests to use `entry["status"]` instead. (Don't add the booleans back — drop them everywhere.)

- [ ] **Step 7: Smoke import**

Run: `cd ~/Documents/Github/lingtai-kernel && python -c "import lingtai.capabilities.email; print('ok')"`
Expected: `ok`

- [ ] **Step 8: Commit**

```bash
cd ~/Documents/Github/lingtai-kernel
git add src/lingtai/capabilities/email.py tests/test_layers_email.py
git commit -m "refactor(email): _schedule_list returns status field, drops legacy active/cancelled booleans"
```

---

## Task 8: i18n updates (en / zh / wen)

**Files:**
- Modify: `lingtai-kernel/src/lingtai/i18n/en.json:75-78`
- Modify: `lingtai-kernel/src/lingtai/i18n/zh.json:75-78`
- Modify: `lingtai-kernel/src/lingtai/i18n/wen.json:75-78`

This task updates the schedule action description and the schedule_id description in all three locale files. No new keys are added.

Per project memory: when updating i18n keys, always update en.json, zh.json, AND wen.json together.

- [ ] **Step 1: Update `en.json`**

Edit `lingtai-kernel/src/lingtai/i18n/en.json`. Find the lines around 75-78:

```json
"email.schedule_action": "create: start a recurring send (requires address, message + schedule.interval, schedule.count). cancel: stop a running schedule (requires schedule.schedule_id). list: show all schedules with progress.",
```

Replace with:

```json
"email.schedule_action": "create: start a recurring send (requires address, message + schedule.interval, schedule.count). cancel: pause a running schedule (requires schedule.schedule_id). reactivate: resume a paused schedule (requires schedule.schedule_id). list: show all schedules with status.",
```

And replace the `schedule_id` line:

```json
"email.schedule_id": "Schedule ID (for cancel)",
```

with:

```json
"email.schedule_id": "Schedule ID (for cancel/reactivate)",
```

- [ ] **Step 2: Update `zh.json`**

Edit `lingtai-kernel/src/lingtai/i18n/zh.json`. Replace:

```json
"email.schedule_action": "create：启动定期发送（需要 address、message + schedule.interval、schedule.count）。cancel：停止运行中的计划（需要 schedule.schedule_id）。list：显示所有计划及进度。",
```

with:

```json
"email.schedule_action": "create：启动定期发送（需要 address、message + schedule.interval、schedule.count）。cancel：暂停运行中的计划（需要 schedule.schedule_id）。reactivate：恢复已暂停的计划（需要 schedule.schedule_id）。list：显示所有计划及状态。",
```

And replace:

```json
"email.schedule_id": "计划 ID（用于 cancel）",
```

with:

```json
"email.schedule_id": "计划 ID（用于 cancel/reactivate）",
```

- [ ] **Step 3: Update `wen.json`**

Edit `lingtai-kernel/src/lingtai/i18n/wen.json`. Replace:

```json
"email.schedule_action": "create：设定期发信（须 address、message + schedule.interval、schedule.count）。cancel：撤运行中之定期（须 schedule.schedule_id）。list：查阅诸定期及进度。",
```

with:

```json
"email.schedule_action": "create：设定期发信（须 address、message + schedule.interval、schedule.count）。cancel：暂止运行中之定期（须 schedule.schedule_id）。reactivate：复启已暂止之定期（须 schedule.schedule_id）。list：查阅诸定期及状态。",
```

And replace:

```json
"email.schedule_id": "定期之 ID（用于 cancel）",
```

with:

```json
"email.schedule_id": "定期之 ID（用于 cancel/reactivate）",
```

- [ ] **Step 4: Verify the JSON files still parse**

Run:
```bash
cd ~/Documents/Github/lingtai-kernel && python -c "import json; [json.loads(open(f).read()) for f in ['src/lingtai/i18n/en.json', 'src/lingtai/i18n/zh.json', 'src/lingtai/i18n/wen.json']]; print('ok')"
```
Expected: `ok`. If any file has a JSON syntax error (trailing comma, missing quote), fix it.

- [ ] **Step 5: Run the i18n test if one exists**

Run: `cd ~/Documents/Github/lingtai-kernel && pytest tests/test_i18n.py -v 2>/dev/null || echo "no i18n tests"`
Expected: PASS or "no i18n tests". If the test fails because it asserts that all keys exist in all locales (a common parity check), the issue is likely a typo — fix it.

- [ ] **Step 6: Commit**

```bash
cd ~/Documents/Github/lingtai-kernel
git add src/lingtai/i18n/en.json src/lingtai/i18n/zh.json src/lingtai/i18n/wen.json
git commit -m "i18n(email): update schedule_action and schedule_id descriptions for reactivate"
```

---

## Task 9: Final verification

**Files:** None modified — this is a verification-only task.

This task runs the full verification gate from the spec to confirm the refactor is complete and correct.

- [ ] **Step 1: Smoke import**

Run: `cd ~/Documents/Github/lingtai-kernel && python -c "import lingtai.capabilities.email; print('ok')"`
Expected: `ok`

- [ ] **Step 2: Run the full email test file**

Run: `cd ~/Documents/Github/lingtai-kernel && pytest tests/test_layers_email.py -v`
Expected: All tests PASS. There should be approximately:
- ~16 untouched tests (parameter validation, send mechanics, notification content)
- ~4 rewritten tests (recovery, list)
- ~14 new tests added across Tasks 1-7
- ~7 deleted tests (the old sentinel-asserting cancel tests)

If any test fails, investigate before proceeding. The plan should leave the test suite green.

- [ ] **Step 3: Visual diff of `email.py` to confirm no `.cancel` references remain**

Run: `cd ~/Documents/Github/lingtai-kernel && grep -n "\.cancel" src/lingtai/capabilities/email.py`
Expected: No output. If any matches appear, investigate.

- [ ] **Step 4: Visual diff of i18n files to confirm all three locales are updated**

Run:
```bash
cd ~/Documents/Github/lingtai-kernel && grep -n "reactivate" src/lingtai/i18n/en.json src/lingtai/i18n/zh.json src/lingtai/i18n/wen.json
```
Expected: At least one match per file (in the `email.schedule_action` value).

- [ ] **Step 5: Run a broader test sweep to catch any unrelated regression**

Run: `cd ~/Documents/Github/lingtai-kernel && pytest tests/ -x -q 2>&1 | tail -20`
Expected: All PASS, or only failures unrelated to email/schedule. If a previously-passing unrelated test fails, investigate before declaring success.

- [ ] **Step 6: View the full git log of this refactor**

Run: `cd ~/Documents/Github/lingtai-kernel && git log --oneline -10`
Expected: 8 commits from this plan, in order:
1. `feat(email): add reactivate action to schedule schema and dispatch (stub)`
2. `feat(email): add status field to schedule records and _set_schedule_status helper`
3. `feat(email): implement _schedule_reactivate with self-heal for crash-mid-completion`
4. `feat(email): startup reconciliation pauses all in-flight schedules on restart`
5. `feat(email): scheduler tick honors status field and sets status=completed on final send`
6. `refactor(email): replace .cancel sentinel files with status field; cancel = pause`
7. `refactor(email): _schedule_list returns status field, drops legacy active/cancelled booleans`
8. `i18n(email): update schedule_action and schedule_id descriptions for reactivate`

- [ ] **Step 7: Notify the user that the implementation is complete**

Tell the user the refactor is done and remind them about the deferred follow-ups:
- The 灵台日报 changelog entry (per `reference_daily_ribao` memory note) should be added in a follow-up commit.
- The kernel changelog or release notes should mention the BREAKING change.

---

## Notes for the Implementer

- **Cross-repo work:** All edits are in `~/Documents/Github/lingtai-kernel`, NOT the current working directory `~/Documents/Github/lingtai`. Always `cd` into the kernel repo before running commands.
- **Atomic file writes:** The existing `_write_schedule()` helper handles temp-file-and-rename. New code reuses it — never write `schedule.json` directly.
- **Concurrency caveat:** Per the spec, there is a known race between cancel and the scheduler tick that can result in one extra send slipping through. This is documented and intentionally not fixed in this refactor. Don't add locks.
- **Test sleep timings:** The existing test style uses `time.sleep(N)` with the scheduler's 1-second polling loop. Tests in this plan match that style. Don't try to be cleverer with mocks unless an existing test uses them.
- **The `_log` call in `_scheduler_tick`** at line ~609 references `self._agent._log` and `self._agent.inbox`. These are existing attributes of `BaseAgent` — no need to add or modify them.
- **D5 (stale `.cancel` files):** Old `.cancel` files left on disk by the previous code are NOT cleaned up. They become dead files that nothing reads. Don't add cleanup logic.
- **D6 (verification scope):** Always run the FULL `test_layers_email.py`, never just the schedule subset, when verifying.
