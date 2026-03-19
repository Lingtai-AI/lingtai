from __future__ import annotations
import json
from pathlib import Path
from unittest.mock import MagicMock
from stoai.addons.gmail.manager import GmailManager


def test_check_returns_tcp_alias(tmp_path):
    agent = MagicMock()
    agent._working_dir = tmp_path
    svc = MagicMock()
    svc.address = "agent@gmail.com"
    mgr = GmailManager(agent, gmail_service=svc, tcp_alias="127.0.0.1:8399")

    result = mgr.handle({"action": "check"})
    assert result["tcp_alias"] == "127.0.0.1:8399"
    assert result["account"] == "agent@gmail.com"


def test_check_lists_gmail_inbox(tmp_path):
    agent = MagicMock()
    agent._working_dir = tmp_path
    svc = MagicMock()
    svc.address = "agent@gmail.com"
    mgr = GmailManager(agent, gmail_service=svc, tcp_alias="127.0.0.1:8399")

    eid = "test-email-1"
    msg_dir = tmp_path / "gmail" / "inbox" / eid
    msg_dir.mkdir(parents=True)
    (msg_dir / "message.json").write_text(json.dumps({
        "from": "user@gmail.com", "to": ["agent@gmail.com"],
        "subject": "hello", "message": "hi there",
        "_mailbox_id": eid, "received_at": "2026-03-18T12:00:00Z",
    }))

    result = mgr.handle({"action": "check"})
    assert len(result["emails"]) == 1
    assert result["emails"][0]["from"] == "user@gmail.com"


def test_send_uses_gmail_service(tmp_path):
    agent = MagicMock()
    agent._working_dir = tmp_path
    svc = MagicMock()
    svc.address = "agent@gmail.com"
    svc.send.return_value = None
    mgr = GmailManager(agent, gmail_service=svc, tcp_alias="127.0.0.1:8399")

    result = mgr.handle({
        "action": "send", "address": "user@gmail.com",
        "subject": "test", "message": "hello",
    })
    assert result["status"] == "delivered"
    svc.send.assert_called_once()


def test_every_response_has_meta(tmp_path):
    agent = MagicMock()
    agent._working_dir = tmp_path
    svc = MagicMock()
    svc.address = "agent@gmail.com"
    mgr = GmailManager(agent, gmail_service=svc, tcp_alias="127.0.0.1:8399")

    for action in ["check", "contacts"]:
        result = mgr.handle({"action": action})
        assert "tcp_alias" in result
        assert "account" in result

    result = mgr.handle({"action": "read", "email_id": "nope"})
    assert "tcp_alias" in result


def test_on_gmail_received_notifies_agent(tmp_path):
    """on_gmail_received should enqueue a notification to the agent inbox."""
    agent = MagicMock()
    agent._working_dir = tmp_path
    svc = MagicMock()
    svc.address = "agent@gmail.com"
    mgr = GmailManager(agent, gmail_service=svc, tcp_alias="127.0.0.1:8399")

    payload = {
        "_mailbox_id": "test-123",
        "from": "user@gmail.com",
        "subject": "hello",
        "message": "hi there",
    }
    mgr.on_gmail_received(payload)

    # Should have enqueued a message
    agent.inbox.put.assert_called_once()
    msg = agent.inbox.put.call_args[0][0]
    assert msg.sender == "system"
    assert "gmail box" in msg.content
    assert 'gmail(action="check")' in msg.content
    # Should have logged
    agent._log.assert_called_once()
    # Should have signaled mail arrival
    agent._mail_arrived.set.assert_called_once()


