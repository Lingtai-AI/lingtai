"""Tests for system intrinsic — agent identity management (role + ltm)."""
from __future__ import annotations

import subprocess
from unittest.mock import MagicMock

import pytest

from stoai.intrinsics import ALL_INTRINSICS
from stoai.base_agent import BaseAgent


def make_mock_service():
    svc = MagicMock()
    svc.get_adapter.return_value = MagicMock()
    svc.provider = "gemini"
    svc.model = "gemini-test"
    return svc


def test_system_in_all_intrinsics():
    assert "system" in ALL_INTRINSICS
    info = ALL_INTRINSICS["system"]
    assert "schema" in info
    assert "description" in info
    assert info["handler"] is None  # handled by BaseAgent


def test_memory_not_in_all_intrinsics():
    """memory intrinsic should be completely removed."""
    assert "memory" not in ALL_INTRINSICS


def test_system_wired_in_agent(tmp_path):
    agent = BaseAgent(agent_id="test", service=make_mock_service(), base_dir=tmp_path)
    assert "system" in agent._intrinsics
    assert "memory" not in agent._intrinsics
    agent.stop(timeout=1.0)


def test_system_view_ltm_empty(tmp_path):
    agent = BaseAgent(agent_id="test", service=make_mock_service(), base_dir=tmp_path)
    agent.start()
    try:
        result = agent._handle_system({"action": "view", "object": "ltm"})
        assert result["status"] == "ok"
        assert result["content"] == ""
        assert result["path"].endswith("system/ltm.md")
    finally:
        agent.stop()


def test_system_view_role_empty(tmp_path):
    agent = BaseAgent(agent_id="test", service=make_mock_service(), base_dir=tmp_path)
    agent.start()
    try:
        result = agent._handle_system({"action": "view", "object": "role"})
        assert result["status"] == "ok"
        assert result["content"] == ""
        assert result["path"].endswith("system/role.md")
    finally:
        agent.stop()


def test_system_view_ltm_with_content(tmp_path):
    agent = BaseAgent(agent_id="test", service=make_mock_service(), base_dir=tmp_path)
    agent.start()
    try:
        ltm_file = agent.working_dir / "system" / "ltm.md"
        ltm_file.write_text("# Memory\n\nimportant fact\n")
        result = agent._handle_system({"action": "view", "object": "ltm"})
        assert result["status"] == "ok"
        assert "important fact" in result["content"]
    finally:
        agent.stop()


def test_system_load_ltm(tmp_path):
    agent = BaseAgent(agent_id="test", service=make_mock_service(), base_dir=tmp_path)
    agent.start()
    try:
        ltm_file = agent.working_dir / "system" / "ltm.md"
        ltm_file.write_text("# Memory\n\nimportant fact\n")
        result = agent._handle_system({"action": "load", "object": "ltm"})
        assert result["status"] == "ok"
        assert result["diff"]["changed"] is True
        section = agent._prompt_manager.read_section("ltm")
        assert "important fact" in section
    finally:
        agent.stop()


def test_system_load_role(tmp_path):
    agent = BaseAgent(agent_id="test", service=make_mock_service(), base_dir=tmp_path)
    agent.start()
    try:
        role_file = agent.working_dir / "system" / "role.md"
        role_file.write_text("You are a researcher")
        result = agent._handle_system({"action": "load", "object": "role"})
        assert result["status"] == "ok"
        section = agent._prompt_manager.read_section("role")
        assert "researcher" in section
    finally:
        agent.stop()


def test_system_load_empty_removes_section(tmp_path):
    agent = BaseAgent(agent_id="test", service=make_mock_service(), base_dir=tmp_path)
    agent.start()
    try:
        ltm_file = agent.working_dir / "system" / "ltm.md"
        ltm_file.write_text("some content")
        agent._handle_system({"action": "load", "object": "ltm"})
        assert agent._prompt_manager.read_section("ltm") is not None

        ltm_file.write_text("")
        agent._handle_system({"action": "load", "object": "ltm"})
        section = agent._prompt_manager.read_section("ltm")
        assert section is None or section.strip() == ""
    finally:
        agent.stop()


