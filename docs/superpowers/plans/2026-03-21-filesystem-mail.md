# Filesystem-Based Mail Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace TCP-based mail with filesystem-based mail where the working directory path IS the address, and make humans first-class mail participants.

**Architecture:** Two repos change: `lingtai-kernel` (MailService ABC, FilesystemMailService, heartbeat file, state rename, mail intrinsic) and `lingtai` (email capability, Go daemon TUI). The kernel gets a new `FilesystemMailService` that delivers by writing files to recipients' inbox directories and monitors its own inbox via polling. The Go daemon replaces TCP `MailClient`/`MailListener` with filesystem read/write.

**Tech Stack:** Python 3.11+, Go, filesystem operations (pathlib, os, shutil)

**Spec:** `docs/superpowers/specs/2026-03-21-filesystem-mail-design.md`

---

### Task 1: Rename AgentState.ERROR → STUCK (Kernel)

**Files:**
- Modify: `/Users/huangzesen/Documents/GitHub/lingtai-kernel/src/lingtai_kernel/state.py:18-21`
- Modify: `/Users/huangzesen/Documents/GitHub/lingtai-kernel/src/lingtai_kernel/base_agent.py` (lines 5, 618, 622, 696, 704)
- Modify: `/Users/huangzesen/Documents/GitHub/lingtai-kernel/src/lingtai_kernel/i18n/en.json:6`
- Modify: `/Users/huangzesen/Documents/GitHub/lingtai-kernel/src/lingtai_kernel/i18n/zh.json:6`
- Modify: `/Users/huangzesen/Documents/GitHub/lingtai-kernel/tests/test_state.py:7`
- Modify: `/Users/huangzesen/Documents/GitHub/lingtai-kernel/tests/test_heartbeat.py` (lines 58, 67, 78-80, 87, 109, 135, 155, 181)
- Modify: `/Users/huangzesen/Documents/GitHub/lingtai/src/lingtai/capabilities/avatar.py:127-141`
- Modify: `/Users/huangzesen/Documents/GitHub/lingtai/src/lingtai/i18n/wen.json:6`
- Test: existing tests in both repos

- [ ] **Step 1: Rename enum in state.py**

In `lingtai-kernel/src/lingtai_kernel/state.py`:
```python
# Change:
ERROR = "error"
# To:
STUCK = "stuck"
```
Update the docstring transitions too:
```python
"""Lifecycle state of an agent.

ACTIVE --(completed)--------> IDLE
ACTIVE --(timeout/exception)-> STUCK
IDLE   --(inbox message)----> ACTIVE
STUCK  --(AED)--------------> ACTIVE  (session reset, fresh run loop)
STUCK  --(AED timeout)------> DEAD    (shutdown)
"""
```

- [ ] **Step 2: Update all AgentState.ERROR references in base_agent.py**

Replace every `AgentState.ERROR` with `AgentState.STUCK` in `base_agent.py`. Key locations:
- Line 5 docstring: "4-state lifecycle: ACTIVE, IDLE, STUCK, DEAD"
- Line 618/622: heartbeat loop `if self._state == AgentState.STUCK:`
- Lines 696, 704: `sleep_state = AgentState.STUCK`

- [ ] **Step 3: Update i18n strings**

In `lingtai-kernel/src/lingtai_kernel/i18n/en.json`:
```json
"system.error_revive": "[system] You were stuck at {ts}, reviving..."
```
Rename key to `system.stuck_revive` and update the string. Update the reference in `base_agent.py` `_perform_aed()` (line 657).

In `lingtai-kernel/src/lingtai_kernel/i18n/zh.json`:
```json
"system.stuck_revive": "[系统] 你在 {ts} 陷入困境，正在恢复…"
```

In `lingtai/src/lingtai/i18n/wen.json`:
```json
"system.stuck_revive": "[系统] 汝于 {ts} 误入歧途，今已唤醒……"
```

