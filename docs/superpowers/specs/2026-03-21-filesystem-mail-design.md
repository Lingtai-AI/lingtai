# Filesystem-Based Mail: Path as Address

**Date**: 2026-03-21
**Status**: Draft
**Scope**: lingtai-kernel + lingtai + Go daemon

## Problem

Mail addresses are currently TCP ports (`127.0.0.1:8501`). Ports are ephemeral — they change across restarts, can conflict, and make contacts stale immediately. The entire mail system depends on active TCP connections, meaning offline agents can't receive mail.

## Core Design Principle

**The filesystem path IS the address.** Delivering mail = writing a file to a directory. TCP is removed entirely for local communication. Remote agents are a future concern (IMAP), not part of this design.

## Address Format

The address is the agent's **working directory** — the identity root:

```
/Users/huangzesen/agents/a1b2c3d4e5f6
```

Each mail service defines its own relative mailbox path within the working directory:

| Service | Relative path | Inbox path |
|---------|--------------|------------|
| Mail intrinsic (kernel) | `mailbox/` | `{address}/mailbox/inbox/{uuid}/` |
| Email capability (lingtai) | `email/` | `{address}/email/inbox/{uuid}/` |

Mail-to-mail, email-to-email. Bridging between services is not lingtai's concern.

## Handshake on Send

Before writing to a recipient's inbox, the sender verifies the destination:

1. Read `{address}/.agent.json`
2. Verify it exists and is valid JSON
3. Verify `agent_id` matches what the sender expects (from contacts)
4. If valid → write to `{address}/{mailbox_rel}/inbox/{uuid}/`

Failure cases:
- `.agent.json` missing → error: "no agent at this address"
- `agent_id` mismatch → error: "agent at this address has changed"

## Message Delivery

Sending mail = writing files to the recipient's inbox directory:

```
{recipient_address}/{mailbox_rel}/inbox/{uuid}/
├── message.json      ← message payload
└── attachments/      ← actual files (no base64)
    ├── report.pdf
    └── image.png
```

**message.json payload** (same fields as today, addresses become paths):

```json
{
  "from": "/Users/huangzesen/agents/b2c3d4e5f6a1",
  "to": "/Users/huangzesen/agents/a1b2c3d4e5f6",
  "subject": "Hello",
  "message": "...",
  "type": "normal",
  "_mailbox_id": "uuid",
  "received_at": "2026-03-21T10:00:00Z"
}
```

**Attachments** are real files — copied or moved into `attachments/`. No more base64 encoding over TCP. This makes large files, binary data, images trivially simple.

**Self-send** (notes to self): write to own inbox path. No special case.

## FilesystemMailService (Kernel)

Replaces `TCPMailService` entirely. Lives in `lingtai_kernel.services.mail`.

### Constructor

```python
FilesystemMailService(working_dir: str | Path, mailbox_rel: str = "mailbox")
```

- `working_dir`: the agent's working directory (the address)
- `mailbox_rel`: relative path for this service's mailbox within the working dir

### ABC

```python
class MailService(ABC):
    @property
    @abstractmethod
    def address(self) -> str: ...          # own working dir path

    @abstractmethod
    def send(self, address: str, payload: dict,
             attachments: list[Path] | None = None) -> str | None: ...
             # returns None on success, error string on failure

    @abstractmethod
    def listen(self, on_message: Callable[[dict], None]) -> None: ...

    @abstractmethod
    def stop(self) -> None: ...
```

### Implementation

- **`send(address, payload, attachments)`**: reads `{address}/.agent.json` for handshake verification, creates `{address}/{mailbox_rel}/inbox/{uuid}/`, writes `message.json`, copies attachment files into `attachments/` subfolder. Returns `None` on success, error string on failure.
- **`listen(on_message)`**: starts a daemon thread that polls own `{mailbox_rel}/inbox/` for new message directories. Tracks seen UUIDs in a `set`. On new message: reads `message.json`, calls `on_message(payload)`. Loads existing UUIDs on startup to avoid re-notifying old messages.
- **`stop()`**: signals the polling thread to exit.
- **`address`** property: returns `str(working_dir)`.

### Poll interval

~0.5 seconds (configurable). The polling thread is lightweight — just listing a directory for new entries.

## Notification Mechanism

Same as today. When the polling thread detects a new message, it calls `on_message(payload)` which triggers `BaseAgent._on_mail_received()`. This injects a `[system]` notification into the agent's LLM conversation, exactly as the TCP listener callback does now.

