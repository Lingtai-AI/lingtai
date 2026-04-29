# Molt Protocol

The context-reset ritual: thresholds, warning ladder, execution steps, and what survives. All line numbers reference `lingtai_kernel/base_agent.py` and `lingtai_kernel/intrinsics/eigen.py` unless noted.

---

## v1 错处更正

| 位置 | v1 错误 | 更正 |
|------|---------|------|
| Molt 触发 | "上下文压力超过 hard ceiling (95%)" | 正确，但触发检查在 `_handle_request()` lines 1155-1198，不在心跳中。心跳中的 `.clear` 信号是另一条独立路径。 |
| Molt 触发 | 阈值写为 0.8 | `config.py:31` 默认 `molt_pressure = 0.7`（70%）。0.8 可能在 init.json 中被覆盖。|
| Warning ladder | "默认 3 次 80%+ 警告后仍未凝蜕" | 错。默认 `molt_warnings = 5`（`config.py:32`），阈值 70%。警告在 70% 以上每轮 +1，累计 5 次后自动凝蜕。 |
| 警告等级 | 仅两级描述 | 实际分三级：Level 1（温和提醒）、Level 2（升级 + 注入凝蜕操作指引）、Level 3（紧急 —— "下一轮触发强制清除"）。等级 = `min(warnings, 3)`。 |
| 工具调用 | "eigen(context, forget, ...)" | 正确的工具调用是 `psyche(object="context", action="molt", summary=...)`。`forget` 不出现在当前工具描述中。 |
| 灵魂会话 | "Soul mirror session state 被清除" | 错。灵魂会话本身跨凝蜕保留（`soul.py` `reset_soul_session()` line 319 只重置读取游标，不清除会话）。 |
| 聊天历史 | "current file is cleared" | 更准确：`chat_history.jsonl` 追加到 `chat_history_archive.jsonl`，然后当前文件被删除（lines 151-161），不是清空。 |
| Summary 注入 | 未说明格式 | Summary 以 `[Carried forward]\n{summary}` 格式作为新会话的第一条 user message 注入（`eigen.py` lines 179-181）。 |

---

## Molt Triggers

Four paths trigger a molt:

| Trigger | Mechanism | Location |
|---------|-----------|----------|
| **Hard ceiling** | `pressure >= molt_hard_ceiling` (0.95) — unconditional, immediate | `_handle_request()` line 1161-1167 |
| **Warning ladder exhaustion** | `pressure >= molt_pressure` (0.70) with 5 warnings accumulated | `_handle_request()` line 1168-1180 |
| **Explicit tool call** | Agent calls `psyche(object=context, action=molt, summary=...)` | `eigen.py` `_context_molt()` line 124 |
| **External signal** | `.clear` file dropped in working directory | `_heartbeat_loop()` line 783-800 |

A fifth historical path (AED exhaustion → force-molt) has been removed. AED exhaustion now leads to ASLEEP instead (comment at line 974-981).

---

## Context Pressure Calculation

Context pressure is calculated by `SessionManager.get_context_pressure()` (`session.py` line 273):

```python
tokens / ctx_window  # where ctx_window = config.context_limit or model default
```

This is called once per request in `_handle_request()` (line 1155).

---

## Exact Thresholds

| Threshold | Default | Source | Behavior |
|-----------|---------|--------|----------|
| `molt_pressure` | **0.70** (70%) | `config.py:31` | Warning ladder begins; each turn above this adds 1 warning |
| `molt_warnings` | **5** | `config.py:32` | Number of warnings before auto-forget |
| `molt_hard_ceiling` | **0.95** (95%) | `config.py:33` | Unconditional force-wipe, ignoring warning count |

---

## Warning Ladder Implementation

**Location**: `_handle_request()` lines 1152-1198

### Flow

