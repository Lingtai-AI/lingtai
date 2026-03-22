# Agent Identity Card & Vigil Enforcement — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `.agent.json` the complete identity card (two-way read/write), enforce vigil (finite wakefulness), rename system intrinsic actions to match the project's vibe, and remove the redundant `_soul_flow` boolean.

**Architecture:** Changes span lingtai-kernel (config, base_agent, system intrinsic, soul intrinsic, i18n, tests) and lingtai (agent.py, avatar.py, tests). The kernel changes come first since lingtai depends on them. Each task is independent enough to commit separately.

**Tech Stack:** Python 3.11+, pytest, lingtai-kernel + lingtai packages.

**Spec:** `docs/superpowers/specs/2026-03-21-agent-identity-vigil-design.md`

---

### Task 1: Rename `lifetime` → `vigil` and `flow_delay` → `soul_delay` in AgentConfig

**Files:**
- Modify: `../lingtai-kernel/src/lingtai_kernel/config.py:23-26`

- [ ] **Step 1: Update config.py**

```python
# Remove line 23 (flow: bool = True)
# Rename line 24: flow_delay → soul_delay
# Rename line 26: lifetime → vigil

@dataclass
class AgentConfig:
    """Configuration for a BaseAgent instance.

    The host app reads its own config files and passes resolved values here.
    No file-based config reading inside lingtai.
    """
    max_turns: int = 50
    provider: str | None = None  # None = use LLMService's provider
    model: str | None = None
    api_key: str | None = None
    base_url: str | None = None
    retry_timeout: float = 120.0
    cpr_timeout: float = 1200.0  # 20 minutes — max CPR before pronouncing dead
    thinking_budget: int | None = None
    data_dir: str | None = None  # for cache files (e.g., model context windows)
    soul_delay: float = 120.0  # seconds idle before soul whispers; large value = effectively off
    language: str = "en"  # agent language ("en", "zh", "wen"); controls all kernel-injected strings
    vigil: float = 3600.0  # agent wakefulness budget in seconds; enforced by heartbeat
    ensure_ascii: bool = False  # JSON output: False = readable unicode, True = \uXXXX escapes
```

- [ ] **Step 2: Smoke-test**

Run: `cd ../lingtai-kernel && python -c "from lingtai_kernel.config import AgentConfig; c = AgentConfig(); print(c.vigil, c.soul_delay)"`
Expected: `3600.0 120.0`

- [ ] **Step 3: Commit**

```bash
cd ../lingtai-kernel && git add src/lingtai_kernel/config.py && git commit -m "refactor: rename lifetime→vigil, flow_delay→soul_delay, remove flow in AgentConfig"
```

---

### Task 2: Remove `_soul_flow`, update `_soul_delay` init in BaseAgent

**Files:**
- Modify: `../lingtai-kernel/src/lingtai_kernel/base_agent.py:215-219,491-494`

- [ ] **Step 1: Update soul initialization (lines 215-219)**

Replace:
```python
        # Soul — inner voice
        # Flow mode: locked at creation via config.flow; agent cannot toggle.
        # Inquiry: on-demand one-shot, independent of flow.
        self._soul_flow = self._config.flow
        self._soul_delay = max(1.0, self._config.flow_delay)
```
With:
```python
        # Soul — inner voice
        # soul_delay controls idle whisper timing. Very large = effectively off.
        # Inquiry: on-demand one-shot, independent of flow.
        self._soul_delay = max(1.0, self._config.soul_delay)
```

- [ ] **Step 2: Update `_start_soul_timer` (lines 491-494)**

Replace:
```python
    def _start_soul_timer(self) -> None:
        """Start the soul delay timer if flow is enabled or inquiry is pending."""
        if not self._soul_flow and not self._soul_oneshot:
            return
```
With:
```python
    def _start_soul_timer(self) -> None:
        """Start the soul delay timer for flow or pending inquiry."""
        if not self._soul_oneshot and self._soul_delay > self._config.vigil:
            return  # delay exceeds vigil — effectively disabled
```

- [ ] **Step 3: Smoke-test**

