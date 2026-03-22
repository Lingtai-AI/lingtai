# Agent Identity Card & Vigil Enforcement

**Date:** 2026-03-21
**Scope:** lingtai-kernel + lingtai
**Status:** Draft

## Problem

Three issues with agent state persistence and lifecycle:

1. **`.agent.json` is write-only.** The agent writes its identity card but never reads it back. On revive (resurrection of a dormant agent), settings like `admin`, `language`, and `capabilities` are lost or defaulted.

2. **`soul_delay` is memory-only.** The agent can tune its soul delay at runtime via the soul intrinsic, but this is never persisted. On any restart path, it resets to the config default.

3. **`lifetime` is decorative.** The agent sees a countdown via `system(show)` but nothing happens when it reaches zero. Agents run forever unless externally killed.

Additionally:
- `_soul_flow` (boolean) is redundant — `soul_delay` is the only setting for flow, and "off" = large delay.
- `system/llm.json` exists only for revive but the kernel doesn't know about it.
- The revive path (`_revive_agent`) drops `admin`, `language`, and most config.
- System intrinsic action names (`shutdown`, `restart`, `sleep`, `silence`, `annihilate`) don't match the project's vibe.

## Design Principles

**The identity card contains what the agent knows about itself.** If the agent can't see it or change it, it doesn't belong in `.agent.json`. Host-level operational knobs (`max_turns`, `retry_timeout`, `cpr_timeout`, `ensure_ascii`) stay implicit as `AgentConfig` constructor args.

**LLM config stays separate.** `system/llm.json` (or `combo.json`) contains provider, model, api_key, base_url, thinking_budget. This is sensitive and not the agent's concern — the agent knows its combo *name*, not its contents.

**No agent can extend its own vigil.** Only the layer above can grant more time:
- Human → sets vigil on 本我 (root agent). Only human can extend it.
- 本我 → sets vigil on 分身 (avatars), can revive them with new vigil.
- 分身 → lives within its allotted vigil, cannot self-extend. Can ask 本我 for more time via mail, but 本我 decides.

**Ephemeral agent network.** Every agent has a finite vigil. When it expires, the agent goes dormant gracefully. No orphan agents silently consuming resources. Revive is the natural continuation for long-lived work.

## Terminology

| Concept | en | zh | wen |
|---|---|---|---|
| agent wakefulness budget | vigil | 精力 | 精力 |

Replaces `lifetime` throughout the codebase.

## System Intrinsic Action Renames

Consolidate and rename actions to match the project's vibe.

### Action consolidation

- **`shutdown` removed** — absorbed into `quell` (self-quell = shutdown)
- **`restart` removed** — replaced by `refresh` (lighter concept: reload tools, reset session, same identity and memory)
- **`sleep` → `nap`** — lighter, more natural
- **`silence` → `interrupt`** — more accurate
- **`annihilate` → `nirvana`** — matches `admin.nirvana` permission, dignified

### Quell unification

`quell` handles both self and other:
- No address → self-quell (what `shutdown` used to do)
- With address → quell another agent (requires `admin.karma`)

### Final action table

| Action | en | zh | wen | Self | Other |
|---|---|---|---|---|---|
| show | show | 观己 | 观己 | self-inspection | — |
| nap | nap | 小憩 | 小憩 | timed pause, wake on mail | — |
| refresh | refresh | 沐浴 | 更衣 | reload MCP, reset session | — |
| quell | quell | 沉睡 | 沉寐 | go dormant | make other dormant (karma) |
| revive | revive | 唤醒 | 唤醒 | — | wake dormant other (karma) |
| interrupt | interrupt | 打断 | 打断 | — | interrupt other's work (karma) |
| nirvana | nirvana | 涅槃 | 涅槃 | — | permanently destroy (nirvana) |

**Sleep gradient:** 小憩 (light nap) → 沉寐 (deep slumber) → 涅槃 (eternal rest)

