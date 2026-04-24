---
name: lingtai-anatomy
description: Reference for how an agent's memory, filesystem, and runtime are organized. Part 1 explains the memory system — the durability hierarchy from conversation to shared library, plus auxiliary layers (soul, token ledger, time veil) and the network-topology layer that lives across stores. Part 2 is the filesystem reference — where manifests, system prompts, history, mailboxes, heartbeats, logs, signal files, and config live, with exact field-level schemas. Part 3 is the runtime anatomy — turn loop, state machine, signal consumption lifecycle, molt mechanics, and mail atomicity. Read Part 1 to understand how knowledge flows between layers; Part 2 when debugging or inspecting on-disk state; Part 3 when reasoning about *how an agent runs*.
version: 1.2.0
---

# LingTai Anatomy

Three complementary views of what an agent is:

- **Part 1 — Memory System**: the durability hierarchy of stores and the network-topology layer that lives across them. How knowledge flows, where it settles, what each layer is for.
- **Part 2 — Filesystem Layout**: where everything lives on disk.
- **Part 3 — Runtime Anatomy**: what happens *when an agent runs*. Turn loop, state machine, signal lifecycle, molt mechanics, mail delivery.

This is a reference. Do not take action based on this skill — use it to understand the shape of what you are.

## Kernel vs. wrapper

Two Python packages split the agent surface:

- **`lingtai_kernel`** — minimum viable agent runtime. Provides the base agent loop, session management, working-directory lock, heartbeat/state machine, mailbox filesystem protocol, and four built-in intrinsics: `eigen` (pad / context / name), `mail`, `soul`, `system`.
- **`lingtai`** — the batteries-included wrapper. Adds higher-level capabilities on top of the kernel: `psyche` (richer pad/lingtai/codex editing), `codex`, `library`, `daemon`, `avatar`, `bash`, file ops, etc.

Most agents you meet in this ecosystem use the `lingtai` wrapper, so tool names in this doc (`psyche(pad, edit, ...)`, `codex(submit, ...)`, `library(...)`) refer to wrapper-level intrinsics. At the kernel level, the equivalent is `eigen(pad, edit, ...)`. When a claim below is kernel-level vs. wrapper-level, it's called out.

---

# Part 1 — Memory System

## The Durability Hierarchy

Knowledge in a LingTai agent flows through a chain of stores, each more durable and more selective than the last. The cost of writing rises with durability: context is free, lingtai is cheap, codex is precious, a shared-library skill is deliberate. Match where you write to how long the knowledge needs to live.

### 1. context — the working surface

The conversation as it happens: the user message you just received, your thinking, the tool results streaming in, the reply you're composing. Everything starts here. Most of it is noise that should pass without being pinned anywhere — tool results you already acted on, thoughts that led nowhere, intermediate states of a calculation.

