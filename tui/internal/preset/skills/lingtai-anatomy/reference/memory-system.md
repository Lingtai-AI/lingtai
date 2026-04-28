# Memory System

The durability hierarchy, auxiliary layers, and the knowledge-flow model that connects them.

---

## v1 错处更正

| 位置 | v1 错误 | 更正 |
|------|---------|------|
| lingtai 条目 | "Both [covenant.md and lingtai.md] are concatenated and injected into the protected `covenant` section" | covenant.md 注入 `## covenant` 段；lingtai.md 注入 `## lingtai` 段。两者都是受保护的（`protected=True`），但注入点不同。 |
| lingtai 条目 | 工具为 `eigen(context, forget, ...)` | 正确的工具调用是 `psyche(object="context", action="molt", summary=...)`。`forget` 不出现在当前工具描述中。 |
| Auxiliary / soul | "The `soul` intrinsic supports two modes: `flow` … and `inquiry`" | `flow`（心流）不是工具 action——它是空闲时自动触发的内省事件。soul 工具只有两个 action：`inquiry`（自省）和 `delay`（调频）。 |
| Auxiliary / soul | 未提及心流的呈现方式 | 心流以 `[心流]` 标记注入 inbox，是灵台自动发出的声音。 |

---

## The Durability Hierarchy

Knowledge flows through a chain of stores, each more durable and more selective than the last. The cost of writing rises with durability. Match where you write to how long the knowledge needs to live.

### 1. context — the working surface

The live conversation: user messages, your thinking, tool results, the reply you're composing. Everything starts here; most of it should pass without being pinned anywhere.

- **Lifespan**: this molt cycle. Wiped on molt.
- **Scope**: self, immediate.
- **Write cost**: free.
- **Persisted?** Not for you — but two on-disk trails exist: `history/chat_history.jsonl` (turn-by-turn transcript, archived on molt) and `logs/events.jsonl` (lifecycle events, heartbeat, errors). Grep these to reconstruct what happened.

### 2. pad — the sketchboard

Your current task state. Plans, pending items, who you're working with, decisions. The *first* thing your future self sees — pad auto-reloads on molt.

- **Lifespan**: long-lived. Overwritten when you rewrite it.
- **Scope**: self.
- **Write cost**: cheap — rewrite fully at every idle.
- **Storage**: `system/pad.md` (Markdown) + `system/pad_append.json` (pinned file references).
- **Tool**: `psyche(pad, edit, content=...)` — supports `files=[...]` to embed file contents with `[file-N]` markers. `psyche(pad, append, files=[...])` pins read-only references that reload on every session (including post-molt). Clear pins with `files=[]`.

Graduation: if something in your context will still matter next turn, move it to pad.

### 3. lingtai — identity

Who you are. Personality, values, strengths, how you work. Distinct from pad — pad is *what you're doing*; lingtai is *who is doing it*.

- **Lifespan**: long-lived. Rarely rewritten because identity rarely changes.
- **Scope**: self.
- **Write cost**: cheap per write but requires real reflection — each update is a *full rewrite*.
- **Storage**: `system/covenant.md` (protected, host-set, not agent-editable) + `system/lingtai.md` (agent-editable). Both are protected (`protected=True`) in the prompt manager, but injected into *different* sections: `covenant` and `lingtai` respectively.
- **Tool**: `psyche(lingtai, update, content=...)` writes to `system/lingtai.md` only. The covenant side is untouched.

Graduation: if something you learned is *about you* — a preference, a strength, a value — it belongs in lingtai.

### 4. codex — permanent facts

Concrete facts that will still be true a year from now. One entry per distinct fact; the store is permanent but bounded.

- **Lifespan**: forever. Stored in `codex/codex.json`.
- **Scope**: self.
- **Write cost**: moderate — slots are limited (default cap: 20), so curate.
- **Entry schema**: `{id, title, summary, content, supplementary}`. Use `supplementary` for depth without consuming another slot.
- **Catalog injection**: system prompt always shows titles + summaries. Full content fetched via `codex(view, ids=[...])`, exported via `codex(export, ids=[...])`.
- **Tool**: `codex(submit, title=..., summary=..., content=...)`.

Graduation: if something you verified is a *fact* (not a procedure), it belongs in codex.

### 5. library (custom) — your procedural skills

Reusable procedures, workflows, scripts. If the knowledge is "how to do X", it belongs here, not in codex.

- **Lifespan**: forever.
- **Scope**: self (until promoted).
- **Write cost**: moderate — a skill is a document with YAML frontmatter, prose, optional scripts/references/assets.
- **Catalog vs. body**: the system prompt carries only the catalog (name, description, path). Full SKILL.md bodies load on demand.
- **Discovery paths**: `.library/intrinsic/` (kernel-bundled), `.library/custom/` (agent-written), plus user-declared paths in `init.json`.
- **Tool**: create via `write`/`edit`, then `system(refresh)` to reload the catalog.

Graduation: if you solved something non-trivial and might need it again, skill it.

### 6. shared library — collective competence

Skills promoted to `../.library_shared/`. Every agent in the network can load them.

- **Lifespan**: forever.
- **Scope**: the whole network.
- **Write cost**: high — others will read it.
- **Command**: `cp -r .library/custom/<name> ../.library_shared/<name>` then `system(refresh)`.

### Summary table