- [ ] **Step 4: Update avatar capability**

In `lingtai/src/lingtai/capabilities/avatar.py`, `_live_status()`:
```python
if state == AgentState.STUCK:
    return "stuck"
```

- [ ] **Step 5: Update all test files**

Replace `AgentState.ERROR` with `AgentState.STUCK` in:
- `lingtai-kernel/tests/test_state.py`: `assert AgentState.STUCK.value == "stuck"`
- `lingtai-kernel/tests/test_heartbeat.py`: all ERROR → STUCK references

- [ ] **Step 6: Run all kernel tests**

```bash
cd /Users/huangzesen/Documents/GitHub/lingtai-kernel && source ../lingtai/venv/bin/activate && python -m pytest tests/ -v
```

- [ ] **Step 7: Run all lingtai tests**

```bash
cd /Users/huangzesen/Documents/GitHub/lingtai && source venv/bin/activate && python -m pytest tests/ -v
```

- [ ] **Step 8: Smoke-test imports**

```bash
python -c "from lingtai_kernel import AgentState; assert AgentState.STUCK.value == 'stuck'; print('OK')"
python -c "import lingtai; print('OK')"
```

- [ ] **Step 9: Commit**

```bash
cd /Users/huangzesen/Documents/GitHub/lingtai-kernel && git add -A && git commit -m "refactor: rename AgentState.ERROR → STUCK"
cd /Users/huangzesen/Documents/GitHub/lingtai && git add -A && git commit -m "refactor: follow kernel AgentState.ERROR → STUCK rename"
```

---

### Task 2: Heartbeat Counter → Timestamp + Disk Write (Kernel)

**Files:**
- Modify: `/Users/huangzesen/Documents/GitHub/lingtai-kernel/src/lingtai_kernel/base_agent.py` (lines 232, 600-660, 1205)
- Modify: `/Users/huangzesen/Documents/GitHub/lingtai-kernel/tests/test_heartbeat.py`
- Test: `test_heartbeat.py`

- [ ] **Step 1: Write failing test for heartbeat file**

Add to `lingtai-kernel/tests/test_heartbeat.py`:
```python
def test_heartbeat_writes_file(self, tmp_path):
    from lingtai_kernel import BaseAgent
    agent = BaseAgent(
        service=make_mock_service(),
        agent_name="test",
        base_dir=tmp_path,
    )
    agent._start_heartbeat()
    time.sleep(1.5)
    agent._stop_heartbeat()

    hb_file = agent._working_dir / ".agent.heartbeat"
    assert not hb_file.exists(), "heartbeat file should be deleted after stop"

def test_heartbeat_file_written_while_running(self, tmp_path):
    from lingtai_kernel import BaseAgent, AgentState
    agent = BaseAgent(
        service=make_mock_service(),
        agent_name="test",
        base_dir=tmp_path,
    )
    agent._start_heartbeat()
    agent._set_state(AgentState.ACTIVE, reason="test")
    time.sleep(1.5)

    hb_file = agent._working_dir / ".agent.heartbeat"
    assert hb_file.exists()
    ts = float(hb_file.read_text().strip())
    assert time.time() - ts < 2.0

    agent._stop_heartbeat()

def test_heartbeat_file_stale_when_dead(self, tmp_path):
    from lingtai_kernel import BaseAgent, AgentState
    agent = BaseAgent(
        service=make_mock_service(),
        agent_name="test",
        base_dir=tmp_path,
    )
    agent._start_heartbeat()
    agent._set_state(AgentState.ACTIVE, reason="test")
    time.sleep(1.5)
    agent._set_state(AgentState.DEAD, reason="test")
    agent._shutdown.set()
    time.sleep(1.5)

    hb_file = agent._working_dir / ".agent.heartbeat"
    # File may exist but should be stale (>2s old) or thread exited
    if hb_file.exists():
        ts = float(hb_file.read_text().strip())
        assert time.time() - ts > 1.0
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/huangzesen/Documents/GitHub/lingtai-kernel && python -m pytest tests/test_heartbeat.py -v -k "heartbeat_file or heartbeat_writes"
```

