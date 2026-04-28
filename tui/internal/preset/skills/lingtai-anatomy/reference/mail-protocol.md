# Mail Protocol — LingTai Anatomy Reference

> **Scope:** Complete technical reference for the LingTai mail/email subsystem,
> covering architecture, delivery lifecycle, scheduling, identity, and wake mechanics.

---

## v1 Corrections

| # | Issue | Correction |
|---|-------|------------|
| 1 | Original SKILL.md v1 refers to the tool as `mail`. | In the wrapper (capability) layer the tool is exposed as **`email`**. The kernel intrinsic name remains `mail`. |
| 2 | v1 anatomy only described basic point-to-point delivery. | The system also includes: scheduled sending, CC/BCC, delay queues, identity cards (身份牒), search/filter, contact management, and the self-send shortcut. |
| 3 | v1 implied a single implementation file. | The implementation spans **four layers** across four files totaling approximately ~2K lines. Exact counts drift with versions. |

---

## Three-Layer Architecture

```
┌──────────────────────────────────────────────────────────┐
│  services/mail.py  (facade, ~4 lines)                    │
│    Re-exports: MailService, FilesystemMailService        │
├──────────────────────────────────────────────────────────┤
│  core/email/__init__.py  (capability layer, ~1 300 lines)│
│    EmailManager — search, contacts, schedule, CC/BCC,    │
│    attachment handling, delay queue                       │
├──────────────────────────────────────────────────────────┤
│  intrinsics/mail.py  (intrinsic layer, ~593 lines)       │
│    Mail intrinsic — 5 basic actions:                     │
│      send / check / read / search / delete               │
├──────────────────────────────────────────────────────────┤
│  services/mail.py  (transport layer, ~375 lines)         │
│    FilesystemMailService — filesystem delivery,          │
│    polling listener, outbox→sent pipeline                │
└──────────────────────────────────────────────────────────┘
```

**Call path (normal send):**

```
Agent invokes tool "email"
  → EmailManager.send()
    → builds payload + identity card
    → spawns Mailman thread
      → FilesystemMailService.send()
        → atomic write to recipient inbox
```

---

## Mail Delivery Lifecycle

### Stage 1 — Send (Parameter Parsing)

1. **Parameter parsing** — the `send` action accepts `to`, `subject`, `message`,
   optional `cc`, `bcc`, `attachments`, `delay`, `schedule`.
2. **Address normalization** — bare names are resolved to full agent addresses
   via the network directory; fully-qualified addresses pass through unchanged.
3. **Duplicate-message defense** — a `_dup_free_passes` counter (default `2`)
   allows the same payload to be sent at most twice in rapid succession before
   the dedup gate rejects it.  This prevents runaway loops where two agents
   auto-reply to each other.
4. **Privacy mode** — when the sender's admin block is empty (e.g. an avatar),
   certain header fields are stripped to prevent credential leakage.
5. **Payload construction** — the message dict is assembled, then
   `_build_manifest()` injects the identity card (see below).
6. **Mailman thread** — a daemon thread is spawned to handle the transport
   asynchronously, returning control to the LLM immediately.

### Stage 2 — Transport (Mailman Daemon Thread)

```
Mailman thread
  │
  ├─ Sleep for delay seconds (0 = immediate)
  │
  ├─ Route selection:
  │    ├─ SSH route   → delegate to remote transport
  │    ├─ Self-send   → shortcut (see Stage 5)
  │    └─ Normal      → FilesystemMailService.send()
  │
  └─ Archive: move outbox/{uuid} → sent/{uuid}
```

The Mailman thread is the sole owner of the outbox→sent transition.  If the
process crashes mid-transfer the outbox entry remains and is **not** retried
automatically — this is intentional (at-most-once semantics).

### Stage 3 — Delivery (FilesystemMailService.send)

1. **Address resolution** — translate recipient name/address to a filesystem
   path under the network root.
2. **Handshake validation** — confirm the target directory contains
   `.agent.json` (`is_agent`) **and** the agent process is alive (`is_alive`,
   checked via PID in `.agent.json`).  If either check fails the message is
   written anyway but a warning is logged.
3. **Inbox entry creation** — a new UUID v4 directory is created under
   `recipient/mailbox/inbox/{uuid}/`.
4. **Attachment handling** — each attachment is validated (must exist, readable),
   then **copied** into `inbox/{uuid}/attachments/`.  Paths inside the message
   body are rewritten to point to the local copy.
5. **Atomic write** — the message is first written to `message.json.tmp`,
   then `os.replace()` atomically renames it to `message.json`.  This prevents
   the polling listener from reading a half-written file.

### Stage 4 — Receive (Polling Listener)

