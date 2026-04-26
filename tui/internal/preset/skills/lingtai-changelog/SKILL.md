---
name: lingtai-changelog
description: Chronicle of breaking changes, renames, and migrations in the LingTai system. Load this when you encounter unfamiliar names, deprecated references, or confusion about what things are called and where they live. Entries are prepended — newest first.
version: 1.0.0
---

# LingTai Changelog

A living chronicle of system-level changes that affect how you work. When something doesn't match what you remember, check here first.

---

## 2026-04-26 — Network exports drop chat_history; clones know they are clones

### What changed

`lingtai-recipe` skill bumped to v3.1. The network-export flow (`/export network`) now does three new things to address the "exported network thinks it is the original" problem:

1. **Strips per-agent `history/chat_history.jsonl`, `history/soul_history.jsonl`, and `history/soul_cursor.json`.** Previously these were copied verbatim, so a cloned agent woke up with the original's full LLM transcript and believed it was still in the same conversation. Now they are removed during `scrub_ephemeral.py`, and the recipe's `greet.md` is repositioned as the network's 「前尘往事」 (charge) — a tight retrospective the cloned agent reads on first launch to learn who it was.
2. **Stamps each agent's `system/brief.md` with an "EXPORTED SNAPSHOT" banner** via the new `scripts/mark_export_source.py`. brief.md sits at the top of the system prompt, so the banner reaches the agent on its first turn after rehydration.
3. **Writes `.exported-from`** at the bundle root recording bundle name, source URL, and export timestamp. Survives `git add .` — proof of origin for downstream forks and a sanity check for "is this a snapshot?"

Also stripped now: `.lingtai/<agent>/.library/intrinsic/` (kernel-managed, identical across installs — recipient kernel rebuilds it on rehydration; was bloating exports with hundreds of duplicated `SKILL.md` files).

### What you should do

If you are about to export a network, follow `lingtai-recipe`'s `assets/export-network.md` end to end — Step 1c now runs `mark_export_source.py`, Step 5d frames `greet.md` as 「前尘往事」 instead of a generic welcome, and Step 5i drafts `README.md` via `scripts/generate_readme.py`. The privacy scanner (`privacy_scan.py`) also folds `.lingtai/`-runtime absolute-path warnings into a single rolled-up count by default — pass `--no-fold` if you want the full firehose.

If you cloned a network and notice the EXPORTED SNAPSHOT banner in your brief, you are in a clone of `<name>`. The original network you remember continues elsewhere. Read `greet.md` for context on who you used to be.

### Why

Driven by feedback from the `quant_company` export on 2026-04-25: the human noticed the cloned orchestrator did not know it was a clone — it had the full chat history and treated the new repo as if it were the original network's working directory. The root cause was that `chat_history.jsonl` was kept by default. Fix: strip the transcript, let `greet.md` serve as the molt-style charge, and stamp the agent's brief so the awareness reaches the very first turn.

---

## 2026-04-21 — Pseudo-agent outbox subscription

### What changed

The human folder (and any other pseudo-agent — a folder with `.agent.json` declaring `admin: null` and no running process) now sends mail via its own outbox instead of having the TUI write directly into the recipient's inbox. Real agents running in the same project subscribe to pseudo-agent outboxes via a new `pseudo_agent_subscriptions` field in `init.jsonc` and poll them on their normal mail-receive loop. Subscription default: `["../human"]`.

### How the pickup works

When your mail-receive loop runs, for each subscribed path:
1. Scan `<path>/mailbox/outbox/`.
2. For each UUID folder whose `message.json` has `To:` matching your address, atomically `os.Rename` the folder from `<path>/mailbox/outbox/<uuid>/` to `<path>/mailbox/sent/<uuid>/`.
3. Ingest the claimed message into your normal inbox pipeline.

If the rename fails (another subscriber won the race), silently skip.

### What you should do

Nothing — this is mechanical runtime behavior; your LLM never sees the subscription list or the polling. But if mail from the human stops arriving, check that your `init.jsonc` has `pseudo_agent_subscriptions: ["../human"]` and that `../human/mailbox/outbox/` is readable.

### Why

Plugins (Claude Code, etc.) that run inside a real agent can now send mail "from the human" by writing into the human's outbox, without reproducing the TUI's delivery logic or knowing recipient filesystem paths. The mechanism is pull-based, so any subscriber — local real agent, or a remote real agent whose kernel polls a mirrored outbox via postman/SSH — picks up mail the same way.

---

## 2026-04-20 — Library capability redesigned

Breaking changes for agents:

- **Tool actions removed**: `library(action='register')` and `library(action='refresh')` no longer exist.
- **New tool action**: `library({"action": "info"})` returns the meta-skill guide plus a runtime health snapshot. Call it to understand your library.
- **Per-agent `.library/`**: every agent now has its own `<agent>/.library/{intrinsic,custom}/`. The network-shared library moved from `.lingtai/.library/` to `.lingtai/.library_shared/` (TUI migration v18).
- **Author a skill**: write it to `.library/custom/<name>/SKILL.md`, then `system({"action": "refresh"})`. No more register step.
- **Publish to network**: `cp -r .library/custom/<name> ../.library_shared/<name>`. No more register step.
- **Loading into working memory**: use `psyche({"object": "pad", "action": "append", "files": ["<location>"]})` to pin a skill into the pad across turns.