def test_system_diff_ltm(tmp_path):
    agent = BaseAgent(agent_id="test", service=make_mock_service(), base_dir=tmp_path)
    agent.start()
    try:
        ltm_file = agent.working_dir / "system" / "ltm.md"
        # First load to commit initial state
        ltm_file.write_text("first version\n")
        agent._handle_system({"action": "load", "object": "ltm"})
        # Edit without loading
        ltm_file.write_text("second version\n")
        result = agent._handle_system({"action": "diff", "object": "ltm"})
        assert result["status"] == "ok"
        assert "first version" in result["git_diff"] or "second version" in result["git_diff"]
    finally:
        agent.stop()


def test_system_diff_no_changes(tmp_path):
    agent = BaseAgent(agent_id="test", service=make_mock_service(), base_dir=tmp_path)
    agent.start()
    try:
        result = agent._handle_system({"action": "diff", "object": "ltm"})
        assert result["status"] == "ok"
        assert result["git_diff"] == ""
    finally:
        agent.stop()


def test_system_load_no_change_no_commit(tmp_path):
    agent = BaseAgent(agent_id="test", service=make_mock_service(), base_dir=tmp_path)
    agent.start()
    try:
        agent._handle_system({"action": "load", "object": "ltm"})
        result = agent._handle_system({"action": "load", "object": "ltm"})
        assert result["diff"]["changed"] is False
    finally:
        agent.stop()


def test_system_unknown_action(tmp_path):
    agent = BaseAgent(agent_id="test", service=make_mock_service(), base_dir=tmp_path)
    result = agent._handle_system({"action": "bogus", "object": "ltm"})
    assert "error" in result
    agent.stop(timeout=1.0)


def test_system_unknown_object(tmp_path):
    agent = BaseAgent(agent_id="test", service=make_mock_service(), base_dir=tmp_path)
    result = agent._handle_system({"action": "view", "object": "bogus"})
    assert "error" in result
    agent.stop(timeout=1.0)


# ---------------------------------------------------------------------------
# Lifecycle integration (constructor arg, stop persistence, resume auto-load)
# ---------------------------------------------------------------------------


def test_ltm_constructor_arg_writes_to_system(tmp_path):
    """ltm= constructor arg should write to system/ltm.md."""
    agent = BaseAgent(
        agent_id="test", service=make_mock_service(), base_dir=tmp_path,
        ltm="initial memory",
    )
    ltm_file = agent.working_dir / "system" / "ltm.md"
    assert ltm_file.is_file()
    assert ltm_file.read_text() == "initial memory"
    agent.stop(timeout=1.0)


def test_role_constructor_arg_writes_to_system(tmp_path):
    """role= constructor arg should write to system/role.md."""
    agent = BaseAgent(
        agent_id="test", service=make_mock_service(), base_dir=tmp_path,
        role="researcher",
    )
    role_file = agent.working_dir / "system" / "role.md"
    assert role_file.is_file()
    assert role_file.read_text() == "researcher"
    agent.stop(timeout=1.0)


def test_existing_system_files_not_overwritten(tmp_path):
    """If system/ltm.md already exists, constructor arg should not overwrite it."""
    working_dir = tmp_path / "test"
    working_dir.mkdir()
    system_dir = working_dir / "system"
    system_dir.mkdir()
    (system_dir / "ltm.md").write_text("existing content")

    agent = BaseAgent(
        agent_id="test", service=make_mock_service(), base_dir=tmp_path,
        ltm="constructor ltm",
    )
    assert (agent.working_dir / "system" / "ltm.md").read_text() == "existing content"
    agent.stop(timeout=1.0)


def test_system_creates_files_if_missing(tmp_path):
    """system intrinsic should create missing files."""
    agent = BaseAgent(agent_id="test", service=make_mock_service(), base_dir=tmp_path)
    agent.start()
    try:
        import shutil
        system_dir = agent.working_dir / "system"
        if system_dir.exists():
            shutil.rmtree(system_dir)

        result = agent._handle_system({"action": "view", "object": "ltm"})
        assert result["status"] == "ok"
        assert (agent.working_dir / "system" / "ltm.md").is_file()
    finally:
        agent.stop()