Run: `cd ../lingtai-kernel && python -c "import lingtai_kernel"`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
cd ../lingtai-kernel && git add src/lingtai_kernel/base_agent.py && git commit -m "refactor: remove _soul_flow, use soul_delay as sole flow control"
```

---

### Task 3: Add `vigil` and `soul_delay` to manifest, persist `soul_delay` on change

**Files:**
- Modify: `../lingtai-kernel/src/lingtai_kernel/base_agent.py:1023-1040`
- Modify: `../lingtai-kernel/src/lingtai_kernel/intrinsics/soul.py:72-74`

- [ ] **Step 1: Update `_build_manifest()` (line 1023)**

Replace:
```python
    def _build_manifest(self) -> dict:
        """Build the manifest dict for .agent.json.

        Subclasses override to add fields (e.g. capabilities).
        Contains identity + construction recipe — enough to respawn
        an agent with the same abilities.
        """
        data = {
            "agent_id": self.agent_id,
            "agent_name": self.agent_name,
            "started_at": self._started_at,
            "working_dir": str(self._working_dir),
            "admin": self._admin,
            "language": self._config.language,
        }
        if self._mail_service is not None and self._mail_service.address:
            data["address"] = self._mail_service.address
        return data
```
With:
```python
    def _build_manifest(self) -> dict:
        """Build the manifest dict for .agent.json.

        Subclasses override to add fields (e.g. capabilities).
        Contains identity + construction recipe — everything the agent
        knows about itself, enough to respawn with the same abilities.
        """
        data = {
            "agent_id": self.agent_id,
            "agent_name": self.agent_name,
            "started_at": self._started_at,
            "working_dir": str(self._working_dir),
            "admin": self._admin,
            "language": self._config.language,
            "vigil": self._config.vigil,
            "soul_delay": self._soul_delay,
        }
        if self._mail_service is not None and self._mail_service.address:
            data["address"] = self._mail_service.address
        return data
```

- [ ] **Step 2: Persist soul_delay on change in soul intrinsic (line 72-74)**

Replace:
```python
        old = agent._soul_delay
        agent._soul_delay = delay
        agent._log("soul_delay", old=old, new=delay)
```
With:
```python
        old = agent._soul_delay
        agent._soul_delay = delay
        agent._log("soul_delay", old=old, new=delay)
        # Persist to .agent.json
        agent._workdir.write_manifest(agent._build_manifest())
```

- [ ] **Step 3: Update soul.py module docstring (lines 1-9)**

Replace:
```python
"""Soul intrinsic — the agent's inner voice.

Actions:
    inquiry — one-shot self-directed question, fires once on next idle
    delay   — adjust the idle delay before the soul whispers

Flow mode (continuous free reflection) is enabled at agent creation
via config.flow and cannot be toggled at runtime.
Inquiry works regardless of flow — it fires once on the next idle.
"""
```
With:
```python
"""Soul intrinsic — the agent's inner voice.

Actions:
    inquiry — one-shot self-directed question, fires once on next idle
    delay   — adjust the idle delay before the soul whispers

soul_delay controls flow timing. Very large delay (> vigil) = effectively off.
Inquiry works regardless of delay — it fires once on the next idle.
"""
```

- [ ] **Step 4: Smoke-test**

Run: `cd ../lingtai-kernel && python -c "import lingtai_kernel"`
Expected: no errors

- [ ] **Step 5: Commit**

```bash
cd ../lingtai-kernel && git add src/lingtai_kernel/base_agent.py src/lingtai_kernel/intrinsics/soul.py && git commit -m "feat: add vigil and soul_delay to manifest, persist soul_delay on change"
```

---

### Task 4: Enforce vigil in heartbeat loop

**Files:**
- Modify: `../lingtai-kernel/src/lingtai_kernel/base_agent.py:570-626`

- [ ] **Step 1: Add vigil check after signal file detection (after line 602)**

Insert after the quell signal file block and before the STUCK check (line 604):

```python
            # Vigil enforcement — self-quell when vigil expires
            if self._uptime_anchor is not None and self._state != AgentState.DORMANT:
                elapsed = time.monotonic() - self._uptime_anchor
                if elapsed >= self._config.vigil:
                    self._log("vigil_expired", elapsed=round(elapsed, 1), vigil=self._config.vigil)
                    self._cancel_event.set()
                    self._set_state(AgentState.DORMANT, reason="vigil expired")
                    self._shutdown.set()