- [ ] **Step 3: Change heartbeat from counter to timestamp**

In `base_agent.py`:
```python
# Line 232: change type
self._heartbeat: float = 0.0  # UTC timestamp (time.time())

# In _heartbeat_loop (line 617-644):
def _heartbeat_loop(self) -> None:
    """Beat every 1 second. Write heartbeat file. AED if STUCK."""
    hb_file = self._working_dir / ".agent.heartbeat"
    while self._heartbeat_thread is not None and not self._shutdown.is_set():
        self._heartbeat = time.time()

        # Write heartbeat file when alive (ACTIVE, IDLE, STUCK)
        if self._state != AgentState.DEAD:
            try:
                hb_file.write_text(str(self._heartbeat))
            except OSError:
                pass

        if self._state == AgentState.STUCK:
            # ... existing AED logic unchanged ...
        else:
            self._cpr_start = None
            self._aed_pending = False

        time.sleep(1.0)

# In _stop_heartbeat (line 612-615):
def _stop_heartbeat(self) -> None:
    """Stop the heartbeat (called only by stop/shutdown)."""
    self._heartbeat_thread = None
    # Delete heartbeat file
    hb_file = self._working_dir / ".agent.heartbeat"
    try:
        hb_file.unlink(missing_ok=True)
    except OSError:
        pass
    self._log("heartbeat_stop", heartbeat=self._heartbeat)
```

- [ ] **Step 4: Update existing heartbeat tests**

Update tests that check `agent._heartbeat` as integer:
- `test_heartbeat_counter_initialized`: assert `agent._heartbeat == 0.0`
- `test_heartbeat_increments`: assert `agent._heartbeat > 0` and `time.time() - agent._heartbeat < 2.0`
- `test_heartbeat_in_status`: assert `isinstance(status["heartbeat"], float)`

- [ ] **Step 5: Run all heartbeat tests**

```bash
cd /Users/huangzesen/Documents/GitHub/lingtai-kernel && python -m pytest tests/test_heartbeat.py -v
```

- [ ] **Step 6: Smoke-test**

```bash
python -c "from lingtai_kernel import BaseAgent; print('OK')"
```

- [ ] **Step 7: Commit**

```bash
cd /Users/huangzesen/Documents/GitHub/lingtai-kernel && git add -A && git commit -m "feat: heartbeat writes timestamp to .agent.heartbeat file"
```

---

### Task 3: FilesystemMailService (Kernel)

**Files:**
- Rewrite: `/Users/huangzesen/Documents/GitHub/lingtai-kernel/src/lingtai_kernel/services/mail.py`
- Create: `/Users/huangzesen/Documents/GitHub/lingtai-kernel/tests/test_filesystem_mail.py`
- Test: new test file

- [ ] **Step 1: Write failing tests for FilesystemMailService**

