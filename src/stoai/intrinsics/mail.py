"""Mail intrinsic — disk-backed mailbox with 5 actions.

Actions:
    send   — fire-and-forget message to an address (self-send short-circuits TCP)
    check  — list inbox summaries (newest first, with unread flags)
    read   — load full message(s) by ID, mark as read
    search — regex search across from/subject/message fields
    delete — remove message(s) from disk
"""
from __future__ import annotations

import json
import re
import shutil
import threading
import uuid
from datetime import datetime, timedelta, timezone
from pathlib import Path

SCHEMA = {
    "type": "object",
    "properties": {
        "action": {
            "type": "string",
            "enum": ["send", "check", "read", "search", "delete"],
            "description": (
                "send: send a message (requires address, message; optional subject, attachments, type). "
                "check: list inbox summaries (optional n to limit count). "
                "read: load full message(s) by id (requires id array). "
                "search: regex search inbox (requires query). "
                "delete: remove message(s) from inbox (requires id array)."
            ),
        },
        "address": {
            "type": "string",
            "description": "Target address for send (e.g. 127.0.0.1:8301)",
        },
        "subject": {"type": "string", "description": "Message subject (for send)"},
        "message": {"type": "string", "description": "Message body (for send)"},
        "attachments": {
            "type": "array",
            "items": {"type": "string"},
            "description": "List of file paths to attach to the message (for send)",
        },
        "type": {
            "type": "string",
            "enum": ["normal", "silence", "kill"],
            "description": (
                "Mail type (for send). 'normal' (default) is regular mail. "
                "'silence' interrupts the target agent and puts it to idle "
                "(revives on next email; requires admin.silence privilege). "
                "'kill' hard-stops the target agent (requires admin.kill privilege). "
                "To revive: re-delegate with the SAME name."
            ),
        },
        "delay": {
            "type": "integer",
            "description": "Delay in seconds before delivery (default: 0)",
        },
        "id": {
            "type": "array",
            "items": {"type": "string"},
            "description": "Message ID(s) for read or delete actions",
        },
        "n": {
            "type": "integer",
            "description": "Max number of messages to show in check (default: all)",
        },
        "query": {
            "type": "string",
            "description": "Regex pattern for search action (searched in from, subject, message)",
        },
    },
    "required": ["action"],
}

DESCRIPTION = (
    "Disk-backed mailbox for inter-agent messaging. "
    "Messages persist to mailbox/inbox/ on disk. "
    "Use 'check' to see inbox summaries, 'read' to load full messages, "
    "'search' to find messages by regex, 'delete' to remove messages. "
    "Etiquette: a short acknowledgement is fine, but do not reply to "
    "an acknowledgement — that creates pointless ping-pong."
)


# ---------------------------------------------------------------------------
# Dispatcher
# ---------------------------------------------------------------------------

def handle(agent, args: dict) -> dict:
    """Handle mail tool — dispatch to action handler."""
    action = args.get("action", "send")
    handler = {
        "send": _send,
        "check": _check,
        "read": _read,
        "search": _search,
        "delete": _delete,
    }.get(action)
    if handler is None:
        return {"error": f"Unknown mail action: {action}"}
    return handler(agent, args)


# ---------------------------------------------------------------------------
# Mailbox helpers
# ---------------------------------------------------------------------------

def _mailbox_dir(agent) -> Path:
    """Return the mailbox root directory."""
    return agent._working_dir / "mailbox"


def _inbox_dir(agent) -> Path:
    """Return the inbox directory."""
    return _mailbox_dir(agent) / "inbox"


def _load_message(agent, msg_id: str) -> dict | None:
    """Load a single message by ID, or None if not found."""
    msg_file = _inbox_dir(agent) / msg_id / "message.json"
    if not msg_file.is_file():
        return None
    try:
        return json.loads(msg_file.read_text())
    except (json.JSONDecodeError, OSError):
        return None


def _list_inbox(agent) -> list[dict]:
    """List all inbox messages, sorted newest first (by received_at)."""
    inbox = _inbox_dir(agent)
    if not inbox.is_dir():
        return []
    messages = []
    for msg_dir in inbox.iterdir():
        if not msg_dir.is_dir():
            continue
        msg_file = msg_dir / "message.json"
        if not msg_file.is_file():
            continue
        try:
            msg = json.loads(msg_file.read_text())
            messages.append(msg)
        except (json.JSONDecodeError, OSError):
            continue
    # Sort newest first by received_at
    messages.sort(key=lambda m: m.get("received_at", ""), reverse=True)
    return messages


def _read_ids_path(agent) -> Path:
    """Path to the read.json tracking file."""
    return _mailbox_dir(agent) / "read.json"


