"""Tests for MailService and FilesystemMailService."""
import json
import threading
import time
from pathlib import Path

import pytest

from lingtai_kernel.services.mail import FilesystemMailService


def _setup_agent_dir(path: Path) -> Path:
    """Create a minimal agent directory with manifest and heartbeat."""
    path.mkdir(parents=True, exist_ok=True)
    (path / ".agent.json").write_text(json.dumps({
        "agent_id": path.name,
        "agent_name": path.name,
    }))
    (path / ".agent.heartbeat").write_text(str(time.time()))
    return path


def _keep_heartbeat_alive(path: Path, stop_event: threading.Event) -> threading.Thread:
    """Start a background thread that keeps the heartbeat fresh."""
    hb = path / ".agent.heartbeat"
    def loop():
        while not stop_event.is_set():
            try:
                hb.write_text(str(time.time()))
            except OSError:
                pass
            stop_event.wait(0.5)
    t = threading.Thread(target=loop, daemon=True)
    t.start()
    return t


class TestFilesystemMailService:
    def test_send_to_listener(self, tmp_path):
        """Test basic send/receive via filesystem."""
        sender_dir = _setup_agent_dir(tmp_path / "sender")
        receiver_dir = _setup_agent_dir(tmp_path / "receiver")

        received = []
        event = threading.Event()
        stop = threading.Event()
        _keep_heartbeat_alive(receiver_dir, stop)

        def on_message(msg):
            received.append(msg)
            event.set()

        listener = FilesystemMailService(working_dir=receiver_dir)
        listener.listen(on_message)

        try:
            sender = FilesystemMailService(working_dir=sender_dir)
            result = sender.send(
                str(receiver_dir),
                {"from": str(sender_dir), "to": str(receiver_dir), "message": "hello"},
            )
            assert result is None

            assert event.wait(timeout=5.0), "Message not received within timeout"
            assert len(received) == 1
            assert received[0]["message"] == "hello"
        finally:
            listener.stop()
            stop.set()

    def test_send_to_nonexistent_returns_error(self, tmp_path):
        """Sending to a non-existent agent directory should return an error string."""
        sender_dir = _setup_agent_dir(tmp_path / "sender")
        sender = FilesystemMailService(working_dir=sender_dir)
        result = sender.send(str(tmp_path / "nonexistent"), {"message": "hello"})
        assert isinstance(result, str)
        assert "No agent" in result

    def test_send_to_stopped_agent_returns_error(self, tmp_path):
        """Sending to an agent with stale heartbeat should return an error."""
        sender_dir = _setup_agent_dir(tmp_path / "sender")
        receiver_dir = _setup_agent_dir(tmp_path / "receiver")
        # Write a stale heartbeat (10 seconds old)
        (receiver_dir / ".agent.heartbeat").write_text(str(time.time() - 10))

        sender = FilesystemMailService(working_dir=sender_dir)
        result = sender.send(str(receiver_dir), {"message": "hello"})
        assert isinstance(result, str)
        assert "not running" in result

    def test_address_property(self, tmp_path):
        """Address should be the working directory path."""
        agent_dir = _setup_agent_dir(tmp_path / "agent")
        svc = FilesystemMailService(working_dir=agent_dir)
        assert svc.address == str(agent_dir)

    def test_stop_is_idempotent(self, tmp_path):
        """Calling stop multiple times should not raise."""
        agent_dir = _setup_agent_dir(tmp_path / "agent")
        svc = FilesystemMailService(working_dir=agent_dir)
        svc.stop()
        svc.stop()

    def test_multiple_messages(self, tmp_path):
        """Multiple messages should all be received."""
        sender_dir = _setup_agent_dir(tmp_path / "sender")
        receiver_dir = _setup_agent_dir(tmp_path / "receiver")

        received = []
        all_done = threading.Event()
        stop = threading.Event()
        _keep_heartbeat_alive(receiver_dir, stop)

        def on_message(msg):
            received.append(msg)
            if len(received) >= 3:
                all_done.set()

        listener = FilesystemMailService(working_dir=receiver_dir)
        listener.listen(on_message)

        try:
            sender = FilesystemMailService(working_dir=sender_dir)
            for i in range(3):
                sender.send(str(receiver_dir), {"message": f"msg-{i}"})
                time.sleep(0.05)

            assert all_done.wait(timeout=5.0), f"Only received {len(received)} of 3 messages"
            messages = [r["message"] for r in received]
            assert "msg-0" in messages
            assert "msg-1" in messages
            assert "msg-2" in messages
        finally:
            listener.stop()
            stop.set()


