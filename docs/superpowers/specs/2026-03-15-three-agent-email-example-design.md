# Three-Agent Email Example — Design Spec

**Date:** 2026-03-15
**File:** `examples/three_agents.py`
**Purpose:** Browser-based playground for testing email CC/BCC flows between three agents.

## Overview

A new example app following the `two_agents.py` pattern, extended to three agents with CC/BCC support in the compose UI. Single self-contained Python file with embedded HTML/CSS/JS.

## Backend

### Ports

| Component | Port |
|-----------|------|
| User mailbox | TCP 8300 |
| Alice | TCP 8301 |
| Bob | TCP 8302 |
| Charlie | TCP 8303 |
| Web UI | HTTP 8080 |

### LLM

MiniMax (`MiniMax-M2.5-highspeed`), same as `two_agents.py`. Reads `MINIMAX_API_KEY` from env / `.env`.

### Agents

Three identical agents, each with:
- `TCPMailService` on their respective port
- `MemoryLoggingService` (keys: `a`, `b`, `c`)
- `add_capability("email")`
- `AgentConfig(max_turns=10)`
- Same generic prompt (proactive, email-only communication, no courtesy loops)
- Role section with name, address, and contacts list (other two agents + user)

### HTTP Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/` | Serve HTML page |
| GET | `/inbox` | Return user mailbox (received emails) |
| GET | `/diary` | Return agent activity logs, keyed by `a`, `b`, `c` |
| POST | `/send` | Send email to agent(s) |

#### POST /send payload

```json
{
  "agent": "a",
  "message": "hello",
  "cc": ["b"],
  "bcc": ["c"]
}
```

- `agent`: required, single target (`"a"`, `"b"`, or `"c"`)
- `message`: required, email body
- `cc`: optional, array of agent keys — mapped to addresses, included in email payload
- `bcc`: optional, array of agent keys — mapped to addresses, included in email payload

The handler creates a `TCPMailService` sender and calls `email.handle()` with the appropriate `address`, `cc`, and `bcc` fields (converted from agent keys to `127.0.0.1:port` addresses).

## Frontend

### Layout

Same split as `two_agents.py`:
- Left (flex:2): Inbox panel with compose bar
- Right (flex:1): Diary panel with agent tabs

### Diary Panel

Tabs at the top: **All | Alice | Bob | Charlie**

- "All" shows interleaved entries from all agents, sorted by time (default)
- Agent tabs filter to that agent's entries only
- Color tags: Alice = red (`#e94560`), Bob = teal (`#4ecdc4`), Charlie = amber (`#f0a500`)

### Compose Bar

Bottom of inbox panel, left to right:
1. **To:** dropdown — Alice / Bob / Charlie (single select)
2. **CC** button — toggles a checkbox row showing the two agents not in To
3. **BCC** button — toggles a checkbox row showing the two agents not in To
4. **Message input** — text field
5. **Send button**

CC/BCC rows appear below the main compose line when toggled. Checkboxes update dynamically when the To selection changes (exclude the To target from CC/BCC options).

### Inbox Display

Emails show:
- Sent: "To: Alice" (+ "CC: Bob, Charlie" if CC was used)
- Received: "From: Alice" with subject, CC field if present
- BCC is never displayed (blind by design)

## File Structure

Single file: `examples/three_agents.py`. No external dependencies beyond stoai and its optional `minimax` extra.

## Non-Goals

- No distinct agent specializations (all generic)
- No WebSocket (polling at 1.5s, same as existing)
- No authentication or persistence
- No refactoring of `two_agents.py`