```
Polling thread (daemon, 0.5 s interval)
  │
  ├─ Phase 1: scan own inbox/
  │    for each {uuid}/message.json not in _seen:
  │      parse → fire on_message callback
  │      add uuid to _seen
  │
  └─ Phase 2: scan pseudo-agent outboxes
       (agents that lack their own polling thread)
```

- **`_seen` set** — an in-memory `set[str]` of UUIDs already processed.
  Survives across poll cycles but not across restarts.
- **Latency** — worst case 0.5 s from atomic write to callback invocation.

### Stage 5 — Self-Send Shortcut

When the Mailman thread detects `recipient == self`:

1. Skip network transport entirely.
2. Write directly to `self/mailbox/inbox/{uuid}/message.json`.
3. Call `_wake_nap("mail_arrived")` to break any active sleep.

This avoids the 0.5 s polling latency for self-directed messages.

---

## Mailbox Directory Structure

```
mailbox/
├── inbox/
│   └── {uuid}/
│       ├── message.json        ← received message
│       └── attachments/        ← local copies of attached files
├── sent/
│   └── {uuid}/
│       └── message.json        ← sent message archive
├── archive/
│   └── {uuid}/
│       └── message.json        ← manually archived messages
├── outbox/
│   └── {uuid}/
│       └── message.json        ← pending (Mailman processing)
├── schedules/
│   └── {schedule_id}/
│       └── schedule.json       ← recurring schedule config
├── read.json                   ← JSON array of read message UUIDs
└── contacts.json               ← JSON array of contact objects
```

### message.json Schema (Received)

```jsonc
{
  "_mailbox_id": "uuid-v4",
  "from": "sender-address",
  "to": ["recipient-address"],
  "cc": [],
  "bcc": [],                             // stripped before delivery by transport
  "subject": "string",
  "message": "string",
  "type": "normal",
  "received_at": "2026-01-15T10:30:00Z",
  "attachments": ["/path/to/local/copy"],
  // Identity card (身份牒) — embedded manifest snapshot from sender
  "identity": {
    "agent_id": "20260423-221801-1710",
    "agent_name": "orchestrator",
    "nickname": null,
    "address": "orchestrator",
    "admin": {"karma": true},
    "language": "wen",
    "state": "active",
    "soul_delay": 120,
    "molt_count": 2
  }
}
```

When the `check` or `read` action returns results to the LLM, `_inject_identity()`
flattens the `identity` dict into top-level convenience fields: `sender_name`,
`sender_nickname`, `sender_agent_id`, `sender_language`, `is_human` (true when
admin is null), and `sender_location` (city/region/timezone dict, human-only).
The raw `identity` dict remains in the stored file; the flat fields are injected
only at presentation time.
```

### message.json Schema (Sent Archive)

```jsonc
{
  "_mailbox_id": "uuid-v4",
  "from": "self-address",
  "to": ["recipient-address"],
  "cc": [],
  "bcc": [],
  "subject": "string",
  "message": "string",
  "type": "normal",
  "sent_at": "2026-01-15T10:30:00Z",
  "delay": 0,
  "attachments": ["/abs/path/to/file"],
  "identity": { /* sender manifest snapshot */ }
}
```

---

## Identity Card (身份牒)

Every outgoing message carries an identity snapshot generated by
`agent._build_manifest()` and injected via `_inject_identity()`.

### Manifest Fields (from sender)

| Field | Type | Source |
|-------|------|--------|
| `agent_id` | str | Agent instance UUID |
| `agent_name` | str | Directory name / configured name |
| `nickname` | str | Human-friendly alias |
| `address` | str | Full network address |
| `admin` | dict | Administrator configuration block |
| `language` | str | Configured language code |
| `state` | str | Current agent state string |
| `soul_delay` | float | Current nap/soul-cycle delay |
| `molt_count` | int | Number of completed molts |
| `location` | str | Absolute path to agent working dir |

### Injected Fields (on check/read results)

`_inject_identity()` parses the manifest and extracts a flat subset into each
message dict visible to the LLM:

| Injected Field | Mapped From |
|----------------|-------------|
| `sender_name` | `manifest.agent_name` |
| `sender_nickname` | `manifest.nickname` |
| `sender_agent_id` | `manifest.agent_id` |
| `sender_language` | `manifest.language` |
| `is_human` | heuristic check on admin block |
| `sender_location` | `manifest.location` |

This allows recipients to reason about who sent a message without needing a
separate directory lookup.

---

## Scheduled Sending

### Schedule Configuration

```jsonc
// mailbox/schedules/{schedule_id}/schedule.json
{
  "schedule_id": "a1b2c3d4e5f6",       // 12-char hex
  "send_payload": {                      // full mail payload
    "to": ["..."],
    "subject": "...",
    "message": "..."
  },
  "interval": 3600,                      // seconds between sends
  "count": 10,                           // total intended sends
  "sent": 3,                             // already sent (incremented first)
  "status": "active"                     // active | inactive | completed
}
```

### Scheduler Loop

```
while True:
    sleep(1)  // 1-second tick
    for each active schedule:
        if now >= last_sent + interval:
            schedule.sent += 1           // increment BEFORE send (at-most-once)
            if schedule.sent >= schedule.count:
                schedule.status = "completed"
            mailman_deliver(schedule.send_payload)