```

- [ ] **Step 2: Update `status()` method (lines 1183-1201)**

Replace `lifetime` → `vigil` and `life_left` → `vigil_left`:
```python
    def status(self) -> dict:
        """Return agent status for monitoring."""
        vigil_left = None
        if self._uptime_anchor is not None:
            elapsed = time.monotonic() - self._uptime_anchor
            remaining = max(0.0, self._config.vigil - elapsed)
            vigil_left = round(remaining, 1)
        return {
            "agent_id": self.agent_id,
            "agent_name": self.agent_name,
            "agent_type": self.agent_type,
            "state": self._state.value,
            "idle": self.is_idle,
            "heartbeat": self._heartbeat,
            "queue_depth": self.inbox.qsize(),
            "vigil": self._config.vigil,
            "vigil_left": vigil_left,
            "tokens": self.get_token_usage(),
        }
```

- [ ] **Step 3: Smoke-test**

Run: `cd ../lingtai-kernel && python -c "import lingtai_kernel"`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
cd ../lingtai-kernel && git add src/lingtai_kernel/base_agent.py && git commit -m "feat: enforce vigil — heartbeat self-quells when vigil expires"
```

---

### Task 5: Rename system intrinsic actions

**Files:**
- Modify: `../lingtai-kernel/src/lingtai_kernel/intrinsics/system.py` (full rewrite of action dispatch)

- [ ] **Step 1: Rewrite system.py**

Update the module docstring:
```python
"""System intrinsic — runtime, lifecycle, and synchronization.

Actions:
    show      — display agent identity, runtime, and resource usage
    nap       — pause execution; wakes on incoming message or timeout
    refresh   — reload MCP servers and reset session (same identity)
    quell     — go dormant (self or other agent via karma)
    revive    — wake a dormant agent (requires karma)
    interrupt — interrupt another agent's work (requires karma)
    nirvana   — permanently destroy an agent (requires nirvana)
"""
```

Update `get_schema()` enum (line 26):
```python
"enum": ["show", "nap", "refresh", "quell", "revive", "interrupt", "nirvana"],
```

Update handler dispatch map (lines 54-63):
```python
    handler = {
        "show": _show,
        "nap": _nap,
        "refresh": _refresh,
        "quell": _quell,
        "revive": _revive,
        "interrupt": _interrupt,
        "nirvana": _nirvana,
    }.get(action)
```

Update `_show()` — rename `lifetime` → `vigil` (lines 79, 107-108):
```python
    vigil_left = max(0.0, agent._config.vigil - uptime) if agent._uptime_anchor is not None else None
    # ...
        "runtime": {
            "current_time": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
            "started_at": agent._started_at,
            "uptime_seconds": round(uptime, 1),
            "vigil": agent._config.vigil,
            "vigil_left": round(vigil_left, 1) if vigil_left is not None else None,
        },
```

