"""Tests for karma/nirvana lifecycle control via system intrinsic."""
from __future__ import annotations

import threading
import time
from pathlib import Path
from unittest.mock import MagicMock

import pytest

from lingtai_kernel.base_agent import BaseAgent
from lingtai_kernel.state import AgentState


def _make_agent(tmp_path, **kwargs):
    """Create a minimal BaseAgent for testing."""
    svc = MagicMock()
    svc.create_session.return_value = MagicMock()
    kwargs.setdefault("agent_id", "test000000ab")
    agent = BaseAgent(svc, base_dir=str(tmp_path), **kwargs)
    return agent


class TestSignalFiles:
    """Signal file detection in heartbeat loop."""

    def test_silence_signal_sets_cancel_event(self, tmp_path):
        agent = _make_agent(tmp_path)
        agent.start()
        try:
            # Write .silence signal file
            (agent.working_dir / ".silence").write_text("")
            # Wait for heartbeat to detect it
            time.sleep(2.0)
            assert agent._cancel_event.is_set()
            assert not (agent.working_dir / ".silence").exists(), "signal file should be deleted"
        finally:
            agent.stop()

    def test_quell_signal_sets_shutdown(self, tmp_path):
        agent = _make_agent(tmp_path)
        agent.start()
        # Write .quell signal file
        (agent.working_dir / ".quell").write_text("")
        # Wait for agent to shut down
        time.sleep(3.0)
        assert agent._shutdown.is_set()
        assert agent.state == AgentState.DORMANT
        assert not (agent.working_dir / ".quell").exists(), "signal file should be deleted"


class TestSystemIntrinsicKarma:
    """Karma actions in system intrinsic."""

    def test_silence_requires_karma_admin(self, tmp_path):
        agent = _make_agent(tmp_path, admin={})
        from lingtai_kernel.intrinsics.system import handle
        result = handle(agent, {"action": "silence", "address": "/some/path"})
        assert "error" in result

    def test_silence_with_karma_admin(self, tmp_path):
        target_dir = tmp_path / "target"
        target_dir.mkdir()
        (target_dir / ".agent.json").write_text('{"agent_id": "t1"}')
        (target_dir / ".agent.heartbeat").write_text(str(time.time()))

        sender_base = tmp_path / "sender"
        sender_base.mkdir()
        agent = _make_agent(sender_base, admin={"karma": True})
        from lingtai_kernel.intrinsics.system import handle
        result = handle(agent, {"action": "silence", "address": str(target_dir)})
        assert result["status"] == "silenced"
        assert (target_dir / ".silence").is_file()

    def test_quell_writes_signal_file(self, tmp_path):
        target_dir = tmp_path / "target"
        target_dir.mkdir()
        (target_dir / ".agent.json").write_text('{"agent_id": "t1"}')
        (target_dir / ".agent.heartbeat").write_text(str(time.time()))

        sender_base = tmp_path / "sender"
        sender_base.mkdir()
        agent = _make_agent(sender_base, admin={"karma": True})
        from lingtai_kernel.intrinsics.system import handle
        result = handle(agent, {"action": "quell", "address": str(target_dir)})
        assert result["status"] == "quelled"
        assert (target_dir / ".quell").is_file()

    def test_quell_rejects_dormant_target(self, tmp_path):
        target_dir = tmp_path / "target"
        target_dir.mkdir()
        (target_dir / ".agent.json").write_text('{"agent_id": "t1"}')

        sender_base = tmp_path / "sender"
        sender_base.mkdir()
        agent = _make_agent(sender_base, admin={"karma": True})
        from lingtai_kernel.intrinsics.system import handle
        result = handle(agent, {"action": "quell", "address": str(target_dir)})
        assert "error" in result

    def test_silence_self_rejected(self, tmp_path):
        agent = _make_agent(tmp_path, admin={"karma": True})
        from lingtai_kernel.intrinsics.system import handle
        result = handle(agent, {"action": "silence", "address": str(agent.working_dir)})
        assert "error" in result

    def test_annihilate_requires_nirvana_admin(self, tmp_path):
        sender_base = tmp_path / "sender"
        sender_base.mkdir()
        agent = _make_agent(sender_base, admin={"karma": True})
        from lingtai_kernel.intrinsics.system import handle
        result = handle(agent, {"action": "annihilate", "address": "/some/path"})
        assert "error" in result

    def test_annihilate_with_nirvana_admin(self, tmp_path):
        target_dir = tmp_path / "target"
        target_dir.mkdir()
        (target_dir / ".agent.json").write_text('{"agent_id": "t1"}')

        sender_base = tmp_path / "sender"
        sender_base.mkdir()
        agent = _make_agent(sender_base, admin={"karma": True, "nirvana": True})
        from lingtai_kernel.intrinsics.system import handle
        result = handle(agent, {"action": "annihilate", "address": str(target_dir)})
        assert result["status"] == "annihilated"
        assert not target_dir.exists()

    def test_annihilate_self_rejected(self, tmp_path):
        agent = _make_agent(tmp_path, admin={"karma": True, "nirvana": True})
        from lingtai_kernel.intrinsics.system import handle
        result = handle(agent, {"action": "annihilate", "address": str(agent.working_dir)})
        assert "error" in result

    def test_revive_rejects_alive_target(self, tmp_path):
        target_dir = tmp_path / "target"
        target_dir.mkdir()
        (target_dir / ".agent.json").write_text('{"agent_id": "t1"}')
        (target_dir / ".agent.heartbeat").write_text(str(time.time()))

        sender_base = tmp_path / "sender"
        sender_base.mkdir()
        agent = _make_agent(sender_base, admin={"karma": True})
        from lingtai_kernel.intrinsics.system import handle
        result = handle(agent, {"action": "revive", "address": str(target_dir)})
        assert "error" in result
        assert "already running" in result["message"]

    def test_revive_without_handler_returns_error(self, tmp_path):
        target_dir = tmp_path / "target"
        target_dir.mkdir()
        (target_dir / ".agent.json").write_text('{"agent_id": "t1"}')

        sender_base = tmp_path / "sender"
        sender_base.mkdir()
        agent = _make_agent(sender_base, admin={"karma": True})
        from lingtai_kernel.intrinsics.system import handle
        result = handle(agent, {"action": "revive", "address": str(target_dir)})
        assert "error" in result
        assert "not supported" in result["message"].lower()
