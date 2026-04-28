# Runtime Loop

The exact mechanism by which an agent processes messages: the three-layer loop, AED recovery, tool dispatch, state machine, signal consumption, and heartbeat. All line numbers reference `lingtai_kernel/base_agent.py` unless noted.

---

## v1 错处更正

| 位置 | v1 错误 | 更正 |
|------|---------|------|
| Turn Loop 条目 | "molt 由 `.clear` 信号文件触发" | 正确，但缺少另一路径：agent 主动调用 `psyche(object="context", action="molt", summary=...)` 也可触发。`_handle_request()` 中的压力检查是第三个路径。 |
| Turn Loop 条目 | "inbox.get() blocks indefinitely" | 错。正常模式下 `inbox.get(timeout=1.0)` 每 1 秒轮询一次（`base_agent.py:931`）。ASLEEP 模式也用 1 秒 timeout（line 912-916）。 |
| Five States 条目 | "STUCK → AED recovery → ACTIVE" 过于简略 | AED 恢复过程包括：关闭 pending tool calls → 重建 session（保留历史） → 注入 i18n 恢复消息 → 重试 `_handle_message`。最多重试 `max_aed_attempts`（默认 3）次。 |
| Heartbeat 条目 | 隐含心跳只写心跳文件 | 心跳线程实际承担五个职责：生存证明、信号处理中心、体力管理、AED 超时监控、快照调度。 |

---

## Three-Layer Loop Architecture

The agent's main execution runs inside `_run_loop()` (line 900), which is a three-layer nested loop:

```
while True:                          ← outer: process lifecycle (line 902)
  while not self._shutdown.is_set(): ← middle: run cycle (line 903)
    if self._asleep.is_set():        ← sleep branch (line 905)
      block on inbox (timeout=1s)
    else:                            ← normal branch (line 929)
      inbox.get(timeout=1.0)
    
    while True:                      ← inner: AED retry (line 939)
      try:
        _handle_message(msg)
        break                        ← success
      except Exception:              ← failure → AED recovery
        aed_attempts += 1
        ...
  break                              ← shutdown exits outer
```

The loop thread is started in `start()` (line 421) via `threading.Thread(target=self._run_loop, daemon=True)`.

---

## Complete Turn Cycle

A single turn runs from inbox message pickup to idle/sleep return:

```
[pick up message]
  ├─ ASLEEP: blocked on inbox (lines 911-916)
  │   └─ message received → clear _asleep → set ACTIVE → reset uptime (lines 922-928)
  └─ Normal: inbox.get(timeout=1.0) (line 931)
      └─ message received → concat queued → set ACTIVE (lines 934-935)

[concat messages] _concat_queued_messages(msg)
  └─ drain remaining inbox, concatenate with \n\n (lines 1093-1117)

[message processing] _handle_message(msg) → _handle_request(msg)
  ├─ _pre_request(msg)          ← extract content (line 1149)
  ├─ build_meta(self)           ← build metadata prefix (line 1150)
  ├─ molt pressure check        ← context management (lines 1155-1198)
  ├─ _session.send(content)     ← send to LLM (line 1204)
  ├─ _process_response(response)← process response & tool calls (line 1207)
  └─ _post_request(msg, result) ← post-hook (line 1208)

[turn wrap-up]
  ├─ set state to IDLE (line 998)
  ├─ _save_chat_history()       ← persist (line 999)
  └─ check auto-insight interval (lines 1001-1010)
```

### Step-by-step call sequence

