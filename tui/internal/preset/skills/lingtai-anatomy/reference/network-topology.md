# Network Topology — LingTai Anatomy Reference

> **Scope:** Avatar system, network discovery, three-edge model, rules
> distribution, daemon-vs-avatar distinction, and best practices for
> multi-agent coordination.

---

## v1 Corrections

| # | Issue | Correction |
|---|-------|------------|
| 1 | v1 mentioned `delegates/ledger.jsonl` format only in passing. | The ledger is the **foundation** of the avatar tree — its append-only records define parent→child relationships, ghost nodes, and tree-walk traversal. |
| 2 | v1 lacked the entire avatar spawn mechanism. | Avatar creation involves name validation, safety checks (path-escape prevention), directory scaffolding, and an independent subprocess launch. |
| 3 | v1 had no network discovery algorithm. | `AgentNetwork` discovers all agents and edges via a four-pass filesystem crawl. |
| 4 | v1 omitted rules distribution. | The `avatar()` tool can push rule updates down the avatar tree via `.rules` signal files, consumed by each agent's heartbeat. |

---

## The Avatar Tree

### Two Avatar Types

| Type | Chinese | Description |
|------|---------|-------------|
| **Shallow** | 投胎 (reincarnation) | White-slate agent. Only `init.json` is written — the avatar starts with default system prompts and no inherited knowledge. |
| **Deep** | 二重身 (doppelgänger) | Full clone. Copies `system/`, `codex/`, and `exports/` from the parent. Inherits knowledge, rules, and exported state. |

```
Parent agent
├── init.json          ← always copied
├── system/            ← copied only for deep avatar
├── codex/             ← copied only for deep avatar
└── exports/           ← copied only for deep avatar
```

### Spawn Flow

```
agent.avatar(name, mission, type, ...)
  │
  ├─ 1. Name validation
  │     • Single segment (no path separators)
  │     • Unicode \w characters + hyphens only
  │     • Max 64 characters
  │     • Must not collide with existing subdirectories
  │
  ├─ 2. Pre-spawn check
  │     • Scan ledger for existing live avatars with same name
  │     • If found → return already_active (no duplicate spawn)
  │
  ├─ 3. Safety checks
  │     • resolved = (network_root / name).resolve()
  │     • assert resolved.parent == network_root
  │     • Prevents path-escape attacks (e.g. "../../../etc")
  │
  ├─ 4. Directory scaffolding
  │     • Create <network_root>/<name>/ (sibling of parent)
  │     • Write init.json (same LLM config as parent)
  │     • For deep: copy system/, codex/, exports/
  │     • Write .comment file (mission briefing from reasoning param)
  │
  ├─ 5. Ledger append
  │     • Append JSONL record to parent's delegates/ledger.jsonl
  │     • Fields: ts, event="avatar", name, working_dir (relative),
  │       mission, type, pid
  │
  └─ 6. Process launch
        • subprocess.Popen(["lingtai", "run", "<name>"])
        • Fully detached — no pipes, no shared FDs
        • Avatar communicates with parent via mail only
```

### Ledger Format

```jsonc
// delegates/ledger.jsonl — one JSON object per line
{"ts":1745793600.0,"event":"avatar","name":"researcher","working_dir":"researcher","mission":"Scan codebase for auth patterns","type":"deep","pid":48872}
{"ts":1745797200.0,"event":"avatar","name":"analyst","working_dir":"analyst","mission":"Analyze results","type":"shallow","pid":49001}
```

Key properties:
- **Append-only** — records are never deleted or mutated.
- **Dead avatars** — directories are skipped in tree walks but ledger records
  persist for historical analysis.
- **Ghost nodes** — `_build_avatar_edges` creates nodes for ledger entries whose
  directories no longer exist, preserving the graph structure.

### Independent Lifecycle

An avatar is a **fully detached subprocess**:

1. No shared file descriptors with parent.
2. Survives parent process death (PID 1 reparenting on Linux).
3. Has its own soul cycle, nap schedule, and mailbox.
4. Communicates with parent **exclusively via mail** — no shared memory, no
   sockets, no signals.
5. Writes its own `.agent.json` on startup, consumed by the network discovery
   algorithm.

---

## The Three-Edge Model

`AgentNetwork` (defined in `network.py`) models the agent ecosystem as a
directed multigraph with **three distinct edge layers**, all discovered by
crawling the filesystem — no central registry required.

### Edge Types