Rename `_sleep` → `_nap` (line 133). Update log events from `system_sleep_*` → `system_nap_*`. Update `"silenced"` reason → `"interrupted"`:
```python
def _nap(agent, args: dict) -> dict:
    max_wait = 300
    seconds = args.get("seconds")
    if seconds is not None:
        seconds = float(seconds)
        if seconds < 0:
            return {"status": "error", "message": "seconds must be non-negative"}
        seconds = min(seconds, max_wait)

    agent._log("system_nap_start", seconds=seconds)

    if agent._cancel_event.is_set():
        agent._log("system_nap_end", reason="interrupted", waited=0.0)
        return {"status": "ok", "reason": "interrupted", "waited": 0.0}
    if agent._mail_arrived.is_set():
        agent._log("system_nap_end", reason="mail_arrived", waited=0.0)
        return {"status": "ok", "reason": "mail_arrived", "waited": 0.0}

    agent._mail_arrived.clear()

    poll_interval = 0.5
    t0 = time.monotonic()

    while True:
        waited = time.monotonic() - t0

        if agent._cancel_event.is_set():
            agent._log("system_nap_end", reason="interrupted", waited=waited)
            return {"status": "ok", "reason": "interrupted", "waited": waited}

        if agent._mail_arrived.is_set():
            agent._log("system_nap_end", reason="mail_arrived", waited=waited)
            return {"status": "ok", "reason": "mail_arrived", "waited": waited}

        if seconds is not None and waited >= seconds:
            agent._log("system_nap_end", reason="timeout", waited=waited)
            return {"status": "ok", "reason": "timeout", "waited": waited}

        if seconds is not None:
            remaining = seconds - waited
            sleep_time = min(poll_interval, remaining)
        else:
            sleep_time = poll_interval

        agent._mail_arrived.wait(timeout=sleep_time)
```

Remove `_shutdown`, replace with self-quell in `_quell`. Remove `_restart`, replace with `_refresh`:

```python
def _refresh(agent, args: dict) -> dict:
    from ..i18n import t
    reason = args.get("reason", "")
    agent._log("refresh_requested", reason=reason)
    agent._refresh_requested = True
    agent._shutdown.set()
    return {
        "status": "ok",
        "message": t(agent._config.language, "system_tool.refresh_message"),
    }
```

Rewrite `_quell` to handle both self and other:
```python
def _quell(agent, args: dict) -> dict:
    from ..i18n import t
    address = args.get("address")
    if not address:
        # Self-quell
        reason = args.get("reason", "")
        agent._log("self_quell", reason=reason)
        agent._set_state(AgentState.DORMANT, reason="self-quell")
        agent._shutdown.set()
        return {
            "status": "ok",
            "message": t(agent._config.language, "system_tool.quell_message"),
        }
    # Quell other — karma-gated
    from pathlib import Path
    from ..handshake import is_alive
    err = _check_karma_gate(agent, "quell", args)
    if err:
        return err
    if not is_alive(address):
        return {"error": True, "message": f"Agent at {address} is not running — already dormant?"}
    (Path(address) / ".quell").write_text("")
    agent._log("karma_quell", target=address)
    return {"status": "quelled", "address": address}
```

Update `_check_karma_gate` — remove the self-action rejection for quell (line 228-229), update action sets:
```python
_KARMA_ACTIONS = {"interrupt", "quell", "revive"}
_NIRVANA_ACTIONS = {"nirvana"}


def _check_karma_gate(agent, action: str, args: dict) -> dict | None:
    from ..handshake import is_agent
    if action in _KARMA_ACTIONS and not agent._admin.get("karma"):
        return {"error": True, "message": f"Not authorized for {action} (requires admin.karma=True)"}
    if action in _NIRVANA_ACTIONS and not (agent._admin.get("karma") and agent._admin.get("nirvana")):
        return {"error": True, "message": f"Not authorized for {action} (requires admin.nirvana=True)"}
    address = args.get("address")
    if not address:
        return {"error": True, "message": f"{action} requires an address"}
    if str(agent._working_dir) == str(address):
        return {"error": True, "message": f"Cannot {action} self"}
    if not is_agent(address):
        return {"error": True, "message": f"No agent at {address}"}
    return None
```

Rename `_silence` → `_interrupt`, update signal file `.silence` → `.interrupt`:
```python
def _interrupt(agent, args: dict) -> dict:
    from pathlib import Path
    from ..handshake import is_alive
    err = _check_karma_gate(agent, "interrupt", args)
    if err:
        return err
    address = args["address"]
    if not is_alive(address):
        return {"error": True, "message": f"Agent at {address} is not running"}
    (Path(address) / ".interrupt").write_text("")
    agent._log("karma_interrupt", target=address)
    return {"status": "interrupted", "address": address}
```