| # | Function / Operation | Line | Description |
|---|---------------------|------|-------------|
| 1 | `inbox.get()` | 913/931 | Pick up message |
| 2 | `_concat_queued_messages()` | 927/934 | Merge queued messages |
| 3 | `_set_state(ACTIVE)` | 924/935 | Transition to active |
| 4 | `_reset_uptime()` | 926 | Reset stamina anchor (ASLEEP wake only) |
| 5 | `_handle_message(msg)` | 941 | Enter message processing |
| 6 | `_handle_request(msg)` | 1126 | Request processor |
| 7 | `_pre_request(msg)` | 1149 | Extract message text |
| 8 | `build_meta(self)` | 1150 | Build metadata prefix |
| 9 | Molt pressure check | 1155-1198 | Context pressure management |
| 10 | `render_meta()` | 1200 | Render metadata |
| 11 | `_session.send(content)` | 1204 | Send to LLM |
| 12 | `_process_response()` | 1207 | Process LLM response (tool call loop) |
| 13 | `_post_request()` | 1208 | Subclass post-hook |
| 14 | `_set_state(IDLE)` | 998 | Return to idle |
| 15 | `_save_chat_history()` | 999 | Persist |

---

## Inner Tool Call Loop

Inside `_process_response()` (line 1222), an inner while-True loop handles tool calls:

```
while True:
  if response has text → collect, log as "diary" (line 1234-1236)
  if response has thoughts → log each as "thinking" (line 1241-1242)
  if NO tool calls → break (line 1244-1245)          ← turn ends naturally
  if cancel_event set → clear, return empty (line 1247-1249) ← interrupted
  guard.check_limit() / guard.check_invalid_tool_limit() (lines 1251-1257)
  execute tool calls via ToolExecutor (lines 1260-1265)
  if intercepted → commit results, return intercept text (lines 1267-1274)
  record calls in guard (line 1276)
  repeated-error early break (lines 1279-1288)
  session.send(tool_results) → new response (line 1290)
  ← loop continues
```

### Turn end conditions

The tool call loop (and thus the turn) ends when any of these occurs:

1. **LLM returns no tool calls** — line 1244: `if not response.tool_calls: break`
2. **Cancel event set** — line 1247: returns empty result (triggered by `.interrupt`, `.sleep`, `.suspend`, or stamina expiry)
3. **Loop guard triggers** — `check_limit()` or `check_invalid_tool_limit()` (lines 1251-1257)
4. **Repeated identical errors** — lines 1279-1288
5. **Tool executor intercept** — lines 1267-1274 (e.g., `_on_tool_result_hook` returns non-None)

---

## Tool Dispatch

`_dispatch_tool()` (line 1307) uses a two-layer lookup:

1. **Layer 1**: `self._intrinsics[name]` — built-in intrinsic handlers (line 1315-1316)
2. **Layer 2**: `self._tool_handlers[name]` — capability/MCP handlers (line 1317-1318)
3. **Unknown**: raises `UnknownToolError` (line 1320)

Intrinsics are wired in `_wire_intrinsics()` (line 309) by iterating `ALL_INTRINSICS`.

---

## LoopGuard

`LoopGuard` limits are set in `_get_guard_limits()` (line 1210):

| Parameter | Default | Source | Meaning |
|-----------|---------|--------|---------|
| `max_turns` | 50 | `config.py:14` `config.max_turns` | Maximum tool calls per request |
| Repeated-tool free passes | 2 | Hard-coded | Consecutive identical tool calls allowed before counting |
| Repeated-tool hard blocks | 8 | Hard-coded | Hard limit on consecutive identical tool calls |

The guard is created at `_handle_request()` lines 1132-1137:

```python
guard = LoopGuard(
    max_iterations=limits[0],    # 50
    free_passes=limits[1],       # 2
    hard_block_after=limits[2],  # 8
)
```

---

## Five-State Machine

### State definitions

Defined in `state.py` (lines 8-26):

```python
class AgentState(enum.Enum):
    ACTIVE    = "active"     # Processing a message
    IDLE      = "idle"       # Waiting for next inbox message
    STUCK     = "stuck"      # LLM error; AED retry loop active
    ASLEEP    = "asleep"     # Sleeping (listeners alive, process alive)
    SUSPENDED = "suspended"  # Suspended (process exits)
```

### State transitions