def _read_ids(agent) -> set[str]:
    """Load set of read message IDs from read.json."""
    path = _read_ids_path(agent)
    if not path.is_file():
        return set()
    try:
        data = json.loads(path.read_text())
        return set(data) if isinstance(data, list) else set()
    except (json.JSONDecodeError, OSError):
        return set()


def _save_read_ids(agent, ids: set[str]) -> None:
    """Atomically write read IDs to read.json."""
    path = _read_ids_path(agent)
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_suffix(".tmp")
    tmp.write_text(json.dumps(sorted(ids)))
    import os
    os.replace(str(tmp), str(path))


def _mark_read(agent, msg_id: str) -> None:
    """Mark a message as read."""
    ids = _read_ids(agent)
    ids.add(msg_id)
    _save_read_ids(agent, ids)


def _message_summary(msg: dict, read_ids: set[str]) -> dict:
    """Build a summary dict for check output."""
    msg_id = msg.get("_mailbox_id", "")
    body = msg.get("message", "")
    preview = body[:120] + "..." if len(body) > 120 else body
    return {
        "id": msg_id,
        "from": msg.get("from", ""),
        "to": msg.get("to", ""),
        "subject": msg.get("subject", ""),
        "preview": preview,
        "time": msg.get("received_at", ""),
        "unread": msg_id not in read_ids,
    }


# ---------------------------------------------------------------------------
# Self-send helpers
# ---------------------------------------------------------------------------

def _is_self_send(agent, address: str) -> bool:
    """Check if the address matches this agent's own address or agent_id."""
    # Match by agent_id (works even without mail service)
    if address == agent.agent_id:
        return True
    # Match by mail service address
    if agent._mail_service is not None and agent._mail_service.address:
        if address == agent._mail_service.address:
            return True
    return False


def _persist_to_inbox(agent, payload: dict) -> str:
    """Persist a message directly to mailbox/inbox/{uuid}/message.json.

    Returns the message ID.
    """
    msg_id = str(uuid.uuid4())
    msg_dir = _inbox_dir(agent) / msg_id
    msg_dir.mkdir(parents=True, exist_ok=True)

    payload = dict(payload)  # shallow copy
    payload["_mailbox_id"] = msg_id
    payload["received_at"] = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")

    (msg_dir / "message.json").write_text(
        json.dumps(payload, indent=2, default=str)
    )
    return msg_id


def _outbox_dir(agent) -> Path:
    """Return the outbox directory."""
    return _mailbox_dir(agent) / "outbox"


def _sent_dir(agent) -> Path:
    """Return the sent directory."""
    return _mailbox_dir(agent) / "sent"


def _persist_to_outbox(agent, payload: dict, deliver_at: datetime) -> str:
    """Write a message to outbox/{uuid}/message.json. Returns the message ID."""
    msg_id = str(uuid.uuid4())
    msg_dir = _outbox_dir(agent) / msg_id
    msg_dir.mkdir(parents=True, exist_ok=True)

    payload = dict(payload)  # shallow copy
    payload["_mailbox_id"] = msg_id
    payload["deliver_at"] = deliver_at.strftime("%Y-%m-%dT%H:%M:%SZ")

    (msg_dir / "message.json").write_text(
        json.dumps(payload, indent=2, default=str)
    )
    return msg_id


def _move_to_sent(agent, msg_id: str, sent_at: str, status: str) -> None:
    """Move outbox/{uuid}/ → sent/{uuid}/, enriching with sent_at and status."""
    src = _outbox_dir(agent) / msg_id
    dst = _sent_dir(agent) / msg_id
    dst.parent.mkdir(parents=True, exist_ok=True)

    if not src.is_dir():
        return

    msg_file = src / "message.json"
    if msg_file.is_file():
        try:
            data = json.loads(msg_file.read_text())
        except (json.JSONDecodeError, OSError):
            data = {}
        data["sent_at"] = sent_at
        data["status"] = status
        msg_file.write_text(json.dumps(data, indent=2, default=str))

    shutil.move(str(src), str(dst))


def _mailman(agent, msg_id: str, payload: dict, deliver_at: datetime,
             *, skip_sent: bool = False) -> None:
    """Daemon thread — one per message. Waits, dispatches, archives to sent."""
    import time as _time

    wait = (deliver_at - datetime.now(timezone.utc)).total_seconds()
    if wait > 0:
        _time.sleep(wait)

    address = payload.get("_dispatch_to") or payload.get("to", "")
    if isinstance(address, list):
        address = address[0] if address else ""

    try:
        if _is_self_send(agent, address):
            _persist_to_inbox(agent, payload)
            agent._mail_arrived.set()
            status = "delivered"
        elif agent._mail_service is not None:
            err = agent._mail_service.send(address, payload)
            status = "delivered" if err is None else "refused"
        else:
            status = "refused"
    except Exception:
        status = "refused"

    sent_at = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")

    if not skip_sent:
        _move_to_sent(agent, msg_id, sent_at, status)
    else:
        outbox_entry = _outbox_dir(agent) / msg_id
        if outbox_entry.is_dir():
            shutil.rmtree(outbox_entry)

    agent._log("mail_sent", address=address, subject=payload.get("subject", ""),
               status=status, message=payload.get("message", ""))