```
┌─────────────────────────────────────────────────────────────────┐
│  Layer 1: Avatar Edges  (parent → child)                        │
│    Source: delegates/ledger.jsonl                                │
│    Meaning: "I created this agent"                               │
│    AvatarEdge: parent_address, child_address, child_name,       │
│               spawned_at, mission, capabilities, provider, model│
├─────────────────────────────────────────────────────────────────┤
│  Layer 2: Contact Edges  (declared "knows about")               │
│    Source: mailbox/contacts.json                                 │
│    Meaning: "I am aware of this agent's existence"               │
│    ContactEdge: owner_address, target_address, target_name, note│
├─────────────────────────────────────────────────────────────────┤
│  Layer 3: Mail Edges  (actual communication)                    │
│    Source: mailbox/inbox/ + mailbox/sent/                        │
│    Meaning: "We have exchanged messages"                         │
│    MailEdge: sender, recipient, count, last_at, subjects,       │
│              records: [MailRecord(id, subject, timestamp)]      │
└─────────────────────────────────────────────────────────────────┘
```

### Discovery Algorithm (Four Passes)

```
Pass 1: _discover_agents
  │  Scan all immediate subdirectories of network root
  │  for .agent.json files.
  │  → Build node set: {address → AgentInfo}
  │
Pass 2: _build_avatar_edges
  │  For each agent, read delegates/ledger.jsonl
  │  For each event="avatar" record:
  │    • Create AvatarEdge(parent → child)
  │    • If child directory missing → add ghost node
  │
Pass 3: _build_contact_edges
  │  For each agent, read mailbox/contacts.json
  │  For each contact entry:
  │    • Create ContactEdge(owner → target)
  │    • If target not in node set → add ghost node
  │
Pass 4: _build_mail_edges
     For each agent, crawl mailbox/inbox/ + mailbox/sent/
     Parse every message.json:
       • Group by (sender, recipient) pair
       • Aggregate into MailEdge with count, last_at, subjects
       • Store individual MailRecords for detail queries
```

### Convenience Queries

| Method | Returns | Description |
|--------|---------|-------------|
| `children_of(address)` | `list[AvatarEdge]` | All avatars spawned by this agent |
| `contacts_of(address)` | `list[ContactEdge]` | All declared contacts of this agent |
| `mail_of(address)` | `list[MailEdge]` | All mail relationships (sent or received) |

### Visual Example

```
           ┌──────────┐
           │  parent   │
           └──┬───┬────┘
         avatar│   │contact
              │   │
       ┌──────▼─┐ ┌▼──────────┐
       │ child-a │ │ neighbor  │
       └─────────┘ └───────────┘
            ▲            │
            └─── mail ───┘
             (3 messages)
```

---

## Network Rules Distribution

### Mechanism

The `avatar()` tool supports a `rules` parameter that broadcasts updated
operating rules to the caller **and all descendants** in the avatar tree.

```
avatar(rules, rules_content="...updated rules text...")
  │
  ├─ Write .rules signal to self/network/.rules
  │
  └─ _walk_avatar_tree(root=self)
       BFS from root:
         for each descendant (via ledger):
           if directory exists and not dead:
             write .rules signal
           else:
             skip (visited or dead)
```

### Consumption by Heartbeat

Each agent's heartbeat cycle:

```
1. Check for .rules signal file in network directory
2. If present:
   a. Read rules_content
   b. Diff against current system/rules.md
   c. If changed → rewrite system/rules.md → refresh system prompt
   d. Delete .rules signal (consumed)
3. Continue normal heartbeat
```

### Properties

| Property | Behavior |
|----------|----------|
| **Idempotent** | Writing the same rules content twice is harmless — diff detects no change. |
| **Best-effort** | Failures (permissions, missing dirs) are silently swallowed. |
| **Eventual** | Distribution is not atomic — descendants update on their next heartbeat, not instantly. |
| **Re-entrant** | If a descendant is spawned after distribution, it will miss the current signal but receive the next one. |

---

## Daemon vs Avatar

| Dimension | Daemon (分神) | Avatar (化身) |
|-----------|---------------|---------------|
| **Lifecycle** | One-shot LLM session | Independent long-running process |
| **Working directory** | Shared with parent | Own directory under `delegates/` |
| **Persistence** | None — output only | Full: mailbox, codex, pad, molt history |
| **Communication** | Returns text to caller | Mail-based, asynchronous |
| **Concurrency** | Default 4 simultaneous | Unlimited (resource-bound) |
| **Output limit** | ~2 000 characters | No hard limit |
| **Survives parent** | No — terminated with parent | Yes — fully detached process |
| **Cost** | Low (single call) | Ongoing (soul cycles consume tokens) |
| **Use case** | File scans, research, batch transforms, one-off analysis | Long-running tasks, memory-dependent work, multi-session coordination |

