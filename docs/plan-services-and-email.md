# StoAI — Services Architecture + talk→email Migration Plan

> **Goal:** Wire the 5-service architecture into BaseAgent and rename `talk` to `email` with fire-and-forget semantics over TCP.

**Current state:** Services exist as standalone modules (`services/file_io.py`, `services/email.py`, etc.) with tests, but BaseAgent doesn't accept or use them. BaseAgent still uses direct file I/O in intrinsics and in-process `_connections` dict for `talk`.

**Target state:** BaseAgent takes 5 optional services. Intrinsics delegate to their backing service. `talk` becomes `email` with `EmailService` (TCP by default). Missing service = intrinsics auto-disabled.

---

## Chunk 1: Wire FileIOService into BaseAgent

The most impactful change — 5 intrinsics (read, edit, write, glob, grep) move from direct file ops to `FileIOService`.

### Task 1: Update BaseAgent constructor to accept FileIOService

**Files:** `src/stoai/agent.py`

- [ ] Add `file_io: FileIOService | None = None` parameter to `__init__`
- [ ] Store as `self._file_io`
- [ ] If `file_io` is None and no `disabled_intrinsics` excludes file ops, create a default `LocalFileIOService(root=working_dir)` — preserves backward compat
- [ ] Update `_wire_intrinsics()`: if `self._file_io is None`, skip all file intrinsics

### Task 2: Rewrite file intrinsic handlers to delegate to FileIOService

**Files:** `src/stoai/agent.py`, `src/stoai/intrinsics/read.py`, `edit.py`, `write.py`, `glob.py`, `grep.py`

Current flow: `_make_file_handler()` resolves path, calls standalone `handle_read(args)` which does `Path(path).read_text()`.

New flow: `_make_file_handler()` calls `self._file_io.read(path)`.

- [ ] Change `_wire_intrinsics()` file intrinsics section to create handlers that call `self._file_io.read()`, `self._file_io.edit()`, etc.
- [ ] Remove `_make_file_handler()` — path resolution moves into `LocalFileIOService` (it already has `_resolve()`)
- [ ] Keep the standalone `handle_read`, `handle_edit`, etc. in intrinsics/ as fallback for direct use, but BaseAgent no longer calls them
- [ ] Update `_PARALLEL_SAFE_TOOLS` if needed (file ops through service may have different concurrency characteristics)

### Task 3: Update file intrinsic tests

**Files:** `tests/test_intrinsics_file.py`, `tests/test_agent.py`

- [ ] Update `test_intrinsics_file.py` to test both standalone handlers AND service-backed handlers
- [ ] Add test: BaseAgent with `file_io=LocalFileIOService()` wires file intrinsics
- [ ] Add test: BaseAgent with `file_io=None` auto-disables file intrinsics
- [ ] Add test: BaseAgent with `file_io=None` but `disabled_intrinsics=set()` still has no file intrinsics (no service = no tool)
- [ ] Verify existing 80 tests still pass (backward compat via default LocalFileIOService)

---

## Chunk 2: talk → email rename + EmailService wiring

### Task 4: Rename talk intrinsic to email

**Files:** `src/stoai/intrinsics/talk.py` → `src/stoai/intrinsics/email.py`, `src/stoai/intrinsics/__init__.py`