```

**At-most-once guarantee:** `sent` is incremented *before* the delivery attempt.
If the process crashes after increment but before delivery, that message is
lost but no duplicate will be sent.

### Startup Reconciliation

On agent restart, all schedules with status not `completed` are forcibly set to
`inactive`.  This is a safety mechanism — the agent must explicitly reactivate
schedules after a crash to prevent runaway sending.

### Cancel and Reactivate

- **Cancel** — sets `status = "inactive"`.  Accepts a single `schedule_id` or
  `"all"` for batch cancellation.
- **Reactivate** — sets `status = "active"`.  If `sent >= count`, self-heals by
  setting `status = "completed"` instead (prevents reactivating a finished
  schedule).

---

## Advanced Features

### Delay Sending

The `delay` parameter (seconds) on a send action causes the Mailman thread to
`time.sleep(delay)` before initiating delivery.  The message sits in `outbox/`
during the wait.

### CC / BCC

- **CC** — recipients listed in `cc` receive the message and are visible to all
  recipients.
- **BCC** — recipients listed in `bcc` receive the message but the `bcc` field
  is stripped from the delivered copy.  Primary/CC recipients never see BCC
  addresses.

### Attachments

1. **Validation** — each path must exist and be readable by the sender.
2. **Local copy** — the file is copied into `inbox/{uuid}/attachments/` on the
   recipient's filesystem.
3. **Path substitution** — references to the original path inside the message
   body are rewritten to the new local path so the recipient can open them.

### Search and Filtering

The `search` action supports:

| Filter | Type | Description |
|--------|------|-------------|
| `from` | regex | Match sender name/address |
| `subject` | regex | Match subject line |
| `contains` | regex | Match message body |
| `after` | ISO-8601 | Only messages after this timestamp |
| `before` | ISO-8601 | Only messages before this timestamp |
| `unread_only` | bool | Exclude IDs present in `read.json` |
| `has_attachments` | bool | Only messages with attachment entries |
| `sort` | enum | `newest` (default) or `oldest` |

All regex filters are applied via `re.search()` (substring match, case-sensitive).

### Mail as Time Machine

| Pattern | Mechanism | Use Case |
|---------|-----------|----------|
| Memory anchor | Self-send, no delay | Record a thought for immediate retrieval |
| Time capsule | Self-send with `delay` | Deliver a reminder to future self |
| Scheduled recurring | Schedule with interval + count | Periodic check-ins, heartbeat confirmation |

---

## Wake Mechanism

Mail is the primary mechanism for waking a sleeping (napping) agent.

```
                    ┌─────────────────────────────────┐
                    │         Agent is napping         │
                    │   (threading.Event.wait(timeout))│
                    └──────────────┬──────────────────┘
                                   │
         ┌─────────────────────────┼──────────────────────────┐
         │                         │                          │
    Self-send                 External delivery          Any path
         │                         │                          │
  Direct call              Polling listener             _wake_nap(reason)
  _wake_nap()              detects new inbox            sets Event
         │                  within 0.5 s                     │
         │                         │                          │
         └─────────────────────────┴──────────────────────────┘
                                   │
                          Event.set() → nap breaks
                          Agent enters next soul cycle
```

1. **Self-send** — `_wake_nap("mail_arrived")` is called directly in the
   Mailman thread.  Zero latency.
2. **External delivery** — the polling listener's 0.5 s scan detects the new
   `message.json`, fires `on_message` → `_wake_nap("mail_arrived")`.
3. **`_wake_nap`** implementation — calls `threading.Event.set()` on the nap
   event, which causes `Event.wait(timeout)` to return immediately.  The reason
   string is stored for the agent's next soul-cycle reasoning.

---

## Kernel vs Wrapper Terminology

| Layer | Name | File | Notes |
|-------|------|------|-------|
| Kernel intrinsic | `mail` | `intrinsics/mail.py` | 5 basic actions. Invoked by the agent runtime. |
| Wrapper tool | `email` | `core/email/__init__.py` | Exposed to the LLM as a callable tool. |
| Transport | — | `services/mail.py` | `FilesystemMailService` shared by both layers. |

**Key point:** When the LLM decides to send an email, it invokes the tool named
`email`.  Under the hood this flows through `EmailManager` → `Mailman` →
`FilesystemMailService`.  The kernel intrinsic `mail` is a lower-level interface
used by the agent runtime itself (e.g., for system notifications).  Both paths
converge on the same transport and mailbox structure.

---

*End of mail-protocol reference.*