**`_set_state()`** (line 587):
- No-op if old == new (line 590-591)
- Sets `self._state = new_state`
- ACTIVE: clears `_idle` event, cancels soul timer (lines 593-595)
- Non-ACTIVE: sets `_idle` event; if IDLE, starts soul timer (lines 597-599)
- Logs transition (line 600)
- Writes manifest to disk (line 601)

### Transition table

| From | Trigger | To | Line |
|------|---------|----|------|
| IDLE | Inbox message received | ACTIVE | `_run_loop` 935 |
| ASLEEP | Inbox message received | ACTIVE | `_run_loop` 924 |
| ACTIVE | Turn completed successfully | IDLE | `_run_loop` 998 |
| ACTIVE | LLM call throws exception | STUCK | `_run_loop` 968 |
| STUCK | AED attempt (session rebuilt, retry) | ACTIVE | `_run_loop` 940 (retry) |
| STUCK | AED timeout (`aed_timeout` exceeded) | ASLEEP | `_heartbeat_loop` 865 |
| STUCK | `max_aed_attempts` exhausted | ASLEEP | `_run_loop` 964/985 |
| ACTIVE/IDLE/STUCK | `.sleep` signal file | ASLEEP | `_heartbeat_loop` 761 |
| ACTIVE/IDLE/STUCK | Stamina expired | ASLEEP | `_heartbeat_loop` 856 |
| ACTIVE/IDLE/STUCK | `.suspend` signal file | SUSPENDED | `_heartbeat_loop` 749 |
| ACTIVE/IDLE/STUCK | `.refresh` signal file | SUSPENDED | `_heartbeat_loop` 737 |
| IDLE | `start()` (initial state) | IDLE | `__init__` 105/273 |
| SUSPENDED | `lingtai run` (external) | IDLE | External |

---

## Heartbeat Thread

`_heartbeat_loop()` (line 706) runs on a daemon thread, one tick per second (`time.sleep(1.0)` at line 883).

### Responsibilities (in execution order per tick)

1. Write heartbeat timestamp to `.agent.heartbeat` (lines 712-716)
2. Check `.interrupt` → set `_cancel_event` (line 719)
3. Check `.refresh` → rename to `.refresh.taken` → SUSPENDED + shutdown (line 729)
4. Check `.suspend` → delete → SUSPENDED + shutdown (line 742)
5. Check `.sleep` → delete → ASLEEP + `_asleep.set()` (line 755)
6. Check `.prompt` → read content → `self.send()` → delete (line 766)
7. Check `.clear` → read source → `context_forget()` → delete (line 783)
8. Check `.inquiry` → rename to `.inquiry.taken` → spawn inquiry thread (line 806)
9. Check `.rules` → diff against `system/rules.md` (line 848, impl at 1512)
10. Stamina check (line 850)
11. AED timeout check — if STUCK for > `aed_timeout` seconds → ASLEEP (line 859)
12. Periodic git snapshots (line 872)

### Coordination with main loop

- **`_cancel_event`**: heartbeat sets this to break the main loop's tool call cycle (`.interrupt`, `.sleep`, `.suspend`)
- **`_shutdown`**: heartbeat sets this to trigger main loop exit (`.suspend`, `.refresh`)
- **`_asleep`**: heartbeat sets this, main loop detects and enters sleep wait
- **`inbox`**: heartbeat injects messages via `self.send()` (`.prompt`, soul whisper)

---

## AED Error Recovery

When `_handle_message()` throws (line 941), the inner AED loop catches it:

```
exception caught:
  ├─ aed_attempts += 1
  ├─ close pending tool_calls via chat.interface.close_pending_tool_calls() (lines 954-957)
  ├─ if aed_attempts > max_aed_attempts (default 3):
  │   └─ ASLEEP (lines 959-966) — give up
  ├─ set state to STUCK (line 968)
  ├─ rebuild session preserving history: _session._rebuild_session(interface) (lines 989-990)
  ├─ inject recovery message using i18n system.stuck_revive template (lines 993-995)
  └─ retry _handle_message(msg) with new recovery message
```