Rename `_annihilate` → `_nirvana`:
```python
def _nirvana(agent, args: dict) -> dict:
    import shutil
    from pathlib import Path
    from ..handshake import is_alive
    err = _check_karma_gate(agent, "nirvana", args)
    if err:
        return err
    address = args["address"]
    if is_alive(address):
        (Path(address) / ".quell").write_text("")
        import time as _time
        deadline = _time.time() + 10.0
        while _time.time() < deadline:
            if not is_alive(address):
                break
            _time.sleep(0.5)
        else:
            if is_alive(address):
                return {"error": True, "message": f"Agent at {address} did not quell within timeout"}
    shutil.rmtree(address)
    agent._log("karma_nirvana", target=address)
    return {"status": "nirvana", "address": address}
```

- [ ] **Step 2: Update signal file detection in base_agent.py heartbeat (lines 583-602)**

Rename `.silence` → `.interrupt`:
```python
            # --- signal file detection ---
            interrupt_file = self._working_dir / ".interrupt"
            if interrupt_file.is_file():
                try:
                    interrupt_file.unlink()
                except OSError:
                    pass
                self._cancel_event.set()
                self._log("interrupt_received", source="signal_file")
```

- [ ] **Step 3: Rename `_restart_requested` → `_refresh_requested` and `_perform_restart` → `_perform_refresh` in base_agent.py**

At line 691-696:
```python
            # Check for refresh before exiting
            if getattr(self, "_refresh_requested", False):
                self._refresh_requested = False
                self._perform_refresh()
                self._shutdown.clear()
                continue  # re-enter the message loop
```

Rename method at line 699:
```python
    def _perform_refresh(self) -> None:
        """Refresh: close old MCP clients, reload from working dir, reset session."""
        self._log("refresh_start")
        # ... (rest of body unchanged, just rename log events)
        self._log("refresh_complete", tools=list(self._mcp_handlers.keys()))
```

- [ ] **Step 4: Update base_agent.py comment at line 439**

Replace `"Lifecycle control (silence, quell, revive, annihilate)"` → `"Lifecycle control (interrupt, quell, revive, nirvana)"`

- [ ] **Step 5: Update state.py docstring (line 16)**

Replace:
```python
    ACTIVE/IDLE --(quell/shutdown)-> DORMANT
```
With:
```python
    ACTIVE/IDLE --(quell)---------> DORMANT
```

- [ ] **Step 6: Update soul.py `whisper()` docstring (line 84)**

Replace `"Flow mode: free reflection."` → `"Continuous mode: free reflection."`

- [ ] **Step 7: Smoke-test**

Run: `cd ../lingtai-kernel && python -c "import lingtai_kernel"`
Expected: no errors

- [ ] **Step 8: Commit**

```bash
cd ../lingtai-kernel && git add src/lingtai_kernel/intrinsics/system.py src/lingtai_kernel/base_agent.py src/lingtai_kernel/state.py && git commit -m "refactor: rename system actions — nap, refresh, quell, interrupt, nirvana"
```

---

### Task 6: Update kernel i18n (en.json, zh.json, wen.json)

**Files:**
- Modify: `../lingtai-kernel/src/lingtai_kernel/i18n/en.json`
- Modify: `../lingtai-kernel/src/lingtai_kernel/i18n/zh.json`
- Modify: `../lingtai-kernel/src/lingtai_kernel/i18n/wen.json`

- [ ] **Step 1: Update en.json**

Key changes:
- `system_tool.description`: Update action list — remove shutdown/restart/sleep/silence/annihilate, add nap/refresh/interrupt/nirvana. Quell now includes self-quell.
- `system_tool.action_description`: Rewrite all action descriptions for new names.
- Remove `system_tool.shutdown_message` and `system_tool.restart_message`
- Add new keys:
  - `"system_tool.quell_message": "Going dormant. Your identity and memory are preserved. You may be revived."`
  - `"system_tool.refresh_message": "Refreshing — reloading tools and resetting session. Your identity and memory are preserved."`