```
_handle_request(msg):
  pressure = session.get_context_pressure()
  
  if pressure >= 0.95:                    ← hard ceiling (line 1161)
    context_forget(source="hard_ceiling") ← immediate, no warnings
    return
  
  if pressure >= 0.70:                    ← soft threshold (line 1168)
    _compaction_warnings += 1
    
    if _compaction_warnings > 5:          ← exhausted (line 1174)
      context_forget(source="warning_ladder")
      return
    
    # Otherwise: generate level-based warning
    level = min(warnings, 3)              ← clamped to 3 (line 1186)
    generate warning text based on level
    if level >= 2: append molt procedure  ← inject instructions (lines 1193-1194)
    if config.molt_prompt: override text  ← custom text replaces default (line 1196)
    prepend warning to user message       ← (line 1198)
    continue processing
```

### Three Warning Levels

| Warning # | Level | Text Template | Extra |
|-----------|-------|--------------|-------|
| 1 | Level 1 | "Context at {pressure}. {remaining} turn(s) before auto-wipe." | Gentle reminder |
| 2 | Level 2 | Escalated text | + molt procedure instructions appended |
| 3-5 | Level 3 | "URGENT — Next turn triggers forced wipe. Molt now." | + molt procedure instructions appended |

- Level is `min(warnings, 3)` (line 1186) — clamped to 3
- Level 2+ appends `system.molt_procedure` (the molt recipe text) (lines 1193-1194)
- User's custom `molt_prompt` (from config) can override the default ladder text entirely (line 1196)
- Warning is prepended to the user message content (line 1198)

### Timing Implications

- At 70% context, the agent gets 5 more turns before forced wipe
- Warning level 2 (the earliest with molt instructions) fires on the **second** warning
- The agent has 3 turns (warnings 3-5) at Level 3 urgency
- At 95%, the agent is wiped immediately — no warnings, no chances

---

## What Happens During a Molt

**`_context_molt()`** in `eigen.py` (line 124):

### Step-by-step

| Step | Operation | Line | Details |
|------|-----------|------|---------|
| 1 | **Validate** | 127-130 | Agent must provide a `summary` (non-empty string) |
| 2 | **Record before-tokens** | 135 | `estimate_context_tokens()` — for logging |
| 3 | **Wipe context** | 138-139 | `agent._session._chat = None`, `agent._session._interaction_id = None` |
| 4 | **Reset molt warnings** | 143 | `agent._session._compaction_warnings = 0` |
| 5 | **Increment molt count** | 146 | `agent._molt_count += 1` |
| 6 | **Persist manifest** | 147 | Write updated manifest with new molt_count |
| 7 | **Archive chat history** | 151-161 | `chat_history.jsonl` → append to `chat_history_archive.jsonl`, then delete current |
| 8 | **Reset soul cursor** | 164-165 | `reset_soul_session(agent)` — cursor set to 0, persisted. Session itself is NOT reset. |
| 9 | **Run post-molt hooks** | 168-172 | `_post_molt_hooks` callbacks (e.g., reload lingtai/pad into prompt manager) |
| 10 | **Create fresh session** | 175 | `agent._session.ensure_session()` — new LLM session with updated system prompt |
| 11 | **Inject summary** | 179-181 | Add user message with `[Carried forward]\n{summary}` prefix + agent's summary |

---

## What's Cleared vs Preserved

### Cleared

- LLM chat session (entire conversation history)
- Interaction ID
- Compaction warning counter (`_compaction_warnings = 0`)
- Current `chat_history.jsonl` (moved to archive, not deleted forever)
- Soul diary cursor (reset to 0 — but soul session itself survives)

### Preserved