No change to `BaseAgent._on_mail_received()`, `_on_normal_mail()`, or the mail type routing (`normal`/`silence`/`kill`).

## Human as First-Class Participant

Humans get a working directory with the same structure as agents. The only distinguishing marker: `admin: null` in `.agent.json`.

**Human's `.agent.json`**:
```json
{
  "agent_id": "human_zesen",
  "agent_name": "Zesen",
  "started_at": "...",
  "working_dir": "/Users/huangzesen/agents/human_zesen",
  "admin": null,
  "language": "zh",
  "address": "/Users/huangzesen/agents/human_zesen"
}
```

**Human's directory structure** (same as agent):
```
{base_dir}/human_zesen/
├── .agent.json
├── mailbox/
│   ├── inbox/
│   ├── sent/
│   ├── contacts.json
│   └── read.json
└── ...
```

The TUI reads/writes the human's mailbox directory. When the user types a message, the TUI writes to the agent's inbox. When the agent replies, it writes to the human's inbox. The TUI polls the human's inbox for new messages.

## Contact Structure

```json
{
  "address": "/Users/huangzesen/agents/a1b2c3d4e5f6",
  "name": "Alice",
  "agent_id": "a1b2c3d4e5f6",
  "note": ""
}
```

- `address`: the agent's working directory (full path)
- `agent_id`: used for handshake verification on send
- Agents learn addresses through **introduction only** — no auto-discovery, no scanning

## Agent Discovery

Explicit introduction only. Agents know who they know.

Introduction happens when:
- The daemon starts an agent — it introduces the human and agent to each other by writing into both `contacts.json` files
- An agent sends mail — the `from` field contains the sender's address
- The host app configures contacts at construction

No scanning of `base_dir`, no registry, no discovery protocol.

## Daemon/TUI Changes

### Startup flow (revised)

1. Read config → get `base_dir` and agent settings
2. Create human working directory at `{base_dir}/human_{id}/` if it doesn't exist
3. Write human's `.agent.json`
4. Start Python agent process
5. Wait for agent's `.agent.json` to appear (instead of `WaitForPort`)
6. Exchange introductions — write human's address into agent's `contacts.json` and agent's address into human's `contacts.json`

### TUI communication

- **Sending**: write `message.json` to `{agent_address}/mailbox/inbox/{uuid}/`, record in human's `mailbox/sent/`
- **Receiving**: poll `{human_mailbox}/inbox/` for new message directories, display in chat view

### Deleted from Go daemon

- `MailClient`, `MailListener` (TCP classes in `mail.go`)
- `WaitForPort` logic
- Port-related config for mail (`agent_port` may still exist for process management)

### Replaced with

- Go filesystem operations: `os.MkdirAll`, `os.WriteFile`, directory polling
- A small Go package for reading/writing the lingtai mailbox format (JSON message files)

## Impact on Existing Code

### Kernel (`lingtai-kernel`)

| Component | Change |
|-----------|--------|
| `TCPMailService` | **Deleted** — replaced by `FilesystemMailService` |
| `MailService` ABC | Simplified — no port, no banner, path-based |
| Mail intrinsic | Minimal — `_mailman` calls `mail_service.send(path, ...)` instead of TCP send |
| `BaseAgent` | No port params in constructor. `_on_mail_received` unchanged |
| `_build_manifest()` | `address` field now holds working dir path (already dynamic) |

### Lingtai

| Component | Change |
|-----------|--------|
| Email capability | Minimal — address format changes, attachment handling simplified. `mailbox_rel="email"` |
| All other capabilities | No changes |

### Go Daemon

| Component | Change |
|-----------|--------|
| `mail.go` | **Rewritten** — filesystem read/write instead of TCP |
| `config/loader.go` | Remove port-based mail config |
| `tui/app.go` | Filesystem-based send/receive |
| Agent startup | Wait for `.agent.json` instead of `WaitForPort` |

## Migration

Clean break. No backward compatibility layer.

- Old contacts with `host:port` addresses: invalid, agents re-introduced on next startup
- Old inbox messages with TCP addresses: stale history, left as-is
- Existing agent sessions: nuked (user confirmed)

## What This Enables

- **Offline delivery**: mail queues up in inbox even when recipient isn't running
- **Trivial attachments**: real files, no base64, no size limits
- **Debuggable**: `ls` an agent's inbox, `cat` a message
- **Stable addresses**: paths don't change across restarts
- **Human-agent parity**: same mailbox structure, same protocol, same tools
- **No port conflicts**: filesystem paths don't collide
