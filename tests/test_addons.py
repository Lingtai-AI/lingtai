from __future__ import annotations
from unittest.mock import MagicMock


def test_addon_registry():
    from stoai.addons import _BUILTIN
    assert "gmail" in _BUILTIN


def test_agent_addon_lifecycle():
    """Agent should accept addons parameter."""
    from stoai.agent import Agent
    import inspect
    sig = inspect.signature(Agent.__init__)
    assert "addons" in sig.parameters
