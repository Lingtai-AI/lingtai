# Topology Tape Reconstruction Design

## Problem

The portal records topology snapshots every 3 seconds to `topology.jsonl`. This tape has accumulated backward-compatibility issues:
- Old frames have `null` arrays instead of `[]`
- Old frames lack `direct`/`cc`/`bcc` breakdown on mail edges
- The tape only covers time since the portal first started, missing earlier agent history
- Migrations and frontend null-guards pile up to handle legacy data

All the information needed to reconstruct the tape already exists on the filesystem:
- `events.jsonl` in each agent's `logs/` — state transitions with timestamps
- `mailbox/inbox/` and `mailbox/archive/` — emails with `received_at`/`sent_at` timestamps, `to`/`cc`/`bcc` fields
- `.agent.json` — agent identity
- `delegates/ledger.jsonl` — avatar spawn relationships

## Design

### Core Idea

The tape is a **cache**, not a source of truth. On portal start, validate the tape format. If invalid or missing, reconstruct the entire tape from filesystem artifacts. Then continue additive recording as before.

### Startup Flow

```
Portal starts
  ↓
Read last frame of topology.jsonl
  ↓
Format check: does mail_edges[0].direct exist (not null)?
  ↓
YES → tape is current format → skip to additive recording
  ↓
NO / empty / missing → reconstruct from filesystem
  ↓
Write fresh topology.jsonl
  ↓
Start additive recording (3s interval, same as today)
```

### Reconstruction Algorithm

**Input**: all agent directories under `.lingtai/`

**Step 1: Collect all events**

Scan every agent's `logs/events.jsonl`. Extract:
- `agent_state` events: `{ts, address, old, new}` — state transitions
- `email_sent` events: `{ts, address, to, cc, bcc}` — mail sent (has recipient breakdown)
- `refresh_start` / `heartbeat_start` events: `{ts, address}` — marks agent coming alive (after suspend)

Also scan `delegates/ledger.jsonl` for avatar edges.

Sort all events by `ts`.

**Step 2: Determine time range**

- `t0` = earliest event timestamp across all agents
- `t1` = now (or latest event if no agents are alive)

**Step 3: Build frames at 3-second intervals**

For each `t` from `t0` to `t1`, stepping by 3 seconds:

1. **Nodes**: all agents that have been "born" by time `t` (first event timestamp ≤ t). Use current `.agent.json` for identity (name, nickname, address). Human node always present.

2. **Agent state at time t**: replay `agent_state` events up to `t`. Start each agent as `ASLEEP` (they boot asleep). If no events exist yet for an agent at time `t`, mark as `ASLEEP`. Between a `suspended` event and the next event from that agent, mark as `SUSPENDED`.

3. **Alive**: an agent is alive at time `t` if its last state transition before `t` was NOT to `suspended`, AND there exists at least one event from it within a reasonable window (e.g., its first event ≤ t ≤ its last event).

4. **Mail edges at time t**: count all emails with `received_at ≤ t` (from mailbox/inbox + mailbox/archive). Break down by to/cc/bcc. Use the actual email files — they have the canonical data.

5. **Avatar edges**: from `delegates/ledger.jsonl`, include edges where the spawn timestamp ≤ t.

6. **Contact edges**: from current contacts files (no historical data — use current state for all frames).

7. **Stats**: computed from the above (count by state, sum mail counts).

**Step 4: Write tape**

Write each frame as a JSONL line: `{"t": <unix_ms>, "net": <Network>}` — same format as today.

### What Changes

**Backend (`portal/internal/`)**:
- New `reconstruct.go` in `internal/api/` or `internal/fs/` — the reconstruction logic
- `server.go` `StartRecording` — call reconstruction if tape is invalid, then additive
- Remove `BirthTime` / `earliestBirth` / backdated-frame logic (reconstruction handles it)
- Remove migration `m002_tape_normalize` (reconstruction replaces it)
- `AppendTopologyAt` stays (used by reconstruction to write frames)

**Frontend (`portal/web/src/`)**:
- Remove all `|| []` null guards on network arrays (tape is always clean)
- Remove `hasBreakdown` fallback in Graph.tsx (direct/cc/bcc always present)
- Remove `normState()` in Graph.tsx (backend normalizes state to uppercase)
- `diffMailBullets` — remove `|| []` guards (clean data guaranteed)

**Keep as-is**:
- `AppendTopology` / `AppendTopologyAt` — still used for additive recording
- `/api/topology` endpoint — still serves the tape
- `/api/network` endpoint — still serves live state
- Frontend replay engine — unchanged (still consumes `TapeFrame[]`)
- `TopBar`, `FilterPanel`, `BottomBar` — unchanged

### Format Check

Read the last non-empty line of `topology.jsonl`. Parse as JSON. Check:
```
frame.net.mail_edges is array (not null)
AND (mail_edges is empty OR mail_edges[0].direct is number)
```

If both pass → valid. Otherwise → reconstruct.

### Performance

Benchmarked on real data:
- 3400 frames / 17MB tape: reconstruction takes ~100ms in Python, Go will be faster
- Email scanning: ~80 emails across 7 agents, negligible
- Events scanning: ~2000 events per agent, well under 1 second total

Even for 100+ agents with months of history, reconstruction should complete in under 5 seconds.

### Edge Cases

- **No events.jsonl** (agent created but never launched): include node as ASLEEP with no state transitions
- **No emails**: mail edges are empty, which is correct
- **Agent directory deleted but emails remain in other agents' mailboxes**: the sender address in emails will still be counted for edge computation even if the agent dir is gone — this is correct (the communication happened)
- **Multiple portal restarts**: each restart checks the tape. If it was being recorded with current format, the check passes and reconstruction is skipped. Fast startup.