Create `lingtai-kernel/tests/test_filesystem_mail.py`:
```python
"""Tests for FilesystemMailService — filesystem-based mail delivery."""
from __future__ import annotations
import json
import time
from pathlib import Path
from unittest.mock import MagicMock

import pytest


def _make_agent_dir(base: Path, agent_id: str) -> Path:
    """Create a minimal agent working dir with .agent.json."""
    d = base / agent_id
    d.mkdir()
    (d / ".agent.json").write_text(json.dumps({
        "agent_id": agent_id,
        "agent_name": "test",
    }))
    # Write fresh heartbeat
    (d / ".agent.heartbeat").write_text(str(time.time()))
    return d


class TestSend:

    def test_send_creates_message(self, tmp_path):
        from lingtai_kernel.services.mail import FilesystemMailService
        sender_dir = _make_agent_dir(tmp_path, "sender01")
        recip_dir = _make_agent_dir(tmp_path, "recip01")
        (recip_dir / "mailbox" / "inbox").mkdir(parents=True)

        svc = FilesystemMailService(sender_dir, mailbox_rel="mailbox")
        result = svc.send(str(recip_dir), {"message": "hello", "subject": "test"})
        assert result is None  # success

        inbox = recip_dir / "mailbox" / "inbox"
        msgs = list(inbox.iterdir())
        assert len(msgs) == 1
        data = json.loads((msgs[0] / "message.json").read_text())
        assert data["message"] == "hello"

    def test_send_copies_attachments(self, tmp_path):
        from lingtai_kernel.services.mail import FilesystemMailService
        sender_dir = _make_agent_dir(tmp_path, "sender01")
        recip_dir = _make_agent_dir(tmp_path, "recip01")
        (recip_dir / "mailbox" / "inbox").mkdir(parents=True)

        # Create a file to attach
        att = sender_dir / "report.txt"
        att.write_text("data")

        svc = FilesystemMailService(sender_dir, mailbox_rel="mailbox")
        result = svc.send(str(recip_dir), {
            "message": "see attached",
            "attachments": [str(att)],
        })
        assert result is None

        inbox = recip_dir / "mailbox" / "inbox"
        msg_dir = list(inbox.iterdir())[0]
        att_dir = msg_dir / "attachments"
        assert att_dir.exists()
        assert (att_dir / "report.txt").read_text() == "data"

    def test_send_fails_no_agent_json(self, tmp_path):
        from lingtai_kernel.services.mail import FilesystemMailService
        sender_dir = _make_agent_dir(tmp_path, "sender01")
        bad_dir = tmp_path / "noagent"
        bad_dir.mkdir()

        svc = FilesystemMailService(sender_dir, mailbox_rel="mailbox")
        result = svc.send(str(bad_dir), {"message": "hello"})
        assert result is not None  # error string
        assert "no agent" in result.lower()

    def test_send_fails_stale_heartbeat(self, tmp_path):
        from lingtai_kernel.services.mail import FilesystemMailService
        sender_dir = _make_agent_dir(tmp_path, "sender01")
        recip_dir = _make_agent_dir(tmp_path, "recip01")
        (recip_dir / "mailbox" / "inbox").mkdir(parents=True)
        # Write stale heartbeat
        (recip_dir / ".agent.heartbeat").write_text(str(time.time() - 10))

        svc = FilesystemMailService(sender_dir, mailbox_rel="mailbox")
        result = svc.send(str(recip_dir), {"message": "hello"})
        assert result is not None
        assert "not running" in result.lower()

    def test_send_fails_agent_id_mismatch(self, tmp_path):
        from lingtai_kernel.services.mail import FilesystemMailService
        sender_dir = _make_agent_dir(tmp_path, "sender01")
        recip_dir = _make_agent_dir(tmp_path, "recip01")
        (recip_dir / "mailbox" / "inbox").mkdir(parents=True)

        svc = FilesystemMailService(sender_dir, mailbox_rel="mailbox")
        result = svc.send(
            str(recip_dir),
            {"message": "hello"},
            expected_agent_id="wrong_id",
        )
        assert result is not None
        assert "changed" in result.lower()


class TestListen:

    def test_listen_detects_new_message(self, tmp_path):
        from lingtai_kernel.services.mail import FilesystemMailService
        agent_dir = _make_agent_dir(tmp_path, "agent01")
        (agent_dir / "mailbox" / "inbox").mkdir(parents=True)

        received = []
        svc = FilesystemMailService(agent_dir, mailbox_rel="mailbox")
        svc.listen(on_message=lambda p: received.append(p))

        # Simulate incoming mail (another agent writes to our inbox)
        msg_dir = agent_dir / "mailbox" / "inbox" / "test-uuid-1"
        msg_dir.mkdir()
        (msg_dir / "message.json").write_text(json.dumps({
            "from": "/tmp/other",
            "message": "hi",
        }))

        time.sleep(1.0)
        svc.stop()
        assert len(received) == 1
        assert received[0]["message"] == "hi"

    def test_listen_ignores_existing_messages(self, tmp_path):
        from lingtai_kernel.services.mail import FilesystemMailService
        agent_dir = _make_agent_dir(tmp_path, "agent01")
        inbox = agent_dir / "mailbox" / "inbox"
        inbox.mkdir(parents=True)

        # Pre-existing message
        old = inbox / "old-uuid"
        old.mkdir()
        (old / "message.json").write_text(json.dumps({"message": "old"}))

        received = []
        svc = FilesystemMailService(agent_dir, mailbox_rel="mailbox")
        svc.listen(on_message=lambda p: received.append(p))
        time.sleep(1.0)
        svc.stop()
        assert len(received) == 0


class TestAddress:

    def test_address_returns_working_dir(self, tmp_path):
        from lingtai_kernel.services.mail import FilesystemMailService
        agent_dir = _make_agent_dir(tmp_path, "agent01")
        svc = FilesystemMailService(agent_dir, mailbox_rel="mailbox")
        assert svc.address == str(agent_dir)
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/huangzesen/Documents/GitHub/lingtai-kernel && python -m pytest tests/test_filesystem_mail.py -v
```

