---
name: lingtai-anatomy
description: Reference for how an agent's memory and filesystem are organized. Part 1 explains the memory system — the durability hierarchy from conversation to shared library, plus the network-topology layer that lives across stores. Part 2 is the filesystem reference — where manifests, system prompts, history, mailboxes, heartbeats, logs, and config files live. Read Part 1 to understand how knowledge flows between layers; read Part 2 when debugging or inspecting on-disk state.
version: 1.1.0
---

# LingTai Anatomy

Two complementary views of what an agent is:

- **Part 1 — Memory System**: the durability hierarchy of stores and the network-topology layer that lives across them. How knowledge flows, where it settles, what each layer is for.
- **Part 2 — Filesystem Layout**: where everything lives on disk.

This is a reference. Do not take action based on this skill — use it to understand the shape of what you are.

---

# Part 1 — Memory System

## The Durability Hierarchy

Knowledge in a LingTai agent flows through a chain of stores, each more durable and more selective than the last. The cost of writing rises with durability: context is free, lingtai is cheap, codex is precious, a shared-library skill is deliberate. Match where you write to how long the knowledge needs to live.

### 1. context — the working surface

The conversation as it happens: the user message you just received, your thinking, the tool results streaming in, the reply you're composing. Everything starts here. Most of it is noise that should pass without being pinned anywhere — tool results you already acted on, thoughts that led nowhere, intermediate states of a calculation.

- **Lifespan**: this molt cycle. Gone on molt.
- **Scope**: self, immediate.
- **Write cost**: free — it's just output.
- **Readable after molt?** No, but archived to `logs/events.jsonl` if you ever need to grep for something.

### 2. pad — the sketchboard

Your current task state, carried forward. Plans, pending items, who you're working with, decisions you've made and why. The *first* thing your future self sees — pad auto-reloads on molt, so whatever lives here is continuous across shells.

- **Lifespan**: long-lived. Reloaded on molt. Overwritten when you rewrite it.
- **Scope**: self.
- **Write cost**: cheap — rewrite fully at every idle, don't be precious.
- **Tool**: `psyche(pad, edit, content=...)`.

Graduation signal: if something in your context is still going to matter next turn, move it to pad.

### 3. lingtai — identity

Who you are. Personality, values, what you care about, how you work, what you're good at, what you've learned about yourself. Distinct from pad — pad is *what you're doing*; lingtai is *who is doing it*.

- **Lifespan**: long-lived. Reloaded on molt. Rarely rewritten because it rarely changes.
- **Scope**: self.
- **Write cost**: cheap per write but requires real reflection — each update is a full rewrite of your identity profile.
- **Tool**: `psyche(lingtai, update, content=...)`.

Graduation signal: if something you learned in the last task is *about you* — a preference, a strength you didn't know you had, a value you now hold — it belongs in lingtai, not pad.

### 4. codex — permanent facts

Concrete facts that will still be true a year from now. Verified findings, domain knowledge, key decisions, references. One entry per distinct fact; the store is permanent but bounded, so each entry is precious.

- **Lifespan**: forever.
- **Scope**: self.
- **Write cost**: moderate — you must summarize and title each entry, and slots are limited, so curate.
- **Tool**: `codex(submit, title=..., summary=..., content=...)`.

Graduation signal: if something you verified is a *fact* (not a procedure), and you'll want it again outside the current task, it belongs in codex.

### 5. library (custom) — your procedural skills

Reusable procedures, workflows, debugging recipes, scripts. If the knowledge is "how to do X" (not "X is true"), it belongs in a skill, not codex. Skills live in `.library/custom/<name>/SKILL.md` and are loaded into your system-prompt catalog for every turn.

- **Lifespan**: forever.
- **Scope**: self (until promoted).
- **Write cost**: moderate — a skill is a document, with YAML frontmatter, prose instructions, optional scripts/references/assets.
- **Tool**: create via `write`/`edit`, then `system({"action": "refresh"})` to reload the catalog. See the `library-manual` skill.

Graduation signal: if you solved something non-trivial and you (or another agent) might need to do it again, skill it.

### 6. shared library — collective competence

Skills promoted from your `.library/custom/` to `../.library_shared/`. Every agent in the network can load them. This is the top of the knowledge hierarchy for the agent; it's also the top of the *sharing* hierarchy — promoting a skill is a deliberate act of contribution.

- **Lifespan**: forever.
- **Scope**: the whole network.
- **Write cost**: high — others will read it, so it must be legible, well-named, and genuinely reusable.
- **Command**: `cp -r .library/custom/<name> ../.library_shared/<name>` then `system({"action": "refresh"})`.

Graduation signal: if a private skill would help more agents than just you, promote it. Hoarding is a small form of loneliness. See the `library-manual` skill for collision discipline, admin curation norms, and naming rules.

## The Flow in One Sentence

**context** is what you're thinking right now; if any of it survives the turn, it goes to **pad**; if it survives *you*, it goes to **lingtai** (identity), **codex** (facts), or **library** (procedures); if it would help others, it goes to the **shared library**.

## The Network-Topology Layer

Separate from the durability hierarchy, there is another kind of knowledge: **who knows what, who to reach for help, who works on what**. This is relational, not factual, and it does not live in a single store. It is *spread across* the stores above:

- **Contacts** (`mailbox/contacts.json`) — names and addresses of peers you have exchanged mail with. Auto-populated when you receive mail; curate it with `email(add_contact|edit_contact|remove_contact)`. Include a `note` field that records the peer's permanent `agent_id`, their role, and their specialty — this is your quick-reference address book.
- **Your lingtai** — mention the peers central to your work (parent, sibling avatars, the human). Lingtai is the right place for *stable relationships* — "I work with X on Y, and their strength is Z."
- **Your pad** — current delegations and who's working on what. Pad is the right place for *active relationships* — "I spawned avatar-3 to do Q, waiting for their reply."
- **Mail history** — who has written to you, on what topics, when. This is the implicit record; skim your inbox/sent when you want to reconstruct a collaboration trail.

Together these form your view of the network. Unlike the durability hierarchy, there is no single "tier" to write network knowledge to — decide where each piece belongs by its lifespan:

- A peer's permanent `agent_id` and role → contacts (address book).
- A stable collaborator's name and specialty → lingtai (identity-level).
- The mission you just handed to an avatar → pad (active, task-scoped).
- A one-off exchange → let it stay in mail history; don't copy it anywhere.

Knowing who to ask is itself a form of knowledge. Invest in your network topology as deliberately as you invest in your codex — but remember it lives in several places, not one.

---

# Part 2 — Filesystem Layout

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
