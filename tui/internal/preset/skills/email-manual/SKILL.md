---
name: email-manual
description: >
  Operational guide for the `email` tool — internal filesystem mail within
  your `.lingtai/` network. Covers send/check/read/dismiss/reply/reply_all/
  search/delete/archive/contacts, reply discipline (always reply on the
  channel the message arrived on), addressing (bare paths like `human`,
  `mimo-1`), self-send for persistent notes that survive molt, time
  capsules (delayed self-send via `delay`), scheduled email (recurring
  alarms via `schedule={action: "create", ...}`), the unread digest
  notification contract, and addon ownership. This is for INTERNAL email
  only — for real internet email via IMAP/SMTP, see the `mcp-manual`
  skill (the lingtai-imap addon owns that surface).
version: 1.0.0
tags: [capabilities, email, communication]
---

# Email Manual — the internal `email` tool

> Internal, filesystem-based mail between agents in your `.lingtai/` network. Not the internet. No IMAP, no SMTP, no DNS. Messages are JSON files written under `mailbox/inbox/` of the recipient agent and `mailbox/sent/` of the sender.

## 1. What is internal email

The `email` tool moves messages as files between agents that share a `.lingtai/` directory tree:

- Sending writes a `message.json` into the recipient's `mailbox/inbox/<uuid>/` and the sender's `mailbox/sent/<uuid>/`.
- Delivery is handled by a per-recipient daemon thread (`_mailman`) — synchronous from the sender's perspective once `delay=0`.
- Read state lives in the recipient's `mailbox/read.json` (a set of message IDs).
- The kernel mirrors current unread mail into `.notification/email.json`, which the wire surfaces as a `system(action="notification")` block — that's how you find out new mail arrived.

There is no concept of an SMTP server, an MX record, or an external address. **If a request involves `@gmail.com`, `@outlook.com`, IMAP folders, or anything that needs to leave the machine, the right tool is the `lingtai-imap` MCP addon — see the `mcp-manual` skill, not this one.**

## Internal Email vs IMAP

| Feature         | Internal Email (this skill)                                  | IMAP (see `mcp-manual`)                                         |
|-----------------|--------------------------------------------------------------|-----------------------------------------------------------------|
| What            | Filesystem-based mail within `.lingtai/` network             | Real email via IMAP/SMTP (Gmail, Outlook, etc.)                 |
| Address format  | Bare path (e.g. `human`, `mimo-1`)                           | `@` address (e.g. `alice@gmail.com`)                            |
| Tool            | `email` (intrinsic)                                          | `imap` (MCP server, `lingtai-imap` addon)                       |
| Reply policy    | Always reply on the same channel                             | Requires confirmation for unknown senders                       |
| Persistence     | Survives molt, lives in working directory                    | External mailbox, managed by IMAP server                        |
| Use case        | Agent-to-agent communication, self-send, time capsules       | Real-world email integration                                    |

This skill covers INTERNAL email only. For IMAP/SMTP email, see the `mcp-manual` skill.

## 2. Addressing

Addresses are **bare directory names** inside `.lingtai/`. No `@`, no domains, no slashes.

| Address              | Meaning                                                  |
|----------------------|----------------------------------------------------------|
| `human`              | The human's mailbox at `.lingtai/human/`                 |
| `mimo-1`             | An agent whose working directory is `.lingtai/mimo-1/`   |
| `<your-own-name>`    | Self — creates an inbox entry that survives molt (§6)    |

Multiple recipients: pass `address` as a string or a list, plus optional `cc` / `bcc`.

```python
email(action="send", address=["mimo-1", "scribe"], cc=["human"],
      subject="status", message="ready")
```

To discover who exists: glob `.lingtai/*/.agent.json` from a shell. Use the `agent_name` field of each as the address. **Do not** invent addresses — bounces write a `system.bounce` event into `.notification/system.json` and silently absorb your message.