### Decision Heuristic

```
if task is output-only AND cheap AND no state needed:
    → use daemon
elif task needs persistence OR memory OR multiple sessions:
    → use avatar
else:
    → default to daemon (cheaper)
```

---

## Best Practices

### Avatar Escalation Protocol

Avatars spawned with an **empty admin block** (the default) must mail their
parent when encountering:

| Trigger | Example |
|---------|---------|
| **Blockers** | Missing dependency, permission denied, unreachable resource |
| **Scope creep** | Task requirements expanding beyond original mission |
| **Budget pressure** | Token usage approaching limits without completion path |
| **Broken peers** | A sibling avatar is unresponsive or producing errors |
| **Security concerns** | Unexpected file access patterns, credential exposure |
| **Surprising findings** | Results that contradict assumptions or require parent judgment |

### Parent Duties

1. **Record spawns in pad** — log avatar name, mission, and expected completion.
2. **Update on reports** — when an avatar mails a status update, acknowledge it.
3. **Don't poll silently** — avoid tight-loop checking of avatar state.  Let
   failures escalate via the mail protocol naturally.
4. **Reap dead avatars** — periodically review ledger for stopped avatars and
   clean up their directories if no longer needed.

### Naming Conventions

- **Single-segment only** — no path separators, no dots.
- **Unicode \w + hyphen** — letters, digits, underscore, CJK characters, hyphen.
- **Max 64 characters** — enforced by the spawn validation.
- **No collisions** — must not match any existing subdirectory in the network root.

### Ledger Discipline

- **Append-only** — never edit or truncate `ledger.jsonl`.
- **Dead records persist** — stopped avatars remain in the ledger as historical
  truth.  Tree walks skip directories that don't exist but the graph structure
  is preserved.
- **PID staleness** — a recorded PID does not guarantee the process is alive.
  Always verify with `is_alive` checks.

---

## Where Network Knowledge Lives

Relational knowledge in LingTai is **not centralized** — it is distributed
across multiple filesystem artifacts that must be cross-referenced:

```
Agent Working Directory
│
├── .agent.json              ← Who I am (identity, PID, state)
├── delegates/
│   └── ledger.jsonl         ← Whom I spawned (avatar edges)
├── mailbox/
│   ├── contacts.json        ← Whom I know about (contact edges)
│   ├── inbox/               ← What I received (mail edges, inbound)
│   └── sent/                ← What I sent (mail edges, outbound)
├── system/
│   ├── lingtai.md           ← Stable relationships (who I work with)
│   └── pad.md               ← Active delegations (who's doing what)
└── mailbox/
    ├── contacts.json        ← Whom I know about (contact edges)
    ├── inbox/               ← What I received (mail edges, inbound)
    └── sent/                ← What I sent (mail edges, outbound)
```

### Knowledge Retrieval Patterns

| Question | Source | Method |
|----------|--------|--------|
| "Who are my children?" | `delegates/ledger.jsonl` | Filter for `event="avatar"` records |
| "Who do I know?" | `mailbox/contacts.json` | Read array, each entry is a known agent |
| "Who have I talked to?" | `mailbox/sent/` + `mailbox/inbox/` | Crawl message headers, extract from/to |
| "Is agent X alive?" | `{x}/.agent.heartbeat` | Read timestamp, compare to now (< 2s = alive) |
| "What is agent X's mission?" | `delegates/ledger.jsonl` | Find latest `event="avatar"` for name X |
| "What rules apply?" | `system/rules.md` | Read directly (refreshed by `.rules` signal) |

### Cross-Referencing

No single file contains the full picture.  The `AgentNetwork` class in
`network.py` performs the four-pass crawl to build a complete graph from these
independent sources.  This design ensures:

- **Fault isolation** — corruption of one agent's contacts doesn't affect others.
- **Offline analysis** — the entire network can be reconstructed from filesystem
  state alone, without running any agent processes.
- **No single point of failure** — there is no central database or registry
  service.  Knowledge is inherently distributed.

---

*End of network-topology reference.*