| Layer | Lifespan | Scope | Write cost | Storage |
|-------|----------|-------|------------|---------|
| context | this molt | self | free | `history/chat_history.jsonl` |
| pad | long-lived | self | cheap | `system/pad.md` + `pad_append.json` |
| lingtai | long-lived | self | cheap (full rewrite) | `system/lingtai.md` |
| codex | forever | self | moderate (bounded) | `codex/codex.json` |
| library (custom) | forever | self | moderate | `.library/custom/*/SKILL.md` |
| shared library | forever | network | high | `../.library_shared/*/SKILL.md` |

## The Flow in One Sentence

**context** is what you're thinking right now; if any of it survives the turn, it goes to **pad**; if it survives *you*, it goes to **lingtai** (identity), **codex** (facts), or **library** (procedures); if it would help others, it goes to the **shared library**.

**Promotion is always agent-directed.** There is no auto-promotion. The hierarchy describes *where knowledge settles* based on your deliberate tool calls.

---

## Psyche Tool Dispatch

The `psyche` tool manages the first three layers and context reset. It overrides the kernel-level `eigen` tool at the wrapper level.

| Call | What happens |
|------|-------------|
| `psyche(lingtai, update, content=...)` | Write to `system/lingtai.md` → auto-reload into prompt |
| `psyche(lingtai, load)` | Reload lingtai into prompt (covenant + lingtai sections) |
| `psyche(pad, edit, content=..., files=[...])` | Write `content` + embedded files → auto-reload |
| `psyche(pad, append, files=[...])` | Pin read-only file references into `pad_append.json` → reload |
| `psyche(pad, load)` | Reload pad section (base pad + pinned appendages) |
| `psyche(context, molt, summary=...)` | Delegate to `eigen(context, molt)` — clear chat, archive history, rebuild session |

Psyche registers a `post_molt_hook` that auto-reloads lingtai + pad into the fresh session after every molt.

---

## Auxiliary Layers

Three additional stores live alongside the hierarchy but don't follow the promotion model. They are audit/reflection/visibility surfaces, not knowledge levels you write to deliberately.

### Soul — introspection machinery

Two mechanisms, one automatic and one on-demand:

| Mode | Trigger | Tool action | Output |
|------|---------|-------------|--------|
| **心流 (flow)** | Agent enters IDLE → waits `soul_delay` seconds (default 120s) → auto-fires | None — not a tool call | Collects recent assistant text + thinking → sends to persistent soul session → response injected into inbox as `[心流]` prefixed message |
| **自省 (inquiry)** | Agent calls `soul(inquiry, inquiry="question")` | `inquiry` | Clones conversation (text + thinking only) → one-shot session → answer returned in tool result |

**Soul session properties:**
- **No tools** — pure text.
- **Survives molt** — soul conversation history is preserved; only the read cursor resets.
- **Gradual forgetting** — `_trim_soul_session()` prunes oldest entries when token budget is exceeded.
- **Persistence** — `logs/soul_flow.jsonl` + `logs/soul_inquiry.jsonl`.

### Token ledger — audit trail

`logs/token_ledger.jsonl` — append-only, one line per LLM call: `{ts, input, output, thinking, cached}`. Not a knowledge layer, but critical for understanding context pressure and cost. On startup, lifetime totals are restored by summing the ledger.

### Time veil — visibility layer

When `manifest.time_awareness: false`, the kernel scrubs all timestamp-bearing fields from the LLM surface. On-disk state is unchanged — only what the LLM sees is veiled. This allows time-blind operation when needed.

---

## Daemon System (分神)

Daemons are disposable LLM sessions that share the working directory but have no persistence. Use them to offload noisy, context-heavy work.

### Daemon vs. Avatar

| Property | Daemon (分神) | Avatar (化身) |
|----------|--------------|-------------|
| Process | shares main process (thread) | independent OS process |
| Persistence | one-shot — gone when done | long-lived with independent memory |
| Memory | none — no state across sessions | independent lingtai/pad/codex |
| Concurrency | max 4 by default (configurable) | unlimited |
| Communication | `ask` / `reclaim` (synchronous) | mail (asynchronous) |

### Operations

| Action | Description |
|--------|-------------|
| `emanate` | Create one or more daemon sessions with specified tasks and tools |
| `list` | Show all active daemons and their status |
| `ask` | Send a follow-up message to a specific daemon |
| `reclaim` | Terminate all active daemons |

Each daemon gets its own LLM session (`tracked=False`), shares the working directory for file access, but is invisible to the main agent's context. Daemon output is capped at ~2000 words; for detailed output, instruct the daemon to write to a file and retrieve it afterward.

---

## Network-Topology Layer

A separate kind of knowledge: **who knows what, who to reach, who works on what**. This is relational, not factual, and does not live in a single store. It is spread across multiple stores:

| Knowledge | Where it lives | Example |
|-----------|---------------|---------|
| Peer addresses and names | `mailbox/contacts.json` | `{address, name, note}` with permanent `agent_id` in note |
| Stable collaborator roles | **lingtai** (identity-level) | "I work with X on Y, their strength is Z" |
| Active delegations | **pad** (task-scoped) | "I spawned avatar-3 to do Q, waiting for reply" |
| Communication history | **mail history** (implicit trail) | inbox/sent/archive folders |

**Where to write network knowledge** — decide by lifespan:
- A peer's permanent `agent_id` and role → **contacts** (address book).
- A stable collaborator's specialty → **lingtai** (identity-level).
- A mission handed to an avatar → **pad** (active, task-scoped).
- A one-off exchange → let it stay in mail history; don't copy it anywhere.

The network topology is discovered at runtime by `network.py` through four passes: discover nodes → read delegate ledgers → read contacts → scan mail history. The result is a graph with three edge types: **avatar edges** (parent-child), **contact edges** (address book), and **mail edges** (communication history).