class TestMailAttachments:
    def test_send_with_attachment(self, tmp_path):
        """Sender copies attachment files into the recipient's inbox."""
        sender_dir = _setup_agent_dir(tmp_path / "sender")
        receiver_dir = _setup_agent_dir(tmp_path / "receiver")

        received = []
        event = threading.Event()
        stop = threading.Event()
        _keep_heartbeat_alive(receiver_dir, stop)

        def on_message(msg):
            received.append(msg)
            event.set()

        # Create a file to attach
        attachment = sender_dir / "image.png"
        attachment.write_bytes(b"\x89PNG_TEST_DATA")

        listener = FilesystemMailService(working_dir=receiver_dir)
        listener.listen(on_message)

        try:
            sender = FilesystemMailService(working_dir=sender_dir)
            result = sender.send(
                str(receiver_dir),
                {
                    "from": "sender",
                    "to": str(receiver_dir),
                    "message": "here is an image",
                    "attachments": [str(attachment)],
                },
            )
            assert result is None
            assert event.wait(timeout=5.0)
            msg = received[0]
            # Receiver should get local file paths
            assert "attachments" in msg
            assert len(msg["attachments"]) == 1
            recv_path = Path(msg["attachments"][0])
            assert recv_path.exists()
            assert recv_path.read_bytes() == b"\x89PNG_TEST_DATA"
            assert "mailbox" in str(recv_path)
        finally:
            listener.stop()
            stop.set()

    def test_send_without_attachment(self, tmp_path):
        """Messages without attachments still work normally."""
        sender_dir = _setup_agent_dir(tmp_path / "sender")
        receiver_dir = _setup_agent_dir(tmp_path / "receiver")

        received = []
        event = threading.Event()
        stop = threading.Event()
        _keep_heartbeat_alive(receiver_dir, stop)

        def on_message(msg):
            received.append(msg)
            event.set()

        listener = FilesystemMailService(working_dir=receiver_dir)
        listener.listen(on_message)

        try:
            sender = FilesystemMailService(working_dir=sender_dir)
            result = sender.send(
                str(receiver_dir),
                {"from": "sender", "to": str(receiver_dir), "message": "no attachments"},
            )
            assert result is None
            assert event.wait(timeout=5.0)
            assert received[0]["message"] == "no attachments"
        finally:
            listener.stop()
            stop.set()

    def test_attachment_file_not_found(self, tmp_path):
        """send() returns error when attachment file does not exist."""
        sender_dir = _setup_agent_dir(tmp_path / "sender")
        receiver_dir = _setup_agent_dir(tmp_path / "receiver")
        stop = threading.Event()
        _keep_heartbeat_alive(receiver_dir, stop)

        try:
            sender = FilesystemMailService(working_dir=sender_dir)
            result = sender.send(
                str(receiver_dir),
                {"from": "s", "to": "r", "message": "hi", "attachments": ["/nonexistent/file.png"]},
            )
            assert isinstance(result, str)
            assert "Attachment not found" in result
        finally:
            stop.set()

    def test_mailbox_directory_structure(self, tmp_path):
        """Received messages are saved in mailbox/<uuid>/message.json + attachments/."""
        sender_dir = _setup_agent_dir(tmp_path / "sender")
        receiver_dir = _setup_agent_dir(tmp_path / "receiver")

        received = []
        event = threading.Event()
        stop = threading.Event()
        _keep_heartbeat_alive(receiver_dir, stop)

        def on_message(msg):
            received.append(msg)
            event.set()

        attachment = sender_dir / "song.mp3"
        attachment.write_bytes(b"MP3_DATA")

        listener = FilesystemMailService(working_dir=receiver_dir)
        listener.listen(on_message)

        try:
            sender = FilesystemMailService(working_dir=sender_dir)
            sender.send(
                str(receiver_dir),
                {"from": "s", "to": "r", "message": "music", "attachments": [str(attachment)]},
            )
            assert event.wait(timeout=5.0)

            # Check mailbox structure
            mailbox = receiver_dir / "mailbox" / "inbox"
            assert mailbox.is_dir()
            msg_dirs = list(mailbox.iterdir())
            assert len(msg_dirs) == 1
            msg_dir = msg_dirs[0]
            assert (msg_dir / "message.json").is_file()
            assert (msg_dir / "attachments" / "song.mp3").is_file()
            assert (msg_dir / "attachments" / "song.mp3").read_bytes() == b"MP3_DATA"
        finally:
            listener.stop()
            stop.set()

    def test_message_without_attachments_also_persisted(self, tmp_path):
        """Even messages without attachments get persisted to mailbox."""
        sender_dir = _setup_agent_dir(tmp_path / "sender")
        receiver_dir = _setup_agent_dir(tmp_path / "receiver")

        received = []
        event = threading.Event()
        stop = threading.Event()
        _keep_heartbeat_alive(receiver_dir, stop)

        def on_message(msg):
            received.append(msg)
            event.set()

        listener = FilesystemMailService(working_dir=receiver_dir)
        listener.listen(on_message)

        try:
            sender = FilesystemMailService(working_dir=sender_dir)
            sender.send(str(receiver_dir), {"from": "s", "to": "r", "message": "plain"})
            assert event.wait(timeout=5.0)

            mailbox = receiver_dir / "mailbox" / "inbox"
            assert mailbox.is_dir()
            msg_dirs = list(mailbox.iterdir())
            assert len(msg_dirs) == 1
            assert (msg_dirs[0] / "message.json").is_file()
        finally:
            listener.stop()
            stop.set()