- **Lifespan**: this molt cycle. The live LLM chat session is wiped on molt.
- **Scope**: self, immediate.
- **Write cost**: free — it's just output.
- **Readable after molt?** Not by you — but persisted to disk in two places: `history/chat_history.jsonl` (active molt's turn-by-turn transcript, archived to `chat_history_archive.jsonl` when molt occurs) and `logs/events.jsonl` (lifecycle events, heartbeat, errors — the audit stream). If you ever need to reconstruct what happened, grep these.

### 2. pad — the sketchboard

Your current task state, carried forward. Plans, pending items, who you're working with, decisions you've made and why. The *first* thing your future self sees — pad auto-reloads on molt, so whatever lives here is continuous across shells.

- **Lifespan**: long-lived. Reloaded on molt from `system/pad.md`. Overwritten when you rewrite it.
- **Scope**: self.
- **Write cost**: cheap — rewrite fully at every idle, don't be precious.
- **Tool**: `psyche(pad, edit, content=...)` — wrapper-level. Supports `files=[...]` to embed file contents inline with `[file-N]` markers. Also `psyche(pad, append, ...)` pins read-only file references into `system/pad_append.json`. At the kernel level the equivalent is `eigen(pad, edit, ...)`.

Graduation signal: if something in your context is still going to matter next turn, move it to pad.

### 3. lingtai — identity

Who you are. Personality, values, what you care about, how you work, what you're good at, what you've learned about yourself. Distinct from pad — pad is *what you're doing*; lingtai is *who is doing it*.

- **Lifespan**: long-lived. Reloaded on molt. Rarely rewritten because it rarely changes.
- **Scope**: self.
- **Write cost**: cheap per write but requires real reflection — each update is a full rewrite of your identity profile.
- **Storage**: identity is a *merge* of two files — `system/covenant.md` (protected, host-set at agent creation) + `system/lingtai.md` (agent-editable). Both are concatenated and injected into the protected `covenant` section of the system prompt, so nothing you write via normal prompt injection can overwrite it.
- **Tool**: `psyche(lingtai, update, content=...)` writes to `system/lingtai.md` only. The covenant side is untouched.

Graduation signal: if something you learned in the last task is *about you* — a preference, a strength you didn't know you had, a value you now hold — it belongs in lingtai, not pad.

### 4. codex — permanent facts

Concrete facts that will still be true a year from now. Verified findings, domain knowledge, key decisions, references. One entry per distinct fact; the store is permanent but bounded, so each entry is precious.

- **Lifespan**: forever. Stored in `codex/codex.json` as a flat list of entries.
- **Scope**: self.
- **Write cost**: moderate — you must summarize and title each entry, and slots are limited (default cap: 20 entries), so curate.
- **Entry schema**: `{id, title, summary, content, supplementary}`. The `supplementary` field lets one entry carry extra detail without consuming another slot — useful when a topic genuinely needs depth.
- **Catalog injection**: your system prompt always shows the codex *catalog* (titles + summaries only, not full content) so you can browse at a glance. Full entry content is fetched on demand via `codex(view, ids=[...])` and pulled into pad via `codex(export, ids=[...])`.
- **Tool**: `codex(submit, title=..., summary=..., content=...)`.

Graduation signal: if something you verified is a *fact* (not a procedure), and you'll want it again outside the current task, it belongs in codex.

### 5. library (custom) — your procedural skills

Reusable procedures, workflows, debugging recipes, scripts. If the knowledge is "how to do X" (not "X is true"), it belongs in a skill, not codex. Skills live in `.library/custom/<name>/SKILL.md` and are loaded into your system-prompt catalog for every turn.

- **Lifespan**: forever.
- **Scope**: self (until promoted).
- **Write cost**: moderate — a skill is a document, with YAML frontmatter, prose instructions, optional scripts/references/assets.
- **Catalog vs. body**: the system prompt carries only the *catalog* — each skill's name, description, version, path, extracted from YAML frontmatter and wrapped in `<available_skills>`. Full SKILL.md bodies are **not** cached in the prompt; they load on demand when you call the skill.
- **Discovery paths**: the library intrinsic scans three sources — `.library/intrinsic/` (kernel/TUI-bundled, rewritten on init), `.library/custom/` (agent-written, canonical edit location), and any user-declared paths in `init.json` under `manifest.capabilities.library.paths` (supports `~`, absolute, relative-to-working-dir).
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

**Promotion is always agent-directed.** There is no machinery that auto-promotes from one layer to the next. The hierarchy describes *where knowledge settles* based on your deliberate choice of tool (`psyche(pad, ...)`, `codex(submit, ...)`, `write` + `system({"action": "refresh"})`, `cp` + refresh). When you see a layer "flow" to the next, that flow is your tool call, not a background process.

## Auxiliary layers

Three additional stores live alongside the durability hierarchy but don't follow the promotion model. They are not "levels" you write to deliberately — they are the agent's audit/reflection/visibility surfaces.

- **soul** (`logs/soul_flow.jsonl`, `logs/soul_inquiry.jsonl`) — introspection machinery. The `soul` intrinsic supports two modes: `flow` (auto-triggered periodic mirror session, writes to `soul_flow.jsonl`) and `inquiry` (on-demand, clones the conversation, injects a question, returns an answer in the tool result). Soul is not a durable knowledge store — its role is to *feed* identity refinement, which you then crystallize into lingtai or codex via explicit tool calls.
- **token_ledger** (`logs/token_ledger.jsonl`) — append-only audit trail of every LLM call. Each line: `{ts, input, output, thinking, cached}`. Not a knowledge layer, but critical for understanding context pressure, molt triggers, and cost.
- **time_veil** — a *visibility* layer, not a store. When `manifest.time_awareness: false`, the kernel scrubs all timestamp-bearing fields (`received_at`, `sent_at`, `current_time`, `ts`, ...) from what the LLM sees, so the agent can run in a time-blind mode. On-disk state is unchanged — only the LLM surface is veiled.

## The Network-Topology Layer

Separate from the durability hierarchy, there is another kind of knowledge: **who knows what, who to reach for help, who works on what**. This is relational, not factual, and it does not live in a single store. It is *spread across* the stores above:

- **Contacts** (`mailbox/contacts.json`) — names and addresses of peers you have exchanged mail with. Auto-populated when you receive mail; curate it with `mail(add_contact|edit_contact|remove_contact)`. Include a `note` field that records the peer's permanent `agent_id`, their role, and their specialty — this is your quick-reference address book.
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
│   ├── pad.md                   # Editable: long-term memory (agent-written)
│   ├── pad_append.json          # Pinned read-only file references
│   ├── brief.md                 # Externally maintained by secretary
│   ├── rules.md                 # Protected: network rules (updated via .rules signal)
│   ├── llm.json                 # LLM config snapshot
│   └── comment.md               # App-level instructions
│
├── history/
│   ├── chat_history.jsonl       # Current molt's conversation log (JSONL)
│   └── chat_history_archive.jsonl  # All past molts' chat_history, appended
│
├── logs/
│   ├── agent.log                # Subprocess stdout/stderr
│   ├── events.jsonl             # Lifecycle/audit events (JSONL)
│   ├── token_ledger.jsonl       # Per-API-call token counts
│   │                            # Each line: {ts, input, output, thinking, cached}
│   ├── soul_flow.jsonl          # Periodic soul-flow introspection records
│   └── soul_inquiry.jsonl       # On-demand soul-inquiry records
│
├── mailbox/
│   ├── inbox/                   # Received mail
│   │   └── <uuid>/
│   │       └── message.json
│   ├── outbox/                  # Queued outbound mail (pre-dispatch)
│   │   └── <uuid>/
│   │       └── message.json
│   ├── sent/                    # Successfully dispatched mail
│   │   └── <uuid>/
│   │       └── message.json
│   ├── archive/                 # Archived mail
│   ├── contacts.json            # Known addresses: [{address, name, note}]
│   └── read.json                # Mail-read tracker: {<uuid>: true|false}
│
├── codex/
│   └── codex.json               # Permanent-fact entries (bounded)
│
├── delegates/
│   └── ledger.jsonl             # Avatar spawning log
│                                # Each line: {parent, child, child_name}
│
├── mcp/
│   └── servers.json             # MCP server configs (stdio/http)
│
└── signal files (transient):
    ├── .sleep                   # Consumed immediately. Agent enters ASLEEP.
    ├── .suspend                 # Consumed immediately. Agent exits gracefully.
    ├── .interrupt               # Consumed immediately. Cancels current turn/tool.
    ├── .prompt                  # Text content → injected as [system] message, then deleted.
    ├── .clear                   # Forced context molt (optional source tag in content).
    ├── .rules                   # New network rules → written to system/rules.md, then deleted.
    ├── .inquiry                 # "<source>\n<question>" → soul introspection.
    ├── .inquiry.taken           # Mutex marker while soul inquiry runs.
    ├── .refresh                 # Config reload request (handshake with .refresh.taken).
    └── .refresh.taken           # Agent mid-refresh — watcher waits for lock to drop.
```

## Key File Formats

### .agent.json (manifest)

```json
{
  "agent_id": "20260423-221801-1710",
  "agent_name": "orchestrator",
  "nickname": "小灵",
  "address": "orchestrator",
  "created_at": "2026-04-23T22:18:01Z",
  "started_at": "2026-04-24T08:53:42Z",
  "admin": {"karma": true, "nirvana": false},
  "language": "wen",
  "stamina": 36000,
  "state": "idle",
  "soul_delay": 120,
  "molt_count": 2,
  "capabilities": [
    ["avatar", {}],
    ["bash", {"yolo": true}],
    ["codex", {}],
    ["library", {"paths": ["~/.lingtai-tui/bundled-skills"]}]
  ],
  "location": {
    "city": "Los Angeles",
    "region": "California",
    "country": "US",
    "timezone": "America/Los_Angeles"
  }
}
```

**State values** (lowercase in JSON, even though the Python enum names are uppercase): `"active"`, `"idle"`, `"stuck"`, `"asleep"`, `"suspended"`.

**Kernel-written fields** (auto-set on agent launch, don't hand-edit): `agent_id`, `created_at`, `started_at`, `molt_count`, `state`.

**Capabilities format**: a list of `[name, config_dict]` pairs. Each capability is wrapper-level and carries its own config schema (e.g. `bash` takes `{"yolo": bool}`, `library` takes `{"paths": [...]}`).

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

The agent updates this on every heartbeat cycle (~1 second interval). To check liveness: parse the float, compare to current time. **Alive if `time.time() - ts < 2.0` seconds** (the kernel default threshold in `handshake.is_alive`). Missing file, unreadable file, or stale timestamp = dead. Humans (admin: null) always return alive — no heartbeat is written for them.

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
  "to": ["human"],
  "cc": [],
  "subject": "Task complete",
  "message": "I finished the analysis...",
  "type": "normal",
  "received_at": "2026-04-13T16:35:00Z",
  "attachments": [],
  "identity": {
    "agent_id": "20260423-221801-1710",
    "agent_name": "orchestrator",
    "nickname": "小灵",
    "address": "orchestrator",
    "created_at": "2026-04-23T22:18:01Z",
    "started_at": "2026-04-24T08:53:42Z",
    "admin": {"karma": true, "nirvana": false},
    "language": "wen",
    "stamina": 36000,
    "state": "active",
    "soul_delay": 120,
    "molt_count": 2
  }
}
```

**Key fields**: `to` is always a list, even for one recipient. `identity` is a full manifest snapshot of the sender at the moment the message was composed — it ships the sender's identity inline so recipients can decide how to treat the message without a second lookup. `_mailbox_id` always equals `id` (used for folder naming).

### token_ledger.jsonl

Each line is one API call:

```json
{"ts": "2026-04-23T22:18:10Z", "input": 1500, "output": 200, "thinking": 50, "cached": 800}
```

### delegates/ledger.jsonl

Each line is one avatar spawn event:

```json
{"parent": "orchestrator", "child": "/abs/path/to/.lingtai/avatar-1", "child_name": "avatar-1"}
```

Note: avatar spawning is a wrapper-level (`lingtai.core.avatar`) feature, not a kernel intrinsic. Kernel-only agents don't have `delegates/`.

---

# Part 3 — Runtime Anatomy

How an agent actually runs. Parts 1 and 2 described *what* exists and *where*; this part describes *when* and *how* things happen.

## The Turn Loop

A **turn** is one complete `_handle_message` cycle: consume a queued message → invoke the LLM → execute tool calls → persist chat history → return to idle (or sleep). The kernel's run loop (`base_agent._run_loop`) is a single-threaded event dispatcher that pulls one message at a time from an in-memory inbox queue and processes it fully before looping.

- **Message batching**: multiple queued messages waiting at the start of a turn are *concatenated* into a single LLM call — they don't each get their own turn.
- **History persistence**: `history/chat_history.jsonl` is appended to after the turn completes, not after each individual tool call. A crash mid-turn loses the in-flight turn but not prior history.
- **Auto-insight**: after every N turns (configurable via `insights_interval`), the agent triggers a soul inquiry before returning to idle — this is the agent reflecting on what it just did.
- **Heartbeat is separate**: a background daemon thread writes `.agent.heartbeat` every ~1 second and checks signal files. The main turn loop and the heartbeat loop run concurrently; signals can arrive mid-turn.

## The Five-State Machine

Every agent is always in exactly one of five states, visible in `.agent.json`'s `state` field (lowercase value).

| State | Meaning | How you enter | How you leave |
|-------|---------|---------------|---------------|
| `active` | Processing a message (LLM call + tools in flight) | message arrives in inbox; turn begins | turn completes (→ `idle`) or throws (→ `stuck`) |
| `idle` | Waiting in the run loop for the next message | turn completes cleanly | new message arrives (→ `active`), `.sleep` signal (→ `asleep`) |
| `stuck` | Exception in the current turn; attempting recovery | unhandled exception in `_handle_message` | AED recovery succeeds (→ `active`), or max attempts exhausted (→ `asleep`) |
| `asleep` | Sleep mode: heartbeat still written, most signals ignored, waiting for a waking message | `.sleep` signal, or `stuck` exhausted AED retries | message arrives in inbox (→ `active`) |
| `suspended` | Process terminated gracefully | `.suspend` or `.refresh` signal | new process launch from outside |

**AED (Automated Error Detection)** — when a turn throws, the run loop catches it, rebuilds the LLM session, injects a recovery message describing what failed, and retries up to `max_aed_attempts` times. If AED exhausts, the agent sleeps rather than force-molting — this gives the operator a chance to inspect logs before any destructive recovery.

**State is persisted** to `.agent.json` on every transition, but rapid `active`↔`idle` flips are throttled to avoid breaking LLM prompt caches on every keystroke.

## Signal Consumption Lifecycle

The heartbeat loop (1-second interval) is the single consumer of signal files. Understanding each signal's consumption pattern helps you predict when your write will actually take effect.

| Signal | Detection | Consumption pattern |
|--------|-----------|--------------------|
| `.sleep` | Heartbeat loop | Deleted immediately. Agent enters `asleep`; heartbeat continues. |
| `.suspend` | Heartbeat loop | Deleted immediately. Agent enters `suspended`; process exits. |
| `.interrupt` | Heartbeat loop | Deleted immediately. Sets the cancel event — wakes from a nap, cancels a running tool. |
| `.prompt` | Heartbeat loop | Content read into memory, **file deleted** before the inject. Content becomes a `[system]` message in the inbox queue. No `.prompt.taken` marker — consumption is atomic. |
| `.clear` | Heartbeat loop | Optional source tag read from content (default `"admin"`), then deleted. Triggers a forced context molt with a system-authored summary. |
| `.inquiry` | Heartbeat loop (mutex-gated) | Only processed if `.inquiry.taken` does **not** exist. On detect: `rename(.inquiry → .inquiry.taken)`. First line = source (`human`/`insight`/agent name), rest = question. A daemon thread runs the inquiry, then deletes `.inquiry.taken`. |
| `.refresh` | Heartbeat loop | `rename(.refresh → .refresh.taken)`, agent transitions to `suspended`, triggers shutdown. A deferred watcher subprocess waits for `.agent.lock` to disappear, then relaunches the agent. New process deletes `.refresh.taken` and sends itself a "refresh successful" message. |
| `.rules` | Heartbeat loop | Content read, diff'd against `system/rules.md`. If different, write to `rules.md` and trigger system refresh. File deleted afterward. No `.taken` marker. |

**Implication for remote signal senders**: `.prompt` is fire-and-forget (no way to tell whether it was consumed from the outside); `.inquiry` gives you an atomic ack via the `.taken` rename; `.refresh` has a proper handshake you can poll.

## Molt: What Gets Wiped, What Survives

Molt is the agent's context-reset ritual: it sheds accumulated LLM history to make room for fresh conversation while preserving identity and durable stores.

**What molt wipes:**

- The live LLM chat session (`_session._chat = None`) and its interaction ID. The next turn starts a fresh LLM conversation.
- In-memory molt-warning counters.
- Soul mirror session state.

**What molt preserves:**

- **Identity**: `agent_id`, `agent_name`, `created_at` — never touched.
- **`molt_count`**: incremented, not reset.
- **System-prompt sections** (all files under `system/`): principle, covenant, procedures, rules, brief, lingtai, pad, comment. All reloaded into the fresh session.
- **Durable stores**: codex, library, mailbox (inbox/outbox/sent/archive/contacts), logs (events, token ledger, soul), delegates ledger, MCP config.
- **Chat history archive**: the current molt's `history/chat_history.jsonl` is appended to `chat_history_archive.jsonl`, then the current file is cleared. No past conversation is deleted — only *made invisible* to the LLM by moving out of the active file.

**Post-molt injection**: the agent (or the system, if molt was forced via `.clear`) provides a summary. This summary becomes the opening user message in the fresh LLM session — it's what the post-molt agent "remembers" about its pre-molt self. The quality of this summary is the only bridge across the gap.

**Triggering**: molt can be (a) agent-initiated via `eigen(context, forget, ...)` / `psyche(...)`, (b) forced by the `.clear` signal, or (c) triggered when accumulated context exceeds `manifest.molt_pressure`% of the context window.

## Mail Delivery: Outbox to Inbox Atomicity

The kernel's mail subsystem is disk-backed JSON with a small state machine.

**Self-send shortcut**: if the `to` address matches the sender's own working-dir name or mail-service address, the mail intrinsic writes directly into the sender's own `inbox/<uuid>/message.json` and pokes the run loop. No outbox round-trip.

**Peer-send flow**:

1. **Compose** → the agent calls `mail(send, to=..., subject=..., message=...)`. The intrinsic generates a UUID, writes `mailbox/outbox/<uuid>/message.json` with the full message body + a manifest-snapshot `identity` block + a `deliver_at` timestamp.
2. **Dispatch** → a daemon (outside the kernel — typically the TUI's mail service, or an addon for remote transport) polls all agents' `outbox/` directories. When a message to a reachable recipient is found, the daemon copies it into the recipient's `inbox/<uuid>/message.json`.
3. **Sent** → upon confirmed dispatch, the message moves from `outbox/<uuid>` → `sent/<uuid>`, enriched with `sent_at` and a delivery status. This rename is the sender's proof-of-delivery signal.

**Recipient pickup**: when a message lands in `inbox/`, the recipient's heartbeat/inbox-watch wakes the run loop and queues the message for the next turn.

**Read tracking**: `mailbox/read.json` is a separate set-of-UUIDs tracker. Read status survives mail moves (e.g., archiving). The message files themselves don't carry a read flag.

**Key design choice**: no external broker is assumed. All mail is plain JSON on disk. This makes debugging transparent (you can `cat message.json` at any stage) but puts atomicity responsibility on the dispatcher — a crashed dispatcher mid-rename can leave a message in both `outbox/` and the recipient's `inbox/`, requiring cleanup logic. The kernel's in-tree dispatcher uses atomic rename where possible; cross-machine addons (SSH, IMAP, Telegram) replace the atomic step with their own delivery semantics.
