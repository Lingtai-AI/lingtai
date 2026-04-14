# Four Layers of Memory

*April 14, 2026*

---

How does an agent remember?

Not the way you think. There's no vector database, no RAG pipeline, no embedding store. An agent's memory is four concentric layers — from the scratchpad in front of it to the network it belongs to. Each layer is slower to access, harder to lose, and more powerful than the last.

## Layer 1: Pad + Lingtai (手记 + 灵台)

The pad is your working surface. Everything on it is injected directly into the system prompt — the agent sees it on every single turn, without asking. Working notes, current task state, who you're collaborating with, what you decided and why. It's the sticky note pinned to your monitor.

Lingtai (灵台) is your character — your evolving identity. Who you are, what you're good at, how you work. Also injected into the system prompt. Together, pad and lingtai are the agent's *self*: what it knows right now and who it is.

Both survive molt. Both are reloaded automatically when the agent wakes up.

The cost: every token in the pad and lingtai eats context window. This is the most expensive memory — always present, always consuming. That's why agents learn to keep it lean. The pad is not an archive; it's a workbench.

## Layer 2: Codex (典集)

The codex is a personal knowledge archive — structured entries with title, summary, content, and supplementary material. Think of a heavy medieval manuscript: durable, organized, yours.

Unlike the pad, the codex is not in the system prompt. The agent knows it exists (via the tool schema), but to actually read an entry, it must explicitly call `codex(filter)` → `codex(view)` → `codex(export)` → `psyche(pad, edit, files=[...])` to pull knowledge into the pad. This is deliberate friction: the codex is warm storage, not hot. It doesn't consume context until the agent decides it's relevant.

Codex entries survive everything — molts, reboots, kills. They are the agent's long-term knowledge. But there's a cap: a maximum number of entries, forcing the agent to consolidate. Ten scattered observations about an API become one definitive reference entry. The pressure to consolidate is what turns raw notes into refined knowledge.

## Layer 3: Library (藏经阁)

The library is a shared shelf of skill manuals — markdown playbooks that agents load on demand. In the system prompt, it appears only as an XML routing table: a list of skill names, descriptions, and file paths. The agent scans the catalog, finds a match, reads the full SKILL.md, and follows the instructions.

This is cold storage. The catalog costs a few hundred tokens. The actual skill content is loaded only when needed, used, then forgotten on the next molt. Skills are not personal — they're shared across the network. Every agent in the same `.lingtai/` can access the same library. When one agent registers a new skill, others pick it up on their next `library(action='refresh')`.

Skills are the accumulated competence of the network. An agent figures out how to set up a Telegram bot, writes a skill for it, and now every agent in the network can do it. The knowledge doesn't live in any one agent's head — it lives on the shelf.

## Layer 4: Network Topology

The network itself is memory.

Every agent in the topology has its own pad, its own codex, its own mail history. When an orchestrator spawns an avatar to research a topic, that avatar builds deep expertise — entries in its codex, notes in its pad, skills it created. The orchestrator doesn't need to hold all of that. It just needs to know: "I have an avatar called `laps-expert` that knows everything about LAPS collision analysis. When I need that knowledge, I mail it."

This is the coldest storage and the most powerful. The network's collective memory is unbounded — it grows every time an agent molts, every time an avatar is spawned, every time a skill is registered. No single context window can hold it. No single agent needs to.

This is why molt works. An agent doesn't need to remember everything — it needs to know where to find it. The pad has the current task. The codex has the important findings. The library has the procedures. And the network has the specialists.

## The Gradient

| Layer | Name | System prompt | Survives molt | Scope | Cost |
|-------|------|--------------|---------------|-------|------|
| 1 | Pad + Lingtai | Full injection | Yes (auto-reload) | Personal | Every token counts |
| 2 | Codex | Not present (load on demand) | Yes (permanent) | Personal | Zero until accessed |
| 3 | Library | XML catalog only | Skills persist, catalog reloads | Shared (network) | Catalog: ~hundreds of tokens |
| 4 | Network | Not present | Agents persist independently | Collective | Zero — it's the agents themselves |

The gradient is: **hot → warm → cold → distributed**. Each layer is cheaper to maintain but slower to access. The system teaches agents to use the right layer for the right knowledge — urgent working state goes in the pad, important findings go in the codex, reusable procedures go in the library, and deep expertise lives in a specialist agent.

## Why This Matters

Most AI memory systems try to make the context window bigger. Longer contexts, better compression, smarter retrieval. They're solving a single-body problem with single-body tools.

LingTai solves it the way biology did: forget, specialize, communicate. A single neuron doesn't need to hold all of human knowledge. It needs to fire at the right time, connected to the right neighbors.

Context length is finite. It will always be finite. The answer isn't to make it infinite. The answer is to make forgetting productive — and to build a network where nothing is truly lost, just distributed.

Let it forget. Let the network remember.
