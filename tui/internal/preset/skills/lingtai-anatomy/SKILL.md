---
name: lingtai-anatomy
description: Reference for the .lingtai/ directory structure. Read this to understand where agent manifests, system prompts, history, mailboxes, heartbeats, logs, and config files live. Useful when debugging, inspecting, or building tools that interact with LingTai agents.
version: 1.0.0
---

# .lingtai/ Directory Anatomy

This is a reference document. It describes the structure of a LingTai project directory. Do not take action based on this skill — use it to understand where things are.

## Project Root

```
.lingtai/
├── meta.json                    # Version tracking for migrations
├── .port                        # Portal port number (when portal is running)
├── .library/                     # Skill library
│   ├── intrinsic/               # Bundled skills (symlink, managed by TUI)
│   ├── <recipe-name>/           # Recipe skills (managed by TUI)
│   └── custom/                  # Agent-created skills
├── .addons/                     # Shared addon configs (cross-agent)
│   └── <addon-name>/
│       └── config.json
├── .tui-asset/                  # TUI-only metadata (not agent state)
├── .portal/                     # Portal-only data
│   ├── topology.jsonl           # Network snapshots (JSONL, every 3s)
│   ├── replay/
│   │   ├── chunks/              # Delta-encoded hourly chunks (.json.gz)
│   │   └── manifest.json        # Chunk metadata
│   └── reconstruct.progress     # Reconstruction progress (transient)
├── human/                       # The human participant (see Agent Directory below)
└── <agent-name>/                # Each agent has its own directory
```

## Identifying the Orchestrator

Scan `.lingtai/*/.agent.json`. The orchestrator is the agent whose `admin` field is a JSON object (map) with at least one truthy boolean value:

```json
{"admin": {"karma": true, "nirvana": true}}
```

- `admin: null` or absent → human (not an agent)
- `admin: {"karma": false, "nirvana": false}` → regular agent (not orchestrator)
- `admin: {"karma": true, ...}` → orchestrator (at least one truthy value)

There is typically one orchestrator per project. It is the primary agent the human interacts with.

## Agent Directory Layout

Every agent (including `human/`) follows the same structure. Human directories have `admin: null` in `.agent.json` and lack heartbeat/status files.

```
<agent-name>/
├── .agent.json                  # Live manifest (identity)
├── init.json                    # Full initialization config
├── .agent.heartbeat             # Unix timestamp (float), updated each cycle
├── .agent.lock                  # Held while agent process is running
├── .status.json                 # Live runtime metrics
│
├── system/                      # System prompt sections
│   ├── system.md                # Assembled system prompt
│   ├── principle.md             # Protected: core principles
│   ├── covenant.md              # Protected: behavioral covenant
│   ├── rules.md                 # Protected: hard rules
│   ├── procedures.md            # Protected: standard procedures
│   ├── lingtai.md               # Editable: identity/character (agent-written)
│   ├── pad.md                # Editable: long-term memory (agent-written)
│   ├── brief.md                 # Externally maintained by secretary
│   └── comment.md               # App-level instructions
│
├── history/
│   └── chat_history.jsonl       # Conversation log (JSONL)
│
├── logs/
│   ├── agent.log                # Subprocess stdout/stderr
│   └── token_ledger.jsonl       # Per-API-call token counts
│                                # Each line: {input, output, thinking, cached}
│
├── mailbox/
│   ├── inbox/                   # Received mail
│   │   └── <uuid>/
│   │       └── message.json
│   ├── sent/                    # Sent mail
│   │   └── <uuid>/
│   │       └── message.json
│   ├── archive/                 # Archived mail
│   └── contacts.json            # Known addresses: [{address, name, note}]
│
├── delegates/
│   └── ledger.jsonl             # Avatar spawning log
│                                # Each line: {parent, child, child_name}
│
├── mcp/
│   └── servers.json             # MCP server configs (stdio/http)
│
└── signal files (transient):
    ├── .sleep                   # Agent enters sleep mode
    ├── .suspend                 # Agent terminates gracefully
    ├── .interrupt               # Reserved
    ├── .prompt                  # Text content → injected as [system] message
    ├── .inquiry                 # "<source>\n<question>" → triggers soul introspection
    ├── .inquiry.taken           # Agent is processing an inquiry
    ├── .refresh                 # Config reload requested
    └── .refresh.taken           # Agent is mid-refresh
```

## Key File Formats

### .agent.json (manifest)

```json
{
  "agent_name": "orchestrator",
  "address": "orchestrator",
  "nickname": "小灵",
  "state": "ACTIVE",
  "admin": {"karma": true, "nirvana": true},
  "capabilities": ["avatar", "search", "vision"],
  "location": {
    "city": "Los Angeles",
    "region": "California",
    "country": "US",
    "timezone": "America/Los_Angeles"
  }
}
```

States: `ACTIVE`, `IDLE`, `STUCK`, `ASLEEP`, `SUSPENDED`.

### init.json

Contains the full initialization configuration. Key fields:

- `manifest` — agent identity (llm, agent_name, language, capabilities, soul, stamina, admin)
- `manifest.llm` — model config (model, provider, base_url, api_compat, api_key_env)
- `manifest.soul` — soul config (delay in seconds between introspection cycles)
- `manifest.stamina` — max uptime in seconds before auto-sleep
- `manifest.context_limit` — max context tokens before molt
- `manifest.molt_pressure` — context usage % that triggers molt
- `venv_path` — path to Python venv for this agent
- `env_file` — path to .env file for secrets
- `addons` — external messaging service configs (telegram, wechat, feishu, imap)

### .agent.heartbeat

Plain text file containing a single unix timestamp as a float:

```
1744567890.123456
```

The agent updates this on every heartbeat cycle. To check liveness: parse the float, compare to current time. Alive if difference < 3 seconds. Missing file = dead.

### .status.json

```json
{
  "tokens": {
    "estimated": false,
    "context": {
      "system_tokens": 1500,
      "tools_tokens": 800,
      "history_tokens": 3200,
      "total_tokens": 5500,
      "window_size": 128000,
      "usage_pct": 4.3
    }
  },
  "runtime": {
    "uptime_seconds": 3600.5,
    "stamina_left": 82800.0
  }
}
```

### message.json (mail)

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "_mailbox_id": "550e8400-e29b-41d4-a716-446655440000",
  "from": "orchestrator",
  "to": "human",
  "cc": [],
  "subject": "Task complete",
  "message": "I finished the analysis...",
  "type": "normal",
  "received_at": "2026-04-13T16:35:00.000000Z",
  "attachments": [],
  "identity": {
    "agent_name": "orchestrator",
    "admin": {"karma": true, "nirvana": true}
  }
}
```

### token_ledger.jsonl

Each line is one API call:

```json
{"input": 1500, "output": 200, "thinking": 50, "cached": 800}
```

### delegates/ledger.jsonl

Each line is one avatar spawn event:

```json
{"parent": "orchestrator", "child": "/abs/path/to/.lingtai/avatar-1", "child_name": "avatar-1"}
```