- `system_tool.seconds_description`: "For nap:" instead of "For sleep:"
- Update `lifetime` → `vigil` in action descriptions where referenced
- Rename `system_tool.reason_description`: "Reason for quell or refresh" instead of "shutdown or restart"

- [ ] **Step 2: Update zh.json**

Same key changes as en.json, using zh vocabulary:
- nap → 小憩, refresh → 沐浴, quell → 沉睡, interrupt → 打断, nirvana → 涅槃, vigil → 精力
- `"system_tool.quell_message": "进入沉睡。身份与记忆已保存。可被唤醒。"`
- `"system_tool.refresh_message": "沐浴中——重新加载工具并重置会话。身份与记忆已保存。"`

- [ ] **Step 3: Update wen.json**

Same key changes, using wen vocabulary:
- nap → 小憩, refresh → 更衣, quell → 沉寐, interrupt → 打断, nirvana → 涅槃, vigil → 精力
- `"system_tool.quell_message": "沉寐已入。身与忆俱在。可待唤醒。"`
- `"system_tool.refresh_message": "更衣中——重载器用，重置会话。身与忆俱在。"`

- [ ] **Step 4: Smoke-test**

Run: `cd ../lingtai-kernel && python -c "from lingtai_kernel.i18n import t; print(t('en', 'system_tool.quell_message'))"`
Expected: should print the quell message, not the key

- [ ] **Step 5: Commit**

```bash
cd ../lingtai-kernel && git add src/lingtai_kernel/i18n/ && git commit -m "i18n: update system intrinsic strings for action renames and vigil"
```

---

### Task 7: Update kernel tests

**Files:**
- Modify: `../lingtai-kernel/tests/test_soul.py`
- Tests in lingtai that test kernel intrinsics will be updated in Task 9.

- [ ] **Step 1: Update test_soul.py**

Remove all `_soul_flow` references:
- Line 9: `agent._soul_flow = True` → remove
- Line 72: `agent._soul_flow = False` → update test to use large delay instead
- Line 201: `assert agent._soul_flow is True` → remove
- Line 217: `assert agent._soul_flow is False` → remove
- Lines 219+: `test_soul_timer_starts_on_idle_when_flow_enabled` → update name/logic
- Line 369: `assert agent._soul_flow is True` → remove