# ---------------------------------------------------------------------------
# Action handlers
# ---------------------------------------------------------------------------

def _send(agent, args: dict) -> dict:
    """Send a message — validate, write to outbox, spawn mailman."""
    address = args.get("address", "")
    subject = args.get("subject", "")
    message_text = args.get("message", "")
    mail_type = args.get("type", "normal")
    delay = args.get("delay", 0)

    if mail_type != "normal" and not agent._admin.get(mail_type):
        return {"error": f"Not authorized to send type={mail_type!r} mail (requires admin.{mail_type}=True)"}

    if not address:
        return {"error": "address is required"}

    payload = {
        "from": (agent._mail_service.address if agent._mail_service is not None and agent._mail_service.address else agent.agent_id),
        "to": address,
        "subject": subject,
        "message": message_text,
        "type": mail_type,
    }

    attachments = args.get("attachments", [])
    if attachments:
        resolved = []
        for p in attachments:
            path = Path(p)
            if not path.is_absolute():
                path = agent._working_dir / path
            if not path.is_file():
                return {"error": f"Attachment not found: {path}"}
            resolved.append(str(path))
        payload["attachments"] = resolved

    deliver_at = datetime.now(timezone.utc) + timedelta(seconds=delay)
    msg_id = _persist_to_outbox(agent, payload, deliver_at)

    t = threading.Thread(
        target=_mailman,
        args=(agent, msg_id, payload, deliver_at),
        name=f"mailman-{msg_id[:8]}",
        daemon=True,
    )
    t.start()

    return {"status": "sent", "to": address, "delay": delay}


def _check(agent, args: dict) -> dict:
    """List inbox summaries with unread flags."""
    messages = _list_inbox(agent)
    read_set = _read_ids(agent)
    total = len(messages)

    n = args.get("n")
    shown = messages[:n] if n is not None else messages

    summaries = [_message_summary(m, read_set) for m in shown]
    unread_count = sum(1 for m in messages if m.get("_mailbox_id", "") not in read_set)

    return {
        "total": total,
        "unread": unread_count,
        "shown": len(summaries),
        "messages": summaries,
    }


def _read(agent, args: dict) -> dict:
    """Load full message(s) by ID, mark as read."""
    ids = args.get("id", [])
    if not ids:
        return {"error": "id is required (array of message IDs)"}

    results = []
    not_found = []
    for msg_id in ids:
        msg = _load_message(agent, msg_id)
        if msg is None:
            not_found.append(msg_id)
        else:
            _mark_read(agent, msg_id)
            results.append(msg)

    response: dict = {"messages": results}
    if not_found:
        response["not_found"] = not_found
    return response


def _search(agent, args: dict) -> dict:
    """Regex search across from/subject/message fields."""
    query = args.get("query", "")
    if not query:
        return {"error": "query is required"}

    try:
        pattern = re.compile(query, re.IGNORECASE)
    except re.error as e:
        return {"error": f"Invalid regex: {e}"}

    messages = _list_inbox(agent)
    read_set = _read_ids(agent)
    matches = []
    for msg in messages:
        fields = [
            msg.get("from", ""),
            msg.get("subject", ""),
            msg.get("message", ""),
        ]
        if any(pattern.search(f) for f in fields):
            matches.append(_message_summary(msg, read_set))

    return {"total": len(matches), "messages": matches}


def _delete(agent, args: dict) -> dict:
    """Remove message(s) from disk and clean read tracking."""
    ids = args.get("id", [])
    if not ids:
        return {"error": "id is required (array of message IDs)"}

    deleted = []
    not_found = []
    for msg_id in ids:
        msg_dir = _inbox_dir(agent) / msg_id
        if msg_dir.is_dir():
            shutil.rmtree(msg_dir)
            deleted.append(msg_id)
        else:
            not_found.append(msg_id)

    # Clean read tracking
    if deleted:
        read_set = _read_ids(agent)
        read_set -= set(deleted)
        _save_read_ids(agent, read_set)

    response: dict = {"deleted": deleted}
    if not_found:
        response["not_found"] = not_found
    return response
