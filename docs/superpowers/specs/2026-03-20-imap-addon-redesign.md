# IMAP Addon Redesign — Faithful IMAP/SMTP Implementation

**Date:** 2026-03-20
**Status:** Approved

## Goal

Rewrite the `imap` addon to faithfully expose IMAP/SMTP protocol capabilities. Multi-account support, server-side flags and search, IDLE push, real folder management. No local workarounds for things the protocol already provides.

## Architecture

### Three Classes

```
IMAPAccount          — one IMAP connection + SMTP credentials + IDLE/poll thread
                       connect, fetch, search, store flags, send, folder ops
                       One instance per email address

IMAPMailService      — manages N IMAPAccount instances
                       Implements MailService interface (for TCP bridge compat)
                       Delegates to account by address

IMAPMailManager      — tool handler + filesystem + notifications
                       Single "imap" tool registered with agent
                       Routes actions to correct account
                       Persists emails to disk under account subdirectory
                       Contact management per account
```

### File Layout

```
addons/imap/
├── __init__.py      — setup(), config parsing (single + multi-account)
├── account.py       — IMAPAccount (connection, IDLE, SMTP, flags, folders, email parsing)
├── service.py       — IMAPMailService (multi-account coordinator)
└── manager.py       — IMAPMailManager (tool handler, filesystem, contacts)
```

### On-Disk Layout

```
imap/
├── alice@gmail.com/
│   ├── INBOX/
│   │   └── {uid}/message.json
│   │   └── {uid}/attachments/photo.png
│   ├── [Gmail]/Sent Mail/
│   │   └── {uid}/message.json
│   ├── [Gmail]/Trash/
│   │   └── ...
│   ├── Archive/
│   │   └── ...
│   ├── contacts.json
│   └── state.json
├── bob@outlook.com/
│   ├── INBOX/
│   │   └── ...
│   ├── Sent Items/
│   │   └── ...
│   └── ...
└── (no config.json — config is injected via constructor)
```

Folder names on disk mirror server folder names exactly. Emails the agent has interacted with (read, sent) are persisted locally as an audit trail — the local disk is not a full mirror.

### `state.json` Schema

Per-account state file. Contains:

```json
{
  "processed_uids": {"INBOX": [1001, 1002, 1003]},
  "folders": {
    "INBOX": {"role": null},
    "[Gmail]/Sent Mail": {"role": "sent"},
    "[Gmail]/Trash": {"role": "trash"},
    "[Gmail]/All Mail": {"role": "archive"},
    "[Gmail]/Drafts": {"role": "drafts"},
    "[Gmail]/Spam": {"role": "junk"}
  },
  "capabilities": {
    "idle": true,
    "move": true,
    "uidplus": true
  }
}
```

- `processed_uids`: per-folder UID sets for incoming dedup.
- `folders`: server folder names → role mapping (discovered on connect).
- `capabilities`: cached server capability flags relevant to our operations.

## Email ID Format

All tool actions that reference emails use `email_id` as an opaque string with the format:

```
{account}:{folder}:{uid}
```

Example: `alice@gmail.com:INBOX:1042`

This compound key is necessary because IMAP UIDs are only unique within a folder on a given server. The manager parses this to route to the correct account and folder. The agent treats it as an opaque ID — it comes from `check`/`search` results and is passed back to `read`/`reply`/`delete`/`move`/`flag`.

## Tool Schema

Single `imap` tool. The `account` param is optional on all actions — defaults to first configured account.

### Actions

| Action | Params | Description |
|--------|--------|-------------|
| `send` | `address`, `subject`, `message`, `cc`, `bcc`, `attachments` | Send via SMTP. Saves to server's Sent folder. |
| `check` | `folder`, `n` | Live IMAP query — fetches headers via ENVELOPE. Not cached to disk. Default: INBOX. |
| `read` | `email_id` | Fetch full body + attachments. Sets `\Seen` on server. Persists to disk. |
| `reply` | `email_id`, `message`, `cc` | Reply with proper threading headers. Sets `\Answered` on server. |
| `search` | `query`, `folder` | Server-side IMAP SEARCH. See syntax below. |
| `delete` | `email_id` | Move to server's Trash folder. |
| `move` | `email_id`, `folder` | Move to any server folder. |
| `flag` | `email_id`, `flags` | Set/clear flags. See flag format below. |
| `folders` | — | List all server folders. |
| `contacts` | — | List contacts for this account. |
| `add_contact` | `address`, `name`, `note` | Add/update contact. |
| `remove_contact` | `address` | Remove contact. |
| `edit_contact` | `address`, `name`, `note` | Update contact fields. |
| `accounts` | — | List configured accounts with connection status. |

### `check` Behavior

`check` is a live IMAP query — it sends `FETCH (ENVELOPE FLAGS)` to the server and returns results directly to the agent. No body is downloaded, no data is written to disk. This is fast even on large mailboxes (only headers + flags).