Rewrite `test_soul_attributes_initialized_flow_off` (lines 207-217) — this test uses `AgentConfig(flow=False)`. Rewrite as a test for large `soul_delay` disabling flow (soul_delay > vigil means timer won't start).

Rename `flow_delay` references:
- Lines 279-298: `flow_delay` → `soul_delay` in AgentConfig construction

- [ ] **Step 2: Run kernel tests**

Run: `cd ../lingtai-kernel && python -m pytest tests/test_soul.py -v`
Expected: all pass

- [ ] **Step 3: Commit**

```bash
cd ../lingtai-kernel && git add tests/test_soul.py && git commit -m "test: update soul tests for _soul_flow removal and soul_delay rename"
```

---

### Task 8: Update lingtai agent.py — manifest, revive, combo

**Files:**
- Modify: `src/lingtai/agent.py:60-75,135-141,212-250`

- [ ] **Step 1: Add `combo` to manifest (line 135-141)**

```python
    def _build_manifest(self) -> dict:
        """Extend kernel manifest with capabilities and combo."""
        data = super()._build_manifest()
        caps = getattr(self, "_capabilities", None)
        if caps:
            data["capabilities"] = caps
        combo = getattr(self, "_combo_name", None)
        if combo:
            data["combo"] = combo
        return data
```

- [ ] **Step 2: Store combo name at construction**

In `Agent.__init__`, add `combo_name` as an explicit parameter (alongside `capabilities` and `addons`). It must be consumed **before** calling `super().__init__()`, otherwise `BaseAgent` will reject the unknown kwarg.

```python
    def __init__(self, *args, combo_name: str | None = None, **kwargs):
        self._combo_name = combo_name
        super().__init__(*args, **kwargs)
```

Note: check the actual `Agent.__init__` signature — it may need to be integrated into the existing parameter list rather than using `*args, **kwargs`.

- [ ] **Step 3: Fix `_revive_agent()` to read full `.agent.json` (lines 212-250)**

```python
    def _revive_agent(self, address: str) -> "Agent | None":
        """Reconstruct and start a dormant agent from its working dir."""
        import json
        from lingtai_kernel.handshake import is_agent, manifest
        from lingtai_kernel.config import AgentConfig

        target = Path(address)
        if not is_agent(target):
            return None

        # Read persisted identity
        agent_meta = manifest(target)

        # Resolve LLM config from combo or llm.json
        combo_name = agent_meta.get("combo")
        llm_config = None

        if combo_name:
            # Try ~/.lingtai/combos/{combo}.json first, then working dir
            combo_path = Path.home() / ".lingtai" / "combos" / f"{combo_name}.json"
            if not combo_path.is_file():
                combo_path = target / "combo.json"
            if combo_path.is_file():
                combo_data = json.loads(combo_path.read_text())
                model_cfg = combo_data.get("model", {})
                llm_config = {
                    "provider": model_cfg.get("provider"),
                    "model": model_cfg.get("model"),
                    "base_url": model_cfg.get("base_url"),
                }
                # Set env vars from combo
                for key, val in combo_data.get("env", {}).items():
                    if val:
                        import os
                        os.environ.setdefault(key, val)

        if llm_config is None:
            llm_path = target / "system" / "llm.json"
            if not llm_path.is_file():
                return None
            llm_config = json.loads(llm_path.read_text())

        # Reconstruct LLMService
        svc = LLMService(
            provider=llm_config["provider"],
            model=llm_config["model"],
            base_url=llm_config.get("base_url"),
        )

        # Reconstruct capabilities from manifest
        caps_raw = agent_meta.get("capabilities")
        capabilities = None
        if caps_raw:
            capabilities = {name: kw for name, kw in caps_raw}

        # Reconstruct config with persisted values
        revived_config = AgentConfig(
            provider=llm_config["provider"],
            model=llm_config["model"],
            vigil=agent_meta.get("vigil", 3600.0),
            soul_delay=agent_meta.get("soul_delay", 120.0),
            language=agent_meta.get("language", "en"),
        )

        # Reconstruct Agent
        revived = Agent(
            svc,
            agent_name=agent_meta.get("agent_name"),
            agent_id=agent_meta.get("agent_id"),
            base_dir=str(target.parent),
            capabilities=capabilities,
            admin=agent_meta.get("admin", {}),
            config=revived_config,
            combo_name=combo_name,
        )
        revived.start()
        return revived
```

- [ ] **Step 4: Smoke-test**

Run: `source venv/bin/activate && python -c "import lingtai"`
Expected: no errors

- [ ] **Step 5: Commit**

```bash
git add src/lingtai/agent.py && git commit -m "feat: fix revive to read full .agent.json, add combo to manifest"
```

---

### Task 9: Update lingtai tests

**Files:**
- Modify: `tests/test_system.py`
- Modify: `tests/test_karma.py`

- [ ] **Step 1: Update test_system.py**

Rename all action names:
- `"sleep"` → `"nap"` (lines 131, 150, 170, 182, 197, 208)
- `"shutdown"` → `"quell"` (line 219) — self-quell, no address
- `"restart"` → `"refresh"` (line 233)
- `_restart_requested` → `_refresh_requested` (line 236)
- `"Shutdown initiated"` → update assertion to match new quell message
- `"Restart initiated"` → update assertion to match new refresh message
- `"silenced"` reason → `"interrupted"` (lines 201)
- Section headers: update comments
- Test function names: `test_system_sleep_*` → `test_system_nap_*`, `test_system_shutdown` → `test_system_self_quell`, `test_system_restart` → `test_system_refresh`

- [ ] **Step 2: Update test_karma.py**

Rename action names:
- `"silence"` → `"interrupt"` (lines 58, 71, 104)
- `"annihilate"` → `"nirvana"` (lines 112, 124, 131)
- `".silence"` → `".interrupt"` (lines 31, 36, 73)
- `"silenced"` status → `"interrupted"` (line 72)
- `"annihilated"` status → `"nirvana"` (line 125)
- Test function names: `test_silence_*` → `test_interrupt_*`, `test_annihilate_*` → `test_nirvana_*`
- Remove `test_silence_self_rejected` and `test_annihilate_self_rejected` — quell handles self now, interrupt/nirvana still reject self

- [ ] **Step 3: Run all tests**

Run: `source venv/bin/activate && python -m pytest tests/test_system.py tests/test_karma.py -v`
Expected: all pass

- [ ] **Step 4: Commit**

```bash
git add tests/test_system.py tests/test_karma.py && git commit -m "test: update system and karma tests for action renames"
```

---

### Task 10: Update lingtai avatar.py and app references

**Files:**
- Modify: `src/lingtai/capabilities/avatar.py:9,207-215`
- Modify: `app/__init__.py:234-235` (if exists)
- Modify: `examples/contemplate.py:60` (if exists)

- [ ] **Step 1: Update avatar.py docstring (line 9)**

Replace `"silence, quell, revive, annihilate"` → `"interrupt, quell, revive, nirvana"`

- [ ] **Step 2: Update avatar.py AgentConfig construction (lines 207-215)**

Rename `flow_delay` usage if present. Verify `language` is passed correctly.

- [ ] **Step 3: Update avatar.py to pass combo_name**

Wherever the avatar spawns an Agent, pass the `combo_name` parameter.

- [ ] **Step 4: Update app/__init__.py**

Rename `lifetime=` → `vigil=` and `flow_delay=` → `soul_delay=` (lines 234-235).

- [ ] **Step 5: Update examples if they reference old config names**

Check `examples/contemplate.py:60` — rename `flow_delay=` → `soul_delay=`.
Check `app/web/examples/simple.py:37` and `app/web/examples/orchestrator.py:87` — rename `flow_delay=` → `soul_delay=`.

- [ ] **Step 6: Smoke-test**

Run: `source venv/bin/activate && python -c "import lingtai"`
Expected: no errors

- [ ] **Step 7: Commit**

```bash
git add src/lingtai/capabilities/avatar.py app/ examples/ && git commit -m "refactor: update avatar, app, and examples for config renames"
```

---

### Task 11: Update lingtai i18n (wen.json for capability strings)

**Files:**
- Modify: `src/lingtai/i18n/en.json`
- Modify: `src/lingtai/i18n/zh.json`
- Modify: `src/lingtai/i18n/wen.json`

- [ ] **Step 1: Update capability i18n strings that reference old action names**

Search for "shutdown", "restart", "sleep", "silence", "annihilate", "lifetime" in all three capability i18n files and update to new terms. Key areas:
- `avatar.description` and related keys — may reference lifecycle actions
- `email.description` — may reference sleep/shutdown

- [ ] **Step 2: Run i18n tests**

Run: `source venv/bin/activate && python -m pytest tests/test_i18n.py -v`
Expected: all pass

- [ ] **Step 3: Commit**

```bash
git add src/lingtai/i18n/ && git commit -m "i18n: update capability strings for action renames"
```

---

### Task 12: Final integration test

- [ ] **Step 1: Run full kernel test suite**

Run: `cd ../lingtai-kernel && python -m pytest tests/ -v`
Expected: all pass

- [ ] **Step 2: Run full lingtai test suite**

Run: `source venv/bin/activate && python -m pytest tests/ -v`
Expected: all pass

- [ ] **Step 3: Smoke-test both packages**

Run: `source venv/bin/activate && python -c "import lingtai_kernel; import lingtai; print('ok')"`
Expected: `ok`

- [ ] **Step 4: Update CLAUDE.md if needed**

Check if CLAUDE.md references `lifetime`, `shutdown`, `restart`, `sleep`, `silence`, `annihilate`, `flow`, `flow_delay` and update to new terms.

- [ ] **Step 5: Commit any remaining changes**

```bash
git add CLAUDE.md && git commit -m "docs: update CLAUDE.md for vigil and action renames"
```