- [ ] **Step 3: Implement FilesystemMailService**

Rewrite `lingtai-kernel/src/lingtai_kernel/services/mail.py`:
- Keep MailService ABC but simplify: `address` returns `str` (not `str | None`), remove port/banner params
- Delete `TCPMailService` entirely
- Implement `FilesystemMailService`:
  - `__init__(self, working_dir, mailbox_rel="mailbox")`: stores paths, creates mailbox dirs
  - `address` property: returns `str(self._working_dir)`
  - `send(self, address, payload, *, expected_agent_id=None)`: handshake + file write + attachment copy
  - `listen(self, on_message)`: starts polling daemon thread
  - `stop()`: signals thread exit
  - Handshake: read `.agent.json` (verify exists, optionally verify agent_id), read `.agent.heartbeat` (verify `time.time() - ts < 2.0`)
  - Atomic write: write `message.json.tmp` then `os.replace()` to `message.json`
  - Attachments: read `payload["attachments"]`, copy files with `shutil.copy2()`

- [ ] **Step 4: Run new tests**

```bash
cd /Users/huangzesen/Documents/GitHub/lingtai-kernel && python -m pytest tests/test_filesystem_mail.py -v
```

- [ ] **Step 5: Run old mail service tests, update or delete as needed**

The old `tests/test_services_mail.py` tests TCPMailService. Delete it and ensure the new test file covers all scenarios.

```bash
cd /Users/huangzesen/Documents/GitHub/lingtai-kernel && python -m pytest tests/ -v
```

- [ ] **Step 6: Smoke-test**

```bash
python -c "from lingtai_kernel.services.mail import FilesystemMailService; print('OK')"
```

- [ ] **Step 7: Commit**

```bash
cd /Users/huangzesen/Documents/GitHub/lingtai-kernel && git add -A && git commit -m "feat: FilesystemMailService replaces TCPMailService"
```

---

### Task 4: Update BaseAgent for Filesystem Mail (Kernel)

**Files:**
- Modify: `/Users/huangzesen/Documents/GitHub/lingtai-kernel/src/lingtai_kernel/base_agent.py` (lines 125-126, 174-192, 263-279, 405, 1041-1058)
- Modify: `/Users/huangzesen/Documents/GitHub/lingtai-kernel/tests/test_base_agent.py`

- [ ] **Step 1: Remove TCP-specific code from BaseAgent**