Results are sorted by UID descending (newest first). Each result includes the compound `email_id` for use in subsequent actions.

### `reply` Threading

A proper email reply must thread correctly in all email clients. When replying:

1. Fetch the original email's `Message-ID` header.
2. Set `In-Reply-To: <original Message-ID>` on the outgoing message.
3. Set `References:` to the original's `References` header (if any) plus the original's `Message-ID`.
4. Prepend `Re:` to subject (if not already present).
5. Set `\Answered` flag on the original email on the server.

### `flag` Format

The `flags` param is a dict of flag names to booleans:

```json
{"flagged": true, "answered": false, "seen": false, "draft": true}
```

- `flagged` → `\Flagged` (star/important)
- `answered` → `\Answered`
- `seen` → `\Seen` (read/unread — allows marking as unread)
- `draft` → `\Draft`

`true` sets the flag, `false` clears it. Only specified flags are changed — omitted flags are untouched.

### IMAP SEARCH Syntax

The `query` param for `search` uses a simple syntax that maps to IMAP SEARCH commands:

| User writes | IMAP command |
|-------------|-------------|
| `from:alice@example.com` | `FROM "alice@example.com"` |
| `to:bob@example.com` | `TO "bob@example.com"` |
| `subject:meeting` | `SUBJECT "meeting"` |
| `since:2026-03-01` | `SINCE 01-Mar-2026` |
| `before:2026-03-20` | `BEFORE 20-Mar-2026` |
| `flagged` | `FLAGGED` |
| `unseen` | `UNSEEN` |
| `seen` | `SEEN` |
| `answered` | `ANSWERED` |
| `"exact phrase"` | `TEXT "exact phrase"` |

Terms are combinable (AND by default): `from:alice since:2026-03-01 unseen`

Fallback: if query doesn't match any structured prefix, pass as `TEXT "..."` for full-text server-side search.

## Configuration

### Single Account (shorthand)

```python
addons={"imap": {
    "email_address": "alice@gmail.com",
    "email_password": "xxxx",
    "imap_host": "imap.gmail.com",
    "smtp_host": "smtp.gmail.com",
}}
```

### Multi-Account

```python
addons={"imap": {
    "accounts": [
        {
            "email_address": "alice@gmail.com",
            "email_password": "xxxx",
            "imap_host": "imap.gmail.com",
            "smtp_host": "smtp.gmail.com",
        },
        {
            "email_address": "bob@outlook.com",
            "email_password": "xxxx",
            "imap_host": "outlook.office365.com",
            "smtp_host": "smtp.office365.com",
            "smtp_port": 587,
        },
    ],
    "bridge_port": 8399,
}}
```

Single-account shorthand: if `email_address` is present at top level (no `accounts` list), wrap into `accounts: [...]` internally.

Default account: first in the list. Agent overrides with `account` param on any action.

### `setup()` Signature

```python
def setup(
    agent: "BaseAgent",
    *,
    # Single-account shorthand (ignored if accounts is provided)
    email_address: str | None = None,
    email_password: str | None = None,
    imap_host: str = "imap.gmail.com",
    imap_port: int = 993,
    smtp_host: str = "smtp.gmail.com",
    smtp_port: int = 587,
    allowed_senders: list[str] | None = None,
    poll_interval: int = 30,
    # Multi-account
    accounts: list[dict] | None = None,
    # Addon-level
    bridge_port: int = 8399,
) -> IMAPMailManager:
```

If `accounts` is provided, use it. Otherwise, build a single-account list from the flat fields. Raises `ValueError` if neither `accounts` nor `email_address` is provided.

### Per-Account Config Fields

| Field | Default | Description |
|-------|---------|-------------|
| `email_address` | required | IMAP/SMTP login address |
| `email_password` | required | App password or credentials |
| `imap_host` | `imap.gmail.com` | IMAP server hostname |
| `imap_port` | `993` | IMAP SSL port |
| `smtp_host` | `smtp.gmail.com` | SMTP server hostname |
| `smtp_port` | `587` | SMTP TLS port |
| `allowed_senders` | `None` (accept all) | Whitelist for incoming |
| `poll_interval` | `30` | Fallback poll interval (seconds), used only if IDLE unsupported |

### Addon-Level Config Fields

| Field | Default | Description |
|-------|---------|-------------|
| `bridge_port` | `8399` | TCP bridge for inter-agent relay |

## Connection & Receiving

### IMAP IDLE with Poll Fallback

1. On connect, check server `CAPABILITY` for `IDLE`.
2. If supported: enter IDLE mode, wake on new mail notification, fetch new messages, re-enter IDLE.
3. If not supported: fall back to polling at `poll_interval`.
4. Re-issue IDLE every 25 minutes proactively (servers kill IDLE after ~29 min per RFC).
5. On connection drop: reconnect with backoff (1s, 2s, 5s, 10s, cap at 60s).
6. Each account manages its own connection lifecycle independently — one account failing doesn't affect others.