**Easter egg:** refresh is 沐浴 (zh) + 更衣 (wen) = 沐浴更衣, a classical idiom meaning "bathe and change clothes before something important."

### Signal file renames

- `.silence` → `.interrupt`
- `.quell` stays `.quell`

## Changes

### 1. `.agent.json` becomes two-way

**Fields** (everything the agent knows about itself):

```json
{
  "agent_id": "a1b2c3d4e5f6",
  "agent_name": "researcher",
  "admin": {"karma": true},
  "language": "wen",
  "capabilities": [["file", {}], ["vision", {}]],
  "combo": "gemini-pro",
  "soul_delay": 120.0,
  "vigil": 3600.0,
  "address": "/agents/a1b2c3d4e5f6",
  "started_at": "2026-03-21T10:00:00Z"
}
```

**Write path** (unchanged pattern):
- `_build_manifest()` builds the dict
- Written at construction and whenever a runtime-tunable field changes
- `soul_delay` is the only runtime-tunable field currently

**Read path** (new):
- `_revive_agent()` reads `.agent.json` and passes values to the constructor
- The constructor itself does NOT read `.agent.json` — values are always passed explicitly
- This keeps the kernel's "no file-based config" principle: the *host* reads the file, not the kernel

### 2. Remove `_soul_flow` boolean

**Delete:**
- `AgentConfig.flow` field
- `BaseAgent._soul_flow` attribute
- The `if not self._soul_flow and not self._soul_oneshot: return` guard in `_start_soul_timer()`

**Replace with:**
- `_start_soul_timer()` always starts (unless shutdown is set or no oneshot pending and delay is very large)
- "Turn off flow" = set `soul_delay` to a value larger than `vigil`. The timer fires after the agent is already dormant, so it never whispers.
- No explicit "off" state needed — the vigil enforces it naturally.

### 3. Persist `soul_delay` to `.agent.json`

**When the soul intrinsic changes `soul_delay`:**
1. Update `self._soul_delay` (as today)
2. Re-write `.agent.json` via `_build_manifest()` + `write_manifest()`

**On construction:**
- `soul_delay` comes from constructor arg / config default as today
- On revive, the value read from `.agent.json` is passed to the constructor

### 4. Rename `lifetime` → `vigil`

**Rename throughout:**
- `AgentConfig.lifetime` → `AgentConfig.vigil`
- `_config.lifetime` references in system intrinsic, base_agent, etc.
- i18n keys: add vigil-related strings to en.json, zh.json, wen.json
- `system(show)` returns `vigil` and `vigil_left` instead of `lifetime` and `life_left`

### 5. Enforce vigil — heartbeat self-quell

**In `_heartbeat_loop()`**, after the existing signal-file checks:

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

**Behavior:**
- Agent goes DORMANT gracefully (same as `.quell` signal)
- Soul timer cancelled as part of shutdown
- Heartbeat file stops updating → `is_alive()` returns False
- `.agent.json` remains on disk with full identity
- Agent is revivable

### 6. Rename system intrinsic actions

**In `intrinsics/system.py`:**
- Remove `_shutdown` handler — `quell` with no address handles self-quell
- Remove `_restart` handler — replaced by `_refresh`
- Rename `_sleep` → `_nap`
- Rename `_silence` → `_interrupt`
- Rename `_annihilate` → `_nirvana`
- Update action enum: `["show", "nap", "refresh", "quell", "revive", "interrupt", "nirvana"]`

**Quell handler logic:**
```python
def _quell(agent, args: dict) -> dict:
    address = args.get("address")
    if not address:
        # Self-quell
        agent._log("self_quell", reason=args.get("reason", ""))
        agent._set_state(AgentState.DORMANT, reason="self-quell")
        agent._shutdown.set()
        return {"status": "ok", "message": "..."}
    # Quell other — existing karma-gated logic
    ...
```