In `base_agent.py`:
- Remove `_banner_id` setup (lines 187-192)
- Remove `_info_handler` / `_get_discovery_info()` (lines 263-279) — no more TCP discovery
- Remove any port-related constructor params
- `_build_manifest()`: `address` field already reads `self._mail_service.address` — now returns a path instead of host:port. No code change needed.
- `_on_mail_received()`: no changes needed — same callback signature
- `_on_normal_mail()`: no changes needed — same notification logic

- [ ] **Step 2: Update start() to pass correct listen callback**

The `listen()` call at line 405 stays the same:
```python
self._mail_service.listen(on_message=lambda payload: self._on_mail_received(payload))
```
No change needed — `FilesystemMailService.listen()` uses the same callback signature.

- [ ] **Step 3: Run kernel tests**

```bash
cd /Users/huangzesen/Documents/GitHub/lingtai-kernel && python -m pytest tests/ -v
```

- [ ] **Step 4: Smoke-test**

```bash
python -c "from lingtai_kernel import BaseAgent; print('OK')"
```

- [ ] **Step 5: Commit**

```bash
cd /Users/huangzesen/Documents/GitHub/lingtai-kernel && git add -A && git commit -m "refactor: remove TCP discovery and banner from BaseAgent"
```

---

### Task 5: Update Mail Intrinsic (Kernel)

**Files:**
- Modify: `/Users/huangzesen/Documents/GitHub/lingtai-kernel/src/lingtai_kernel/intrinsics/mail.py` (lines 277-330, 336-381)
- Test: existing mail intrinsic tests

- [ ] **Step 1: Update _mailman dispatch**

In `intrinsics/mail.py`, the `_mailman()` function (line 277-330):
- The `mail_service.send(address, payload)` call stays the same — the new `FilesystemMailService.send()` has the same signature
- Self-send logic stays the same — `_persist_to_inbox()` writes directly
- Attachment handling: currently resolves paths and includes them in payload — this stays the same. `FilesystemMailService.send()` reads `payload["attachments"]` and copies files.

The main change: `_mailman` currently strips logical prefixes from addresses (e.g., `alice@127.0.0.1:8501` → `127.0.0.1:8501`). This prefix stripping can be removed since addresses are now paths.

- [ ] **Step 2: Wire up agent_id verification in _mailman**

In `_mailman()`, before calling `mail_service.send()`, look up the recipient address in `contacts.json` and pass `expected_agent_id` if found:

```python
# In _mailman, before dispatch:
contacts_path = _mailbox_dir(agent) / "contacts.json"
expected_id = None
if contacts_path.is_file():
    try:
        contacts = json.loads(contacts_path.read_text())
        for c in contacts:
            if c.get("address") == address:
                expected_id = c.get("agent_id")
                break
    except (json.JSONDecodeError, OSError):
        pass

err = agent._mail_service.send(address, payload, expected_agent_id=expected_id)
```

This implements the spec's handshake step 2: verify `agent_id` if recipient is in contacts, skip if unknown.

- [ ] **Step 3: Update _send handler**

The `_send` handler (line 336-381) validates addresses and creates outbox entries. Update address validation to accept filesystem paths instead of host:port.

- [ ] **Step 3: Run mail intrinsic tests**

```bash
cd /Users/huangzesen/Documents/GitHub/lingtai && source venv/bin/activate && python -m pytest tests/test_intrinsics_comm.py -v
```

- [ ] **Step 4: Commit**

```bash
cd /Users/huangzesen/Documents/GitHub/lingtai-kernel && git add -A && git commit -m "refactor: mail intrinsic uses filesystem paths instead of TCP addresses"
```

---

### Task 6: Update Email Capability (Lingtai)

**Files:**
- Modify: `/Users/huangzesen/Documents/GitHub/lingtai/src/lingtai/capabilities/email.py` (lines 547-669, 603-605, 936-1016)
- Test: `tests/test_layers_email.py`

- [ ] **Step 1: Update email capability for filesystem mail**