---

## Stamina Timer

- **Default**: `stamina: float = 3600.0` seconds (1 hour) — `config.py` line 27
- **Anchor**: `_uptime_anchor` (line 109), set via `time.monotonic()` in `start()` (line 435)
- **Reset**: `_reset_uptime()` (line 476) called when waking from ASLEEP (line 926)
- **Check**: every heartbeat tick (lines 851-857):

```python
if self._uptime_anchor is not None and self._state not in (AgentState.ASLEEP, AgentState.SUSPENDED):
    elapsed = time.monotonic() - self._uptime_anchor
    if elapsed >= self._config.stamina:
        self._cancel_event.set()
        self._set_state(AgentState.ASLEEP, reason="stamina expired")
        self._asleep.set()
```

**Key behavior**:
- All non-ASLEEP/SUSPENDED time (including IDLE waiting) consumes stamina
- ASLEEP time does NOT consume stamina (check excludes that state)
- Waking from ASLEEP resets anchor → stamina is fully "restored"
- Override via `init.json` `manifest.stamina`

---

## Mail Wake Mechanism

**`_on_normal_mail()`** (line 525):
1. Extract metadata (sender, subject, message)
2. `_wake_nap("mail_arrived")` — sets `_nap_wake` event (line 545)
3. Truncate preview to 500 chars (lines 547-550)
4. Format notification via i18n template `system.new_mail` (lines 551-555)
5. Create `MSG_REQUEST` message from "system" (line 558)
6. Put into `self.inbox` (line 559)

If agent is ASLEEP: `_run_loop()` is blocked on `inbox.get(timeout=1.0)` (line 913). The new message unblocks it → wake sequence → ACTIVE.

---

## Configuration Parameters

| Parameter | Default | Source | Description |
|-----------|---------|--------|-------------|
| `stamina` | 3600.0s (1h) | `config.py:27` | Max uptime before auto-sleep |
| `max_turns` | 50 | `config.py:14` | Max tool calls per request |
| `aed_timeout` | 360.0s (6min) | `config.py:20` | Max STUCK duration before ASLEEP |
| `max_aed_attempts` | 3 | `config.py:21` | Max AED retry attempts |
| `retry_timeout` | 300.0s (5min) | `config.py:19` | LLM call watchdog timeout |
| `soul_delay` | 120.0s (2min) | `config.py:25` | Idle seconds before soul whisper |
| `_inbox_timeout` | 1.0s | `base_agent.py:77` | Inbox polling timeout |
| Heartbeat interval | 1.0s | `base_agent.py:883` | Heartbeat loop sleep |

---

## Function Index

| Function | Line | Purpose |
|----------|------|---------|
| `_run_loop` | 900 | Main loop entry |
| `_handle_message` | 1123 | Message router |
| `_handle_request` | 1130 | Request processor core |
| `_process_response` | 1222 | Response + tool call loop |
| `_dispatch_tool` | 1307 | Two-layer tool dispatch |
| `_set_state` | 587 | State transition with side effects |
| `_heartbeat_loop` | 706 | 1s tick: signals, stamina, AED, snapshots |
| `_start_heartbeat` | 681 | Start heartbeat thread |
| `_concat_queued_messages` | 1093 | Merge inbox messages |
| `_pre_request` | 1730 | Pre-processing hook |
| `_post_request` | 1737 | Post-processing hook |
| `_save_chat_history` | 1612 | Persist chat history |
| `start` | 421 | Start agent |
| `stop` | 480 | Stop agent |
| `send` | 1581 | Public: put message in inbox |
| `_wake_nap` | 621 | Wake from nap |
| `_reset_uptime` | 476 | Reset stamina anchor |
| `_soul_whisper` | 626 | Soul flow callback |
| `_perform_refresh` | 1014 | .refresh handshake |
| `_get_guard_limits` | 1210 | Return LoopGuard limits |