**Refresh handler** (replaces restart):
```python
def _refresh(agent, args: dict) -> dict:
    reason = args.get("reason", "")
    agent._log("refresh_requested", reason=reason)
    agent._refresh_requested = True  # renamed from _restart_requested
    agent._shutdown.set()
    return {"status": "ok", "message": "..."}
```

**Signal file rename:**
- `.silence` → `.interrupt`
- `.quell` unchanged

### 7. Fix `_revive_agent()` in lingtai

**Current** (broken):
```python
revived = Agent(
    svc,
    agent_name=agent_meta.get("agent_name"),
    agent_id=agent_meta.get("agent_id"),
    base_dir=str(target.parent),
    capabilities=capabilities,
    # missing: admin, language, config (vigil, soul_delay)
)
```

**Fixed:**
```python
from lingtai_kernel.config import AgentConfig

revived_config = AgentConfig(
    provider=llm_config["provider"],
    model=llm_config["model"],
    vigil=agent_meta.get("vigil", 3600.0),
    language=agent_meta.get("language", "en"),
    # host-level knobs use defaults — not the agent's concern
)

revived = Agent(
    svc,
    agent_name=agent_meta.get("agent_name"),
    agent_id=agent_meta.get("agent_id"),
    base_dir=str(target.parent),
    capabilities=capabilities,
    admin=agent_meta.get("admin", {}),
    config=revived_config,
)
```

Revive reuses the agent's vigil budget from `.agent.json` — it's part of the identity. The clock starts fresh (`started_at` is overwritten) so the agent gets a full vigil period. `soul_delay` is restored from `.agent.json`.

### 8. Add `combo` field to `.agent.json`

**In `_build_manifest()`:** include `combo` name (string, not contents).

**In avatar spawn:** already writes `combo.json` to working dir. No change needed.

**In `_revive_agent()`:** resolve combo by name from `~/.lingtai/combos/{combo}.json`, fall back to `{working_dir}/combo.json`.

## Files Changed

### lingtai-kernel

| File | Change |
|---|---|
| `config.py` | Rename `lifetime` → `vigil`, rename `flow_delay` → `soul_delay`, remove `flow` |
| `base_agent.py` | Remove `_soul_flow`, add vigil enforcement in heartbeat, persist `soul_delay` on change, add `vigil` + `combo` to manifest, rename `_restart_requested` → `_refresh_requested`, update `_perform_restart` → `_perform_refresh` |
| `intrinsics/system.py` | Consolidate actions (remove shutdown/restart, add refresh, rename sleep→nap, silence→interrupt, annihilate→nirvana), unify quell for self+other |
| `intrinsics/soul.py` | Remove `_soul_flow` guard, persist `soul_delay` after change |
| `i18n/en.json` | Update all system_tool strings for new action names + vigil |
| `i18n/zh.json` | Update all system_tool strings (沉睡, 小憩, 沐浴, 打断, 涅槃, 精力) |
| `i18n/wen.json` | Update all system_tool strings (沉寐, 小憩, 更衣, 打断, 涅槃, 精力) |

### lingtai

| File | Change |
|---|---|
| `agent.py` | Fix `_revive_agent()` to read full `.agent.json`, add `combo` to manifest |
| `capabilities/avatar.py` | Pass `combo` name in config/manifest |

## Migration

- `lifetime` → `vigil` is a clean rename, no backward compat needed (per project conventions)
- `config.flow` removal is clean — no external API exposed it
- `shutdown` / `restart` action names removed — no backward compat shims
- `.silence` signal file → `.interrupt`
- Existing `.agent.json` files without `vigil`/`soul_delay` fields get defaults on read

## Non-Goals

- Vigil extension mechanism (no self-extension by design)
- Persisting host-level knobs (`max_turns`, `retry_timeout`, etc.)
- Merging `system/llm.json` into `.agent.json` (they serve different purposes)
- Warning the agent before vigil expires (could be added later via molt-style warnings)