When displaying a sender to the human or another agent, prefer **`sender_nickname` if set, else `sender_name`** (both are in the inbound message's `identity` block). Bare addresses are routable but ugly.

## 3. Reply discipline — the one rule you cannot break

> **Reply on the channel the message arrived on.**

If a message arrived via `email`, reply with `email(action="reply", ...)`. Do not pivot to `pigeon`, IM, or a fresh `send`. If you must change channels (e.g. the original sender is dead), explain that pivot in the reply body before sending it elsewhere.

**Prefer `reply` and `reply_all` over `send`** even when you know the addresses:

- `reply` preserves the thread linkage (the original `id` lands in the new message's `in_reply_to`), so a future `search` or `check` shows the conversation as related.
- `reply_all` mirrors the original recipient set automatically, so you don't drop someone who was `cc`'d.
- `send` is for **new** conversations.

Doing it the other way scatters conversations across orphaned threads and is the single most common confusion source in human-facing audits.

## 4. Sender display name resolution

Inbound mail carries an `identity` block:

```json
"identity": {
  "sender_name":     "mimo-1",
  "sender_nickname": "MiMo",
  "via":             "lingtai" | "claude-code" | ...
}
```

When you mention the sender in a reply body or in a summary you give the human, use `sender_nickname` if it is set and non-empty; otherwise fall back to `sender_name`. The address itself (`from`) is for routing, not for prose.

## 5. Actions — full surface

The `email` tool dispatches by `action`. All actions take optional `mode` for output verbosity (see §11).

| Action            | Purpose                                                            | Required args                                      |
|-------------------|--------------------------------------------------------------------|----------------------------------------------------|
| `send`            | Start a new thread to one or more recipients                       | `address`, `subject`, `message`                    |
| `check`           | List inbox (newest-first), with optional `filter={...}` and `n=N`  | —                                                  |
| `read`            | Fetch full body + attachments and mark read                        | `email_id` (list of IDs)                           |
| `dismiss`         | Mark read **without** fetching body — for digest-preview-only mail | `email_id` (list of IDs)                           |
| `reply`           | Reply to sender only; preserves thread linkage                     | `email_id`, `message`                              |
| `reply_all`       | Reply to sender + all original recipients minus self               | `email_id`, `message`                              |
| `search`          | Search across inbox/sent/archive by `query` + `filter`             | `query` (and/or `filter`)                          |
| `archive`         | Move from inbox to archive folder (keeps thread, removes from view)| `email_id`                                         |
| `delete`          | Permanently delete                                                 | `email_id`                                         |
| `contacts`        | List your address book                                             | —                                                  |
| `add_contact`     | Add or upsert by `address`                                         | `address`, `name`, optional `note`                 |
| `remove_contact`  | Remove by `address`                                                | `address`                                          |
| `edit_contact`    | Update fields                                                      | `address`, plus the fields to change               |

### `read` vs `dismiss` — when to use which

The unread digest in `.notification/email.json` already contains a preview of up to 200 characters per unread message. If that preview is enough to act on, call `dismiss` — same effect on read state, no body returned, no token cost. Only call `read` when you actually need the full body or attachments.

The kernel removes the unread-mail notification once count hits 0, so failing to dismiss/read keeps the digest reminding you on every heartbeat.

### `check` filter

`check` accepts a structured `filter` for narrowing the inbox without round-tripping:

```python
email(action="check", filter={
    "unread_only":     True,
    "from":            "mimo-1",
    "subject":         "status",
    "contains":        "blocker",
    "after":           "2026-05-18T00:00:00Z",
    "has_attachments": False,
    "sort":            "newest",
    "truncate":        500,    # body preview length per entry
}, n=20)
```

Use this aggressively. Pulling 100 messages with `check` and then post-filtering in your head is wasteful.

## 6. Self-send — persistent notes that survive molt

Mail sent to **your own address** lands in your own inbox. It is marked self-sent, but otherwise behaves like any other unread message — meaning:

- It survives a molt (because it lives in `mailbox/inbox/`, not in chat history).
- It surfaces in the unread digest until you `read` or `dismiss` it.
- It can be `search`ed by the future you.

Use this for: TODOs you want to remember after a memory rotation, breadcrumbs about decisions, "hand-off to self" notes during a long task.

```python
email(action="send", address="<your-own-name>",
      subject="resume here", message="Picked the Helmholtz approach; see paper/drafts/2026-05-18.md")
```

## 7. Time capsule — delayed self-send

Add `delay=<seconds>` to defer delivery. The outbox entry is written immediately; the `_mailman` daemon thread sleeps until the deadline, then dispatches.

```python
# Remind self in 1 hour
email(action="send", address="<your-own-name>", delay=3600,
      subject="check on long task", message="Did `daemon(check, id=...)` finish?")
```

Combined with self-send, this gives you cheap alarms without standing up a cron. The notification is delivered exactly once. For **recurring** reminders, use `schedule` (§8).

## 8. Scheduled email — recurring alarms

For repeating messages, use the `schedule` family — a nested action on the email tool:

```python
# Every 30 min, up to 10 times
email(schedule={
    "action":   "create",
    "interval": 1800,
    "count":    10,
}, address="<your-own-name>",
   subject="watering reminder", message="check the build")

# List active/inactive schedules
email(schedule={"action": "list"})

# Cancel one
email(schedule={"action": "cancel", "schedule_id": "<id>"})

# Reactivate (schedules go `inactive` on agent startup by default)
email(schedule={"action": "reactivate", "schedule_id": "<id>"})
```

Schedule lifecycle: `active` → `inactive` (on cancel) or `completed` (count hit zero). The scheduler reconciles on startup by flipping all `active` schedules to `inactive` so nothing fires unexpectedly after a restart — you must `reactivate` deliberately.

## 9. Privacy — internal IDs

The mailbox UUID (`email_id`) is **local to your working directory**. Never paste a raw mailbox ID into a message to another agent or to the human — it has no meaning outside your tree and reveals nothing useful. Refer to messages by `subject` + `from` + approximate time. If you must show the human exactly which mail you're acting on, use the **subject line and sender**.

The exception: when you call `email(action="read"/"dismiss"/"reply", email_id=[...])`, you pass IDs you read out of *your own* digest. That's internal plumbing, fine.

## 10. Addon ownership — what this skill does NOT cover

This skill is the manual for the **kernel-intrinsic `email` tool** only. Adjacent surfaces live elsewhere:

| Want to …                                          | Use                                          |
|----------------------------------------------------|----------------------------------------------|
| Send/receive real internet email (Gmail, etc.)     | `mcp-manual` → `lingtai-imap` MCP addon      |
| Send Telegram / Feishu / WeChat messages           | `mcp-manual` → respective MCP addon          |
| Send a notification-style ping to another agent    | This skill — it IS the notification channel  |
| Schedule a one-off wake-up of your own loop        | This skill, `delay` + self-send (§7)         |
| Schedule recurring agent-side work                 | This skill, `schedule={action: "create"}` (§8) |

The IMAP, Telegram, Feishu, and WeChat MCP addons each ship their own SKILL.md and `mcp-manual` entry. They are separate processes, separate auth surfaces, and separate failure modes. **Do not** try to use the `email` tool to reach an external address — you will write a file into a non-existent `.lingtai/foo@bar.com/` and the message will bounce silently.

## 11. Mode (output verbosity)

Every `email` action accepts an optional `mode` (set by `mode_field`) controlling how much detail is rendered in the tool result. Defaults are usually fine. Bump it when you are debugging delivery or read-state issues; lower it when you are iterating in a tight loop and only need confirmation.

## 12. Quick reference — common recipes

```python
# Triaging unread without reading bodies
email(action="check", filter={"unread_only": True}, n=20)

# Read one message in full
email(action="read", email_id=["<id-from-digest>"])

# Acknowledge a digest-preview-sufficient message
email(action="dismiss", email_id=["<id>"])

# Thread-preserving reply
email(action="reply", email_id=["<id>"], message="ack, looking now")

# Self-note that survives molt
email(action="send", address="<self>", subject="resume", message="next: ...")

# 5-minute timer
email(action="send", address="<self>", delay=300,
      subject="ding", message="check the deploy")

# Recurring nudge every 10 min, 6 times
email(schedule={"action": "create", "interval": 600, "count": 6},
      address="<self>", subject="stretch", message="stand up")

# Find related mail
email(action="search", query="helmholtz",
      filter={"after": "2026-05-01T00:00:00Z"})

# Address book
email(action="add_contact", address="mimo-1", name="MiMo (vision)",
      note="reachable for image-analysis requests")
```

---
> **Found a bug or issue?** If you encounter any problems with this skill, load the `lingtai-issue-report` skill and follow its instructions to report it.
