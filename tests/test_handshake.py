"""Tests for lingtai_kernel.handshake utility."""
from __future__ import annotations

import json
import time
from pathlib import Path

import pytest

from lingtai_kernel.handshake import is_agent, is_alive, manifest


@pytest.fixture
def agent_dir(tmp_path):
    """Create a minimal agent working directory."""
    meta = {"agent_id": "abc123", "agent_name": "test"}
    (tmp_path / ".agent.json").write_text(json.dumps(meta))
    return tmp_path


def test_is_agent_true(agent_dir):
    assert is_agent(agent_dir) is True


def test_is_agent_false(tmp_path):
    assert is_agent(tmp_path) is False


def test_is_agent_str_path(agent_dir):
    assert is_agent(str(agent_dir)) is True


def test_is_alive_fresh(agent_dir):
    (agent_dir / ".agent.heartbeat").write_text(str(time.time()))
    assert is_alive(agent_dir) is True


def test_is_alive_stale(agent_dir):
    (agent_dir / ".agent.heartbeat").write_text(str(time.time() - 5.0))
    assert is_alive(agent_dir) is False


def test_is_alive_no_heartbeat(agent_dir):
    assert is_alive(agent_dir) is False


def test_is_alive_custom_threshold(agent_dir):
    (agent_dir / ".agent.heartbeat").write_text(str(time.time() - 3.0))
    assert is_alive(agent_dir, threshold=5.0) is True
    assert is_alive(agent_dir, threshold=2.0) is False


def test_manifest_returns_dict(agent_dir):
    result = manifest(agent_dir)
    assert result == {"agent_id": "abc123", "agent_name": "test"}


def test_manifest_missing_file(tmp_path):
    with pytest.raises(FileNotFoundError):
        manifest(tmp_path)