Key changes:
- `mailbox_rel`: email capability should use `"email"` as its mailbox relative path. When creating the `FilesystemMailService` for email, pass `mailbox_rel="email"`.
- Contact structure: add `agent_id` field alongside `address`, `name`, `note`
- Sender address resolution (line 603-605): `mail_service.address` now returns a path — no changes needed
- Per-recipient dispatch: already uses `_mailman()` which calls `mail_service.send()` — works with new signature
- Remove any TCP-specific address handling (logical prefix stripping, host:port parsing)

- [ ] **Step 2: Update contact add/upsert to include agent_id**

In `_add_contact()` and contact management (lines 936-1016):
```python
{
    "address": "/path/to/agent/workdir",
    "name": "Alice",
    "agent_id": "a1b2c3d4e5f6",
    "note": ""
}
```

- [ ] **Step 3: Run email tests**

```bash
cd /Users/huangzesen/Documents/GitHub/lingtai && source venv/bin/activate && python -m pytest tests/test_layers_email.py -v
```

- [ ] **Step 4: Run all lingtai tests**

```bash
python -m pytest tests/ -v
```

- [ ] **Step 5: Smoke-test**

```bash
python -c "import lingtai; print('OK')"
```

- [ ] **Step 6: Commit**

```bash
cd /Users/huangzesen/Documents/GitHub/lingtai && git add -A && git commit -m "feat: email capability uses filesystem mail paths"
```

---

### Task 7: Rewrite Go Daemon Mail (daemon)

**Files:**
- Rewrite: `/Users/huangzesen/Documents/GitHub/lingtai/daemon/internal/agent/mail.go`
- Modify: `/Users/huangzesen/Documents/GitHub/lingtai/daemon/internal/config/loader.go`
- Modify: `/Users/huangzesen/Documents/GitHub/lingtai/daemon/internal/tui/app.go`
- Modify: `/Users/huangzesen/Documents/GitHub/lingtai/daemon/internal/setup/wizard.go`

- [ ] **Step 1: Rewrite mail.go — filesystem read/write**

Replace `MailClient` and `MailListener` with:

```go
// MailWriter writes messages to an agent's mailbox inbox
type MailWriter struct {
    recipientDir string  // agent working dir
    mailboxRel   string  // "mailbox" or "email"
}

func NewMailWriter(recipientDir, mailboxRel string) *MailWriter

// Send writes a message to the recipient's inbox
// Returns error if handshake fails (no .agent.json, stale heartbeat, agent_id mismatch)
func (w *MailWriter) Send(payload map[string]interface{}) error

// MailPoller polls a mailbox inbox for new messages
type MailPoller struct {
    mailboxDir string        // full path to mailbox dir (e.g., {workdir}/mailbox)
    seen       map[string]bool
    handler    func(map[string]interface{})
    done       chan struct{}
}

func NewMailPoller(mailboxDir string, handler func(map[string]interface{})) *MailPoller
func (p *MailPoller) Start()  // starts polling goroutine
func (p *MailPoller) Stop()   // stops polling
```

Handshake in `Send()`:
1. Read `{recipientDir}/.agent.json` — verify exists
2. Read `{recipientDir}/.agent.heartbeat` — verify timestamp within 2 seconds
3. Write `{recipientDir}/{mailboxRel}/inbox/{uuid}/message.json`

- [ ] **Step 2: Add human working directory creation**

Add a function to create the human's working directory and `.agent.json`:
```go
func SetupHumanWorkdir(baseDir, humanID, humanName, language string) (string, error)
```
Creates `{baseDir}/{humanID}/`, writes `.agent.json` with `admin: null`.

- [ ] **Step 3: Add heartbeat writer for human**

The daemon needs to write `.agent.heartbeat` for the human participant on a 1s tick:
```go
func StartHumanHeartbeat(workdir string, done <-chan struct{})
```

- [ ] **Step 4: Update config/loader.go**