- [ ] Rename `intrinsics/talk.py` → `intrinsics/email.py`
- [ ] Update SCHEMA:
  - Remove `action` field (no more `send_and_wait` — that's an upper layer)
  - Remove `target_id` — replaced by `address`
  - Add `address` field: `{"type": "string", "description": "Target address (e.g. localhost:8301)"}`
  - Keep `message` field
  - New schema:
    ```python
    SCHEMA = {
        "type": "object",
        "properties": {
            "address": {"type": "string", "description": "Target address (e.g. localhost:8301)"},
            "message": {"type": "string", "description": "Message to send"},
        },
        "required": ["address", "message"],
    }
    ```
- [ ] Update DESCRIPTION: `"Send an email (fire-and-forget message) to another agent at the given address."`
- [ ] Update `intrinsics/__init__.py`: replace `talk` with `email` in `ALL_INTRINSICS`
- [ ] Delete the old `intrinsics/talk.py` file

### Task 5: Update BaseAgent — replace talk with email

**Files:** `src/stoai/agent.py`

- [ ] Add `email: EmailService | None = None` parameter to `__init__`
- [ ] Store as `self._email_service`
- [ ] Remove `self._connections: dict[str, BaseAgent]`
- [ ] Replace `_handle_talk()` with `_handle_email()`:
  ```python
  def _handle_email(self, args: dict) -> dict:
      address = args.get("address", "")
      message_text = args.get("message", "")
      if not address:
          return {"error": "address is required"}
      if self._email_service is None:
          return {"error": "email service not configured"}

      payload = {
          "from": self._email_service.address or self.agent_id,
          "to": address,
          "message": message_text,
      }
      success = self._email_service.send(address, payload)
      if success:
          return {"status": "delivered"}
      else:
          return {"status": "refused", "error": f"Could not deliver to {address}"}
  ```
- [ ] Update `_wire_intrinsics()`: replace `"talk": self._handle_talk` with `"email": self._handle_email`
- [ ] If `self._email_service is None`, skip wiring `email` intrinsic
- [ ] Remove `connect()` and `disconnect()` methods
- [ ] Replace `talk()` public method with `email()` public method:
  ```python
  def email(self, address: str, message: str) -> dict:
      return self._handle_email({"address": address, "message": message})
  ```
- [ ] Start `EmailService.listen()` in `start()` method, passing inbox callback
- [ ] Stop `EmailService` in `stop()` method

### Task 6: Wire inbox to EmailService

**Files:** `src/stoai/agent.py`

The inbox (`self.inbox: queue.Queue`) already exists. When EmailService receives a message, it should put it in the inbox:

- [ ] In `start()`, if `self._email_service` is not None, call:
  ```python
  self._email_service.listen(on_message=self._on_email_received)
  ```
- [ ] Add `_on_email_received(self, payload: dict)`:
  ```python
  def _on_email_received(self, payload: dict) -> None:
      sender = payload.get("from", "unknown")
      content = payload.get("message", "")
      msg = _make_message(MSG_REQUEST, sender, content)
      self.inbox.put(msg)
  ```
- [ ] In `stop()`, call `self._email_service.stop()` if not None

### Task 7: Update tests for email

**Files:** `tests/test_agent.py`, `tests/test_intrinsics_comm.py`

- [ ] Rename all `talk`-related tests to `email`
- [ ] Update `test_connect_agents` → test email via `TCPEmailService` instead
- [ ] Add test: two agents with `TCPEmailService`, agent A emails agent B, B receives in inbox
- [ ] Add test: email without service returns error
- [ ] Add test: email to bad address returns `{"status": "refused"}`
- [ ] Remove `send_and_wait` tests (no longer exists at base level)
- [ ] Update `AgentNotConnectedError` → may want to rename/repurpose or remove

---

## Chunk 3: Wire VisionService and SearchService

### Task 8: Update BaseAgent for VisionService

**Files:** `src/stoai/agent.py`

- [ ] Add `vision: VisionService | None = None` parameter
- [ ] Store as `self._vision_service`
- [ ] Update `_handle_vision()`: delegate to `self._vision_service.analyze_image()` instead of direct LLM multimodal call
- [ ] If `self._vision_service is None`, skip wiring `vision` intrinsic
- [ ] Current fallback: if no VisionService but LLMService exists, auto-create `LLMVisionService(self.service)` — or just disable? (Discuss: breaking change vs convenience)

### Task 9: Update BaseAgent for SearchService

**Files:** `src/stoai/agent.py`

- [ ] Add `search: SearchService | None = None` parameter
- [ ] Store as `self._search_service`
- [ ] Update `_handle_web_search()`: delegate to `self._search_service.search()` instead of direct LLM grounding call
- [ ] If `self._search_service is None`, skip wiring `web_search` intrinsic
- [ ] Same auto-create question as Vision

### Task 10: Update tests for vision and search services

**Files:** `tests/test_agent.py`

- [ ] Add test: agent with `VisionService` has `vision` intrinsic
- [ ] Add test: agent without `VisionService` has no `vision` intrinsic
- [ ] Add test: agent with `SearchService` has `web_search` intrinsic
- [ ] Add test: agent without `SearchService` has no `web_search` intrinsic

---

## Chunk 4: Update exports, docs, cleanup

### Task 11: Update __init__.py and types.py

**Files:** `src/stoai/__init__.py`, `src/stoai/types.py`

- [ ] Remove `AgentNotConnectedError` if no longer needed (or rename to `EmailDeliveryError`)
- [ ] Export `EmailService`, `FileIOService`, `VisionService`, `SearchService` from top-level
- [ ] Verify all layer exports still work

### Task 12: Update design docs

**Files:** `docs/design.md`, `docs/status.md`

- [ ] Mark services wiring as complete in `docs/status.md`
- [ ] Update any remaining references to `talk` in docs

### Task 13: Full test run + smoke test

- [ ] Run all tests: `venv/bin/python -m pytest tests/ -v`
- [ ] Smoke test: `python -c "from stoai import BaseAgent, LocalFileIOService, TCPEmailService; print('ok')"`
- [ ] Verify: `BaseAgent(agent_id="test", service=None)` creates an agent with no intrinsics (no crash)
- [ ] Verify: `BaseAgent(agent_id="test", service=llm, file_io=LocalFileIOService())` has file intrinsics
- [ ] Commit and push

---

## Design Decisions to Make During Implementation

1. **Backward compat for FileIOService**: Should `BaseAgent(service=llm)` (no `file_io=`) auto-create a `LocalFileIOService`? Proposed: **yes**, to avoid breaking existing tests. The "no service = no intrinsic" rule applies only when the user explicitly doesn't pass the service.

2. **Vision/Search auto-creation**: Should `BaseAgent(service=llm)` (no `vision=` or `search=`) auto-create `LLMVisionService(llm)` and `LLMSearchService(llm)`? Current code does vision/search via direct LLM calls. Proposed: **yes for now** (auto-create from LLM), to preserve backward compat. Can be changed to explicit-only later.

3. **`AgentNotConnectedError`**: With `talk` → `email`, this error class doesn't make sense. Options:
   - Remove entirely (email just returns `{"status": "refused"}`)
   - Rename to `EmailDeliveryError`
   - Keep for now, deprecate later
   Proposed: **remove** — email is fire-and-forget, errors are return values not exceptions.

4. **`send_and_wait` pattern**: Currently exists in `_handle_talk`. With email being fire-and-forget, this moves to the `delegate` layer. The `delegate` layer would implement sync patterns like:
   - Send email, poll inbox for reply with matching `conversation_id`
   - Or use a callback pattern
   This is NOT part of this plan — it's delegate layer work.

---

## File Change Summary

| File | Change |
|------|--------|
| `src/stoai/agent.py` | Add 4 service params, rewrite intrinsic wiring, talk→email |
| `src/stoai/intrinsics/talk.py` | **Delete** |
| `src/stoai/intrinsics/email.py` | **Create** (renamed from talk, new schema) |
| `src/stoai/intrinsics/__init__.py` | talk→email in ALL_INTRINSICS |
| `src/stoai/types.py` | Remove AgentNotConnectedError (or rename) |
| `src/stoai/__init__.py` | Update exports |
| `tests/test_agent.py` | Update talk→email tests, add service tests |
| `tests/test_intrinsics_comm.py` | Update talk references |
| `tests/test_intrinsics_file.py` | Add service-backed tests |
| `docs/design.md` | Update references |
| `docs/status.md` | Mark complete |