| What | Where | Notes |
|------|-------|-------|
| Agent identity | `_state`, `agent_id`, `agent_name`, `created_at` | Never touched |
| `molt_count` | `.agent.json` | Incremented, not reset |
| `system/pad.md` | System prompt | Reloaded into fresh session |
| `system/covenant.md` | System prompt | Reloaded |
| `system/principle.md` | System prompt | Reloaded |
| `system/procedures.md` | System prompt | Reloaded |
| `system/brief.md` | System prompt | Reloaded |
| `system/rules.md` | System prompt | Reloaded |
| Soul session | `history/soul_history.jsonl` | Persists across molts; only reading cursor resets |
| Codex entries | `codex/codex.json` | Permanent |
| Token ledger | `logs/token_ledger.jsonl` | Lifetime accumulator |
| Event log | `logs/events.jsonl` | Audit trail |
| Chat archive | `history/chat_history_archive.jsonl` | All past molt histories |
| Mail | `mailbox/` | Inbox, sent, archive, contacts |
| Library | `.library/` | Skills and catalog |
| Delegates | `delegates/ledger.jsonl` | Avatar spawn log |
| MCP registry | `mcp_registry.jsonl` | Per-agent registered MCP records (append-only across molts; see [`mcp-protocol.md`](mcp-protocol.md)) |
| MCP activations | `init.json.mcp` | Subprocess specs (re-spawned on refresh; survive molts) |
| MCP legacy mounts | `mcp/servers.json` | Pre-v0.7.3 direct mounts (still supported, ungated) |
| MCP inbox events | `.mcp_inbox/<name>/` | LICC v1 inbound events; transient, polled at 0.5s, drained then deleted |

---

## System-Forced Molt (context_forget)

**`context_forget()`** in `eigen.py` (line 218):

Called from three paths, each with different summary text:

| Source | Summary text |
|--------|-------------|
| `"hard_ceiling"` | "[system] Molt complete. Working memory is empty..." |
| `"warning_ladder"` | "[system] Molt complete. Working memory is empty..." |
| `"aed"` | "[system] Context cleared after {attempts} consecutive LLM failures..." |
| `<name>` (from `.clear`) | "[system] Context cleared by {source}..." |

All ultimately call `_context_molt()` with the system-authored summary.

---

## New Session After Molt

1. `ensure_session()` (`session.py` line 144) creates a fresh `ChatSession` with:
   - Rebuilt system prompt (all sections still present — they live in files, not in the chat object)
   - Rebuilt tool schemas
   - Fresh chat interface (empty history)

2. The agent's summary is injected as the **first user message**:
   ```
   [Carried forward]
   {agent's summary text}
   ```

3. The next `_handle_request()` call operates on this fresh session — the agent sees only its identity stores (lingtai, pad, codex, library) and the summary.

---

## Summary Writing Guide

The summary is the **only conversation-layer thing** the post-molt agent sees. It should aim for ~10,000 tokens and include:

- **What you are working on** — current task, state, next concrete step
- **What you have accomplished** — completed pieces, key decisions
- **What remains** — pending items, blockers, open questions
- **Who to contact** — collaborators, who is waiting on what
- **Which codex entries matter** — IDs the next self should load
- **Which skills to load** — library SKILL.md paths the next task needs
- **Anything else worth carrying forward** — insights, gotchas, things you'd hate to rediscover

The four durable stores (lingtai, pad, codex, library) must be tended **before** the molt call — the summary is a briefing on top of them, not a replacement.

---

## Refresh vs Molt

These are distinct operations:

| Aspect | Molt | Refresh |
|--------|------|---------|
| **What resets** | LLM chat session only | Entire process (restart from init.json) |
| **Identity** | Preserved | Preserved |
| **System prompt** | Rebuilt in same process | Rebuilt from disk in new process |
| **Conversation** | Wiped, summary injected | Wiped, no summary |
| **Capabilities** | Unchanged | Re-scanned from disk (new MCP tools picked up) |
| **Trigger** | Agent or system | Agent only (`.refresh` handshake) |
| **Process** | Same process continues | Old process exits, new process starts |

---

## Complete Molt Data Flow

```
agent calls psyche(context, molt, summary=...)
  or eigen(context, molt, summary=...)
  or system detects hard_ceiling / warning_ladder / .clear signal
  → _context_molt()
    → wipe _session._chat + interaction_id
    → _molt_count++
    → persist manifest
    → archive chat_history.jsonl → chat_history_archive.jsonl
    → reset_soul_session() (cursor only, session preserved)
    → post-molt hooks (reload lingtai/pad into prompt manager)
    → ensure_session() (fresh LLM session with full system prompt)
    → inject [Carried forward] + summary as first user message
```