Remove `AgentPort` and `CLIPort` fields from Config struct (no longer needed for mail). Keep `BaseDir` / `ProjectDir` for filesystem paths. Add `HumanID` field if needed.

- [ ] **Step 5: Update tui/app.go**

Replace TCP-based communication:
- Remove `*agent.MailClient` and `*agent.MailListener` fields
- Add `*agent.MailWriter` (writes to agent's inbox) and `*agent.MailPoller` (polls human's inbox)
- `NewChat()`: create MailWriter pointing to agent's working dir, start MailPoller on human's inbox
- Send: `m.writer.Send(payload)` instead of `m.mail.Send(payload)`
- Receive: MailPoller callback delivers to `mailCh` channel (same pattern as before)
- Remove banner handling, port logic

- [ ] **Step 6: Update daemon startup**

In the agent startup flow:
- Wait for agent's `.agent.json` to appear (instead of `WaitForPort`)
- Exchange introductions: write contacts into both human's and agent's `contacts.json`

- [ ] **Step 7: Update wizard.go**

Remove `agent_port` input field from setup wizard. Port configuration is no longer needed for mail.

- [ ] **Step 8: Build and verify**

```bash
cd /Users/huangzesen/Documents/GitHub/lingtai/daemon && go build ./...
```

- [ ] **Step 9: Commit**

```bash
cd /Users/huangzesen/Documents/GitHub/lingtai && git add -A && git commit -m "feat: daemon uses filesystem mail — no more TCP"
```

---

### Task 8: Update Remaining Tests (Lingtai)

**Files:**
- Modify: `/Users/huangzesen/Documents/GitHub/lingtai/tests/test_intrinsics_comm.py`
- Modify: `/Users/huangzesen/Documents/GitHub/lingtai/tests/test_layers_email.py`
- Modify: `/Users/huangzesen/Documents/GitHub/lingtai/tests/test_three_agent_email.py`
- Modify: `/Users/huangzesen/Documents/GitHub/lingtai/tests/test_services_mail.py`
- Delete or rewrite: tests that depend on TCPMailService

- [ ] **Step 1: Update test mocks**

All tests that mock `MailService` need:
- `address` property returns a path string (not `host:port`)
- `send()` signature matches new ABC
- Remove any TCP-specific assertions

- [ ] **Step 2: Update test_services_mail.py**

Replace TCP-based tests with filesystem-based tests, or delete if covered by kernel's `test_filesystem_mail.py`.

- [ ] **Step 3: Update test_three_agent_email.py**

This integration test needs filesystem-based setup instead of TCP ports.

- [ ] **Step 4: Run full test suite**

```bash
cd /Users/huangzesen/Documents/GitHub/lingtai && source venv/bin/activate && python -m pytest tests/ -v
```

- [ ] **Step 5: Smoke-test both packages**

```bash
python -c "import lingtai_kernel; print('kernel OK')"
python -c "import lingtai; print('lingtai OK')"
```

- [ ] **Step 6: Commit**

```bash
cd /Users/huangzesen/Documents/GitHub/lingtai && git add -A && git commit -m "test: update all tests for filesystem mail"
```

---

### Task 9: Final Spec Commit + Cleanup

- [ ] **Step 1: Update spec status to Implemented**

Change `**Status**: Draft` to `**Status**: Implemented` in `docs/superpowers/specs/2026-03-21-filesystem-mail-design.md`.

- [ ] **Step 2: Final full test run across both repos**

```bash
cd /Users/huangzesen/Documents/GitHub/lingtai-kernel && python -m pytest tests/ -v
cd /Users/huangzesen/Documents/GitHub/lingtai && python -m pytest tests/ -v
cd /Users/huangzesen/Documents/GitHub/lingtai/daemon && go build ./...
```

- [ ] **Step 3: Commit**

```bash
cd /Users/huangzesen/Documents/GitHub/lingtai && git add -A && git commit -m "docs: mark filesystem mail spec as implemented"
```
