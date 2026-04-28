---
name: lingtai-changelog
description: Chronicle of breaking changes, renames, and migrations in the LingTai system. Load this when you encounter unfamiliar names, deprecated references, or confusion about what things are called and where they live. Entries are prepended — newest first.
version: 1.0.0
---

# LingTai Changelog

A living chronicle of system-level changes that affect how you work. When something doesn't match what you remember, check here first.

---

## 2026-04-28 — Pending-notifications meta field on every text input and tool result

### What changed

Every text input and every tool result now carries a `pending_notifications` field whenever your runtime inbox has queued messages that haven't been delivered yet. The field has the shape:

```
"pending_notifications": {
  "count": <int>,
  "previews": [<str>, ...]   // one entry per queued message, each ≤50 chars
}
```

Previews are non-destructive snapshots of the queued messages — flattened to a single line and truncated. The full text still arrives at the natural turn boundary (drained by `_concat_queued_messages` when control returns to `_run_loop`).

In your text-input prefix you'll see a second line under the time/context line:

```
[Current time: ... | context: ...]
[Pending notifications (3) — full text arrives after the current tool cascade:
  - [system] New message in mail box. Address: alice. Subject: Quick que...
  - [soul flow] Maybe pause and consider why...
  - [system] New message in mail box. Address: bob. Subject: Status update.]
```

In tool results, the same dict appears as a JSON field alongside `current_time`, `context_usage`, etc.

### What you should do

- When you see `pending_notifications` mid-cascade, you can choose to (a) finish your current task — the full notifications will arrive after the cascade ends, or (b) pivot to handle them now if any look urgent.
- Don't try to "drain" them yourself with a tool call — there's no such tool. The full messages are queued and will be delivered automatically at the next turn boundary.
- If the cascade is short and you'd rather see the full content first, just produce a text-only response (no tool_calls) — that ends the cascade and the next turn will receive all queued notifications.

### Why

Previously, runtime notifications (incoming mail, soul whispers, addon notifies) sat silently in the inbox queue during a tool cascade and only became visible to you AFTER the cascade ended and control returned to the outer loop. For long cascades that meant minutes of obliviousness to mail that had already arrived. The new meta field gives you early awareness without breaking the chat-completions invariant (you can't inject `user[message]` between `assistant[tool_calls]` and `tool[results]`, but you CAN ride the snapshot inside the tool result itself).

The full delivery still happens at the same natural boundary as before — this change is purely additive awareness, not a behavioral change to delivery timing.

---

## 2026-04-28 — `mail`/`email` mode renamed: `rel` → `peer`; SSH mode removed

### What changed

The `mode` parameter on `mail` (kernel intrinsic) and `email` (capability) accepted three values: `rel`, `abs`, `ssh`. It now accepts two: **`peer`** and **`abs`**.

- `rel` → renamed to `peer`. Same semantics: resolve the address as a bare working-directory name against your `.lingtai/` network folder. Default mode — you almost never need to set it explicitly.
- `abs` → unchanged. Treat the address as a literal absolute filesystem path to another agent's working directory. Use this only when the recipient lives in a *different* `.lingtai/` network on the same machine.
- `ssh` → **deleted**. The `_deliver_ssh()` helper, the `if mode == "ssh"` dispatch branch, and the schema enum value are all gone. SSH-based cross-machine delivery was premature and is being superseded by the planned Postman/IPv6 mesh.

The `email` capability now also exposes `mode` (it previously did not — it was structurally locked to peer-only). The schema field is inherited from kernel `mail.mode_field()` so the two tools cannot drift.

### What you should do

- Just call `mail(action="send", address="本我", message="...")` — you don't need to think about `mode` at all for any agent in your own network. The default is `peer`.
- If you find yourself wanting to mail across networks (rare — usually a sign you should be coordinating through a shared agent), pass `mode="abs"` with the recipient's full working-directory path.
- If you have any procedure or skill content that says `mode="rel"`, update it to `mode="peer"` (or just drop the explicit mode — it's the default).
- If you have any procedure that mentions SSH-based mail delivery, delete that — there's no replacement yet.

### Why

`rel` was a path-resolution term that misled agents into thinking about *path semantics* when the actual concept is *network topology* — "is this person in my network or somewhere else." `peer` matches the mental model agents already use (the `from` field in mail, the network listing in your brief). The SSH path was untested in the wild, hardcoded a transport into the dispatch loop instead of going through `_mail_service` like every other mode, and is being replaced by a properly-designed mesh transport.

### Migration safety

No backward compatibility — `mode="rel"` and `mode="ssh"` are now hard rejected by the validator with a clear error message. Outbox payloads on disk cannot carry `_mode` (it's stripped before persist), so replays default to `peer` cleanly. Self-send still short-circuits before any resolve step and is mode-agnostic.

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

### Same-day addendum: scope disambiguation in nested-recipe projects

A second failure mode surfaced from the `impersonate-meta` export: the project itself was *seeded from a recipe* (a methodology recipe with its own `.recipe/` at the project root) and *also contained a network* (the agents living in `.lingtai/`). Both export sub-guides were ambiguous about which artifact to ship — the agent could end up republishing the seeding methodology recipe instead of distilling the actual inner network. Both `assets/export-network.md` and `assets/export-recipe.md` now open with a "First: which 'network' / 'recipe' does the human mean?" disambiguation block, and `export-network.md`'s Step 5 explicitly says the launch recipe is NEW (with replace-vs-relocate options for any pre-existing root `.recipe/`).

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