**IDLE and command interleaving:** IMAP does not accept commands while in IDLE state. When the agent issues an action (check, read, flag, move, etc.), the background IDLE thread must:

1. Send `DONE` to exit IDLE.
2. Acquire a mutex on the IMAP connection.
3. Execute the requested command(s).
4. Release the mutex.
5. Re-enter IDLE.

The `IMAPAccount` class must use a threading lock to coordinate between the IDLE background thread and action requests from the tool handler.

### Fetch Strategy

- **`check` action:** `FETCH (ENVELOPE FLAGS)` — sender, subject, date, flags. No body download. Results returned directly, not persisted to disk. Fast on large mailboxes.
- **`read` action:** `FETCH (BODY[])` — full body + attachments. Only when agent explicitly reads. Persisted to disk as audit trail.
- `\Seen` is set by the server on body fetch (standard IMAP behavior). We keep this.

### Flag Sync

- Our addon writes flags to the server via `STORE`.
- On each check/poll, we read current flags from the server.
- If an email was read externally (e.g. Gmail web), the server's `\Seen` flag is reflected.
- **No local `read.json`** — flags live on the server where they belong.

## SMTP Send Details

### Basic Send

Build MIME message (MIMEText or MIMEMultipart with attachments), set `From`, `To`, `Subject` headers, send via SMTP with STARTTLS + login.

### CC/BCC Handling

- **CC:** Added as a `Cc:` header on the MIME message. CC addresses are included in SMTP `RCPT TO` envelope.
- **BCC:** NOT added as a header (must not appear in delivered message). BCC addresses are included in SMTP `RCPT TO` envelope only. Python's `smtplib.send_message()` handles this correctly when BCC is omitted from headers but passed in the envelope — we use `smtplib.SMTP.sendmail()` with explicit recipient list instead of `send_message()` to control this.

All recipients (`to` + `cc` + `bcc`) are combined into the SMTP envelope recipient list.

## Folder Management

### Discovery

On connect, query `LIST "" "*"` to discover server folders. Map common roles using RFC 6154 special-use attributes:

| Attribute | Role |
|-----------|------|
| `\Trash` | Trash/deleted items |
| `\Sent` | Sent mail |
| `\Archive` | Archive |
| `\Drafts` | Drafts |
| `\Junk` | Junk/spam |

If no special-use attributes, fall back to name heuristics (e.g. "Sent Items", "Deleted Items", "[Gmail]/Trash").

Store the folder → role mapping in `state.json` per account.

### Operations

- **`delete`:** move to Trash role folder. If no Trash folder exists, set `\Deleted` flag. Use `UID EXPUNGE <uid>` (RFC 4315 UIDPLUS extension) when available to avoid purging other pending deletions. Fall back to `EXPUNGE` only if UIDPLUS is not supported.
- **`move`:** use IMAP `MOVE` extension if supported (check CAPABILITY). Otherwise `COPY` to destination + `\Deleted` on source + `UID EXPUNGE`.
- **`folders`:** return list of all server folders with their roles (if mapped).

## Error Handling

- Each `IMAPAccount` is independent — connection failure on one doesn't affect others.
- IDLE timeout: proactive re-issue at 25 minutes.
- IDLE interrupt: mutex-protected, sends `DONE` before any command.
- Reconnect backoff: 1s → 2s → 5s → 10s → cap at 60s.
- SMTP send failures: return error string (same as current).
- Duplicate send protection: block 3rd+ identical message to same recipient (same as current).
- UID-based dedup for incoming, per account per folder.
- Unknown folder in `move`/`check`: return error with available folder list.
- Stale `email_id`: if UID no longer exists on server (deleted externally), return clear error.

## What We're NOT Building

- SORT/THREAD extensions (not universally supported, sort locally)
- NAMESPACE (multi-account shared mailboxes — overkill)
- QUOTA (storage management — not agent's job)
- APPEND (upload messages to folders — niche)
- DSN (delivery status notifications — overkill)
- Auto-forwarding for `+` aliases (future — infra supports it)

## Testing Strategy

- Unit tests with mocked IMAP/SMTP (same pattern as current).
- `IMAPAccount` tests: connection, CAPABILITY parsing, IDLE detection, ENVELOPE fetch, flag STORE, folder LIST with role mapping, SEARCH command building, IDLE interrupt-and-resume, CC/BCC SMTP handling, reply threading headers.
- `IMAPMailService` tests: multi-account routing, default account selection, single-account shorthand.
- `IMAPMailManager` tests: action dispatch, email_id parsing (account:folder:uid), filesystem persistence, contact management, account param routing.
- Integration: manual test with real Gmail account (same as current `app/email` launcher).