See the `library-manual` capability manual for the full workflow.

---

## 2026-04-16 — Addon Secrets Move to Admin's `.secrets/`

### What changed

Addon configs (IMAP, Feishu, Telegram, WeChat) can now live inside the orchestrator agent's own working directory at `.secrets/<addon>.json`, in plaintext JSON without `*_env` indirection. The old project-shared path keeps working — nothing is forced to move.

### New path

| Addon | New path (inside admin's working dir) |
|-------|----------------------------------------|
| imap | `.secrets/imap.json` |
| feishu | `.secrets/feishu.json` |
| telegram | `.secrets/telegram.json` |
| wechat | `.secrets/wechat.json` (+ `.secrets/credentials.json` after QR login) |

### Old path (still works, no migration required)

| Addon | Old path (relative to project root) |
|-------|--------------------------------------|
| imap | `.lingtai/.addons/imap/config.json` |
| feishu | `.lingtai/.addons/feishu/config.json` |
| telegram | `.lingtai/.addons/telegram/config.json` |
| wechat | `.lingtai/.addons/wechat/config.json` |

### Why

Addons are an admin-only responsibility — avatars must not configure them. Keeping addon secrets inside the orchestrator's own directory makes that ownership explicit, removes the `*_env` / `.env` indirection, and keeps each agent's secrets self-contained.

### What you should do

- **New setups:** use the new path. See `lingtai-imap-setup`, `lingtai-feishu-setup`, `lingtai-telegram-setup`, or `lingtai-wechat-setup` skills for full instructions.
- **Existing setups:** leave them alone unless the human asks to migrate. Only the `lingtai-imap-setup` skill ships migration instructions; for other addons, the human should migrate manually.
- **Avatars:** you should never be configuring addons. If an addon tool is missing from your tool list, that is by design — ask your orchestrator.

---

## 2026-04-13 — The Pad / Codex / Library Rename

### What changed

Three core concepts were renamed to better reflect what they actually are:

| Before | After | What it is | System prompt presence |
|--------|-------|-----------|----------------------|
| `memory` (psyche sub-action) | **pad** | Your working notes — always in front of you | FULL — entire content injected |
| `library` (tool) | **codex** | Your personal knowledge archive — structured entries you curate | SEMI — summaries, load on demand |
| `skills` (capability) | **library** | The skill library — a shelf of playbooks you consult | ROUTING — XML catalog only |

### New names in each language

| Level | English | 中文 | 文言 |
|-------|---------|------|------|
| 1 | pad | 手记 | 简 |
| 2 | codex | 典集 | 典 |
| 3 | library | 藏经阁 | 藏经阁 |

### What moved on disk

| Old path | New path |
|----------|----------|
| `system/memory.md` | `system/pad.md` |
| `system/memory_append.json` | `system/pad_append.json` |
| `library/library.json` | `codex/codex.json` |
| `.lingtai/.skills/` | `.lingtai/.library/` |

A TUI migration (m015) handles the filesystem renames automatically for existing agents.

### Tool call changes

**Psyche / eigen:**
```
# Old:
psyche(memory, edit, content=...)
psyche(memory, load)
psyche(memory, append, files=[...])

# New:
psyche(pad, edit, content=...)
psyche(pad, load)
psyche(pad, append, files=[...])
```

**Knowledge archive (was library, now codex):**
```
# Old:
library(submit, title=..., summary=..., content=...)
library(filter, pattern=...)
library(view, ids=[...])
library(export, ids=[...])

# New:
codex(submit, title=..., summary=..., content=...)
codex(filter, pattern=...)
codex(view, ids=[...])
codex(export, ids=[...])
```

**Skill library (was skills, now library — then redesigned 2026-04-20):**
```
# Old (pre-2026-04-13):
skills(action='register')
skills(action='refresh')

# Intermediate (2026-04-13 rename, removed 2026-04-20):
library(action='register')
library(action='refresh')

# Current (2026-04-20+):
library({"action": "info"})          # health check + guide
system({"action": "refresh"})        # rescan library paths
```

### Why the rename

The old names were misleading:

- **"memory"** implied persistence and recall, but it's really a scratchpad — working notes you jot down, always visible, always editable. **Pad** says what it is.
- **"library"** implied a public reference you browse, but it's really your personal knowledge manuscript — structured entries you curate over time, heavy and durable. **Codex** captures the weight and personal ownership.
- **"skills"** were already called "skills" inside, but the container was also called "skills." Now the container is a **library** — a library of skills. You walk to the 藏经阁 (hall of scriptures), find the right 功法 (technique manual), and bring it back to your desk.

The three levels form a gradient of context presence:
1. **Pad** — hot, always in your prompt, your working surface
2. **Codex** — warm, structured entries you pull into your pad when needed
3. **Library** — cold, an XML routing table; you load a skill's full SKILL.md on demand

### If you see old names

If you encounter `system/memory.md`, `library/library.json`, `.skills/`, or tool calls using the old names in existing files, notes, or emails from before this rename — they refer to `pad`, `codex`, and `library` respectively. The TUI migration renamed the files, but references in your own pad notes, codex entries, or old email may still use the old names.
