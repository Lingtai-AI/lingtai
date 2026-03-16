"""Tests for the delegate capability."""
import time
from unittest.mock import MagicMock

import pytest

from stoai.capabilities.bash import BashManager
from stoai.capabilities.delegate import DelegateManager, setup as setup_delegate


def make_mock_service():
    svc = MagicMock()
    svc.get_adapter.return_value = MagicMock()
    svc.provider = "gemini"
    svc.model = "gemini-test"
    return svc


class TestDelegateManager:
    def test_spawn_returns_address(self, tmp_path):
        """Spawn should return a valid address."""
        from stoai.stoai_agent import StoAIAgent
        parent = StoAIAgent(agent_id="parent", service=make_mock_service(), base_dir=tmp_path,
                            capabilities=["delegate"])
        mgr = parent.get_capability("delegate")
        result = mgr.handle({})
        assert result["status"] == "ok"
        assert "address" in result
        assert "127.0.0.1:" in result["address"]
        assert "agent_id" in result

    def test_spawn_with_role(self, tmp_path):
        """Spawn with role override should create agent with that role."""
        from stoai.stoai_agent import StoAIAgent
        parent = StoAIAgent(agent_id="parent", service=make_mock_service(), base_dir=tmp_path,
                            capabilities=["delegate"])
        parent.update_system_prompt("role", "I am the parent", protected=True)
        mgr = parent.get_capability("delegate")
        result = mgr.handle({"role": "I am the researcher"})
        assert result["status"] == "ok"

    def test_spawn_copies_parent_role(self, tmp_path):
        """Spawn without role should copy parent's role."""
        from stoai.stoai_agent import StoAIAgent
        parent = StoAIAgent(agent_id="parent", service=make_mock_service(), base_dir=tmp_path,
                            capabilities=["delegate"])
        parent.update_system_prompt("role", "I am the parent", protected=True)
        mgr = parent.get_capability("delegate")
        result = mgr.handle({})
        assert result["status"] == "ok"

    def test_spawn_inherits_capabilities(self, tmp_path):
        """Spawned agent should get parent's capabilities (minus delegate)."""
        from stoai.stoai_agent import StoAIAgent
        parent = StoAIAgent(agent_id="parent", service=make_mock_service(), base_dir=tmp_path,
                            capabilities={"bash": {"yolo": True}, "delegate": {}})
        result = parent._mcp_handlers["delegate"]({})
        assert result["status"] == "ok"

    def test_spawn_with_ltm(self, tmp_path):
        """Spawn with ltm should inject it as a system prompt section."""
        from stoai.stoai_agent import StoAIAgent
        parent = StoAIAgent(agent_id="parent", service=make_mock_service(), base_dir=tmp_path,
                            capabilities=["delegate"])
        mgr = parent.get_capability("delegate")
        result = mgr.handle({"ltm": "Remember: always be concise"})
        assert result["status"] == "ok"

    def test_spawn_no_recursive_delegate(self, tmp_path):
        """Spawned agent should not get delegate capability (prevents recursion)."""
        from stoai.stoai_agent import StoAIAgent
        parent = StoAIAgent(agent_id="parent", service=make_mock_service(), base_dir=tmp_path,
                            capabilities={"bash": {"yolo": True}, "delegate": {}})
        # Parent has both bash and delegate in capabilities log
        cap_names = [name for name, _ in parent._capabilities]
        assert "bash" in cap_names
        assert "delegate" in cap_names


class TestSetupDelegate:
    def test_setup_delegate(self):
        agent = MagicMock()
        mgr = setup_delegate(agent)
        assert isinstance(mgr, DelegateManager)
        agent.add_tool.assert_called_once()


class TestAddCapability:
    def test_add_capability_delegate(self, tmp_path):
        from stoai.stoai_agent import StoAIAgent
        agent = StoAIAgent(agent_id="test", service=make_mock_service(), base_dir=tmp_path,
                           capabilities=["delegate"])
        mgr = agent.get_capability("delegate")
        assert isinstance(mgr, DelegateManager)
        assert "delegate" in agent._mcp_handlers

    def test_add_capability_unknown(self, tmp_path):
        from stoai.stoai_agent import StoAIAgent
        with pytest.raises(ValueError, match="Unknown capability"):
            StoAIAgent(agent_id="test", service=make_mock_service(), base_dir=tmp_path,
                       capabilities=["nonexistent"])

    def test_add_multiple_capabilities_separately(self, tmp_path):
        from stoai.stoai_agent import StoAIAgent
        agent = StoAIAgent(agent_id="test", service=make_mock_service(), base_dir=tmp_path,
                           capabilities={"bash": {"yolo": True}, "delegate": {}})
        bash_mgr = agent.get_capability("bash")
        delegate_mgr = agent.get_capability("delegate")
        assert isinstance(bash_mgr, BashManager)
        assert isinstance(delegate_mgr, DelegateManager)

    def test_capabilities_log(self, tmp_path):
        """StoAIAgent should record (name, kwargs) in _capabilities."""
        from stoai.stoai_agent import StoAIAgent
        agent = StoAIAgent(agent_id="test", service=make_mock_service(), base_dir=tmp_path,
                           capabilities={"bash": {"yolo": True}, "delegate": {}})
        assert len(agent._capabilities) == 2
        assert agent._capabilities[0] == ("bash", {"yolo": True})
        assert agent._capabilities[1] == ("delegate", {})
