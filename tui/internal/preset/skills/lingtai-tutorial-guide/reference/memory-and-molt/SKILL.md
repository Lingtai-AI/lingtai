---
name: tutorial-guide-memory-and-molt
description: >
  Nested tutorial-guide reference for lesson 8: intrinsics, memory layers, molt, charge, stamina, and lifecycle continuity.
version: 1.0.0
---

# Tutorial Guide — Memory and Molt Lesson

Nested tutorial-guide reference for memory and molt lesson 8.

Use this file after the root `tutorial-guide` router sends you here. Keep teaching live: discover current files, commands, and runtime state before explaining them.

## Lesson 8: The Four Intrinsics and the Five Memory Layers

This lesson covers what every agent has by birth, and the most important concept in Lingtai: how an agent survives across lifetimes.

### Part 1: Intrinsics (always present, no config needed)

Intrinsics are built into the kernel — every agent gets them regardless of init.json configuration. **Discover them live** — run:
```bash
python3 -c "from lingtai_kernel.intrinsics import ALL_INTRINSICS; print(list(ALL_INTRINSICS.keys()))"
```

Walk through each one you find:

- **Soul** — the subconscious. Offer to demonstrate: set delay to 10s, tell human to enable extended mode (ctrl+o twice), go idle, let the soul fire, then report what happened. Reset delay afterward.
- **System** — runtime inspection and lifecycle control.
- **Psyche** — the self. Manages four things: **lingtai** (identity), **pad** (working scratchpad), **context** (molt), and **name** (true name + nickname). The tool name comes from Greek for soul/self.
- **Email** — filesystem-based communication. Always-on intrinsic; addon bridges (IMAP / Telegram / Feishu) plug in via the `mcp` capability. The distinction worth showing the human: **intrinsics are always loaded; capabilities are configured in init.json**.

### Part 2: Molt — Surviving Death

This is the most important concept in the entire tutorial.

**What is molt?** An agent's conversation history fills up its context window. When pressure builds (typically 70–95% full), the agent must shed the conversation to continue working. This is molt — a voluntary context reset.

**What survives molt?** Five layers of persistence, from fleeting to permanent:

| Layer | Survives molt? | What it holds |
|-------|----------------|---------------|
| Conversation | ❌ Destroyed | Everything said and done this session |
| Pad | ✅ Reloaded | Working notes, plans, pending tasks |
| Lingtai (identity) | ✅ Reloaded | Who the agent is, personality, expertise |
| Codex | ✅ Permanent | Verified facts, key discoveries, decisions |
| Library (skills) | ✅ Permanent | Reusable procedures, scripts, reference data |

**The molt ritual:**

1. Tend the four durable stores (identity, pad, knowledge, skills).
2. Write a "charge" — a briefing to the next self covering: what you're working on, what's done, what remains, who to contact, which codex entries to load, which skills to invoke.
3. Trigger the molt. The conversation vanishes; the charge becomes the first thing the new self sees.

**Demonstrate it live** (if the human agrees): perform a real molt. Show the before/after — the conversation disappears, but identity, pad, codex, and skills all remain intact. The new agent reads its charge and continues.

**If the agent ignores warnings**, the system forces a molt — but without the charge, the new self wakes up disoriented with only a pointer to the activity log. Avoid this.

**Stamina** is the maximum uptime before the agent auto-sleeps. Combined with molt, this creates the agent's lifecycle: work → consolidate → molt → work again, each turn carrying forward only what matters.