def test_duplicate_send_blocked(tmp_path):
    """Sending the same message 3+ times should be blocked."""
    agent = MagicMock()
    agent._working_dir = tmp_path
    svc = MagicMock()
    svc.address = "agent@gmail.com"
    svc.send.return_value = None
    mgr = GmailManager(agent, gmail_service=svc, tcp_alias="127.0.0.1:8399")

    args = {"action": "send", "address": "user@gmail.com", "subject": "x", "message": "same"}

    # First two sends should succeed (free passes)
    r1 = mgr.handle(args)
    assert r1["status"] == "delivered"
    r2 = mgr.handle(args)
    assert r2["status"] == "delivered"

    # Third identical send should be blocked
    r3 = mgr.handle(args)
    assert r3["status"] == "blocked"


def test_send_with_attachments(tmp_path):
    """Manager should pass attachments to the gmail service."""
    agent = MagicMock()
    agent._working_dir = tmp_path
    svc = MagicMock()
    svc.address = "agent@gmail.com"
    svc.send.return_value = None
    mgr = GmailManager(agent, gmail_service=svc, tcp_alias="127.0.0.1:8399")

    # Create a test file
    img = tmp_path / "photo.png"
    img.write_bytes(b"\x89PNG")

    result = mgr.handle({
        "action": "send",
        "address": "user@gmail.com",
        "subject": "photo",
        "message": "see attached",
        "attachments": [str(img)],
    })

    assert result["status"] == "delivered"
    call_payload = svc.send.call_args[0][1]
    assert call_payload["attachments"] == [str(img)]


def test_send_with_relative_attachment(tmp_path):
    """Attachments with relative paths should resolve from working dir."""
    agent = MagicMock()
    agent._working_dir = tmp_path
    svc = MagicMock()
    svc.address = "agent@gmail.com"
    svc.send.return_value = None
    mgr = GmailManager(agent, gmail_service=svc, tcp_alias="127.0.0.1:8399")

    # Create a test file inside working dir
    img = tmp_path / "photo.png"
    img.write_bytes(b"\x89PNG")

    result = mgr.handle({
        "action": "send",
        "address": "user@gmail.com",
        "subject": "photo",
        "message": "see attached",
        "attachments": ["photo.png"],
    })

    assert result["status"] == "delivered"
    call_payload = svc.send.call_args[0][1]
    # Should be resolved to absolute path
    assert call_payload["attachments"] == [str(img)]


def test_read_includes_attachment_metadata(tmp_path):
    """Reading an email with attachments should include attachment info."""
    agent = MagicMock()
    agent._working_dir = tmp_path
    svc = MagicMock()
    svc.address = "agent@gmail.com"
    mgr = GmailManager(agent, gmail_service=svc, tcp_alias="127.0.0.1:8399")

    eid = "email-with-attachment"
    msg_dir = tmp_path / "gmail" / "inbox" / eid
    msg_dir.mkdir(parents=True)
    att_dir = msg_dir / "attachments"
    att_dir.mkdir()
    (att_dir / "photo.png").write_bytes(b"\x89PNG")
    (msg_dir / "message.json").write_text(json.dumps({
        "from": "user@gmail.com", "to": ["agent@gmail.com"],
        "subject": "photo", "message": "see attached",
        "_mailbox_id": eid, "received_at": "2026-03-19T12:00:00Z",
        "attachments": [{"filename": "photo.png", "path": str(att_dir / "photo.png"), "size": 4, "content_type": "image/png"}],
    }))

    result = mgr.handle({"action": "read", "email_id": [eid]})
    assert result["status"] == "ok"
    email = result["emails"][0]
    assert len(email["attachments"]) == 1
    assert email["attachments"][0]["filename"] == "photo.png"


def test_start_stop_lifecycle(tmp_path):
    agent = MagicMock()
    agent._working_dir = tmp_path
    svc = MagicMock()
    svc.address = "agent@gmail.com"
    mgr = GmailManager(agent, gmail_service=svc, tcp_alias="127.0.0.1:8399")
    mgr._bridge = MagicMock()

    mgr.start()
    svc.listen.assert_called_once()
    mgr._bridge.listen.assert_called_once()

    mgr.stop()
    svc.stop.assert_called_once()
    mgr._bridge.stop.assert_called_once()
