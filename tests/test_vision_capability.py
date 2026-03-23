"""Tests for vision capability and VisionService."""
from __future__ import annotations

from unittest.mock import MagicMock, patch

import pytest

from lingtai.capabilities.vision import VisionManager, setup
from lingtai.services.vision import VisionService, create_vision_service


def make_mock_service():
    svc = MagicMock()
    svc.provider = "gemini"
    svc.model = "gemini-test"
    svc._key_resolver = MagicMock(return_value="fake-key")
    return svc


def make_mock_agent(tmp_path, svc=None):
    agent = MagicMock()
    agent.service = svc or make_mock_service()
    agent._config = MagicMock()
    agent._config.language = "en"
    agent._working_dir = tmp_path
    return agent


def test_vision_added_by_setup(tmp_path):
    """setup() should register the vision tool on the agent."""
    mock_svc = MagicMock(spec=VisionService)
    agent = make_mock_agent(tmp_path)
    mgr = setup(agent, vision_service=mock_svc)
    agent.add_tool.assert_called_once()
    assert agent.add_tool.call_args[1]["schema"] is not None or agent.add_tool.call_args[0][1] is not None
    assert isinstance(mgr, VisionManager)


def test_vision_with_dedicated_service(tmp_path):
    """Vision capability should use VisionService if provided."""
    mock_vision_svc = MagicMock(spec=VisionService)
    mock_vision_svc.analyze_image.return_value = "A dog in the park"

    agent = make_mock_agent(tmp_path)
    mgr = VisionManager(agent, vision_service=mock_vision_svc)

    img_path = tmp_path / "test.jpg"
    img_path.write_bytes(b"\xff\xd8\xff fake jpeg")
    result = mgr.handle({"image_path": str(img_path)})
    assert result["status"] == "ok"
    assert "dog" in result["analysis"]
    mock_vision_svc.analyze_image.assert_called_once()


def test_vision_missing_image(tmp_path):
    """Vision should return error for missing image file."""
    mock_vision_svc = MagicMock(spec=VisionService)
    agent = make_mock_agent(tmp_path)
    mgr = VisionManager(agent, vision_service=mock_vision_svc)
    result = mgr.handle({"image_path": "/nonexistent/image.png"})
    assert result.get("status") == "error"


def test_vision_relative_path(tmp_path):
    """VisionManager should resolve relative paths against working directory."""
    mock_vision_svc = MagicMock(spec=VisionService)
    mock_vision_svc.analyze_image.return_value = "An image"

    agent = make_mock_agent(tmp_path)
    mgr = VisionManager(agent, vision_service=mock_vision_svc)
    img_path = tmp_path / "photo.png"
    img_path.write_bytes(b"\x89PNG fake")
    result = mgr.handle({"image_path": "photo.png"})
    assert result["status"] == "ok"
    mock_vision_svc.analyze_image.assert_called_once_with(str(img_path), prompt="Describe what you see in this image.")


def test_vision_service_error_handled(tmp_path):
    """VisionManager should catch VisionService exceptions and return error dict."""
    mock_vision_svc = MagicMock(spec=VisionService)
    mock_vision_svc.analyze_image.side_effect = RuntimeError("API down")

    agent = make_mock_agent(tmp_path)
    mgr = VisionManager(agent, vision_service=mock_vision_svc)
    img_path = tmp_path / "test.png"
    img_path.write_bytes(b"\x89PNG fake")
    result = mgr.handle({"image_path": str(img_path)})
    assert result["status"] == "error"
    assert "API down" in result["message"]


def test_vision_empty_response_is_error(tmp_path):
    """VisionManager should return error when service returns empty string."""
    mock_vision_svc = MagicMock(spec=VisionService)
    mock_vision_svc.analyze_image.return_value = ""

    agent = make_mock_agent(tmp_path)
    mgr = VisionManager(agent, vision_service=mock_vision_svc)
    img_path = tmp_path / "test.png"
    img_path.write_bytes(b"\x89PNG fake")
    result = mgr.handle({"image_path": str(img_path)})
    assert result["status"] == "error"


def test_vision_setup_with_provider_and_key(tmp_path):
    """setup() should create a VisionService from provider + api_key."""
    with patch("lingtai.capabilities.vision.create_vision_service") as mock_factory:
        mock_svc = MagicMock(spec=VisionService)
        mock_factory.return_value = mock_svc

        agent = make_mock_agent(tmp_path)
        mgr = setup(agent, provider="anthropic", api_key="sk-test")

        mock_factory.assert_called_once_with("anthropic", api_key="sk-test")
        assert isinstance(mgr, VisionManager)


def test_vision_setup_requires_provider_or_service(tmp_path):
    """setup() without provider or service raises ValueError."""
    agent = make_mock_agent(tmp_path)
    with pytest.raises(ValueError, match="vision capability requires"):
        setup(agent)


def test_create_vision_service_unknown_provider():
    """create_vision_service should raise ValueError for unknown providers."""
    with pytest.raises(ValueError, match="Unsupported vision provider"):
        create_vision_service("unknown_provider", api_key="key")


def test_vision_service_abc_cannot_instantiate():
    """VisionService ABC should not be instantiable directly."""
    with pytest.raises(TypeError):
        VisionService()


def test_vision_empty_image_path(tmp_path):
    """VisionManager should return error for empty image path."""
    mock_vision_svc = MagicMock(spec=VisionService)
    agent = make_mock_agent(tmp_path)
    mgr = VisionManager(agent, vision_service=mock_vision_svc)
    result = mgr.handle({"image_path": ""})
    assert result["status"] == "error"
    assert "image_path" in result["message"].lower() or "provide" in result["message"].lower()


def test_vision_setup_no_provider_raises(tmp_path):
    """setup() without provider or service should raise ValueError."""
    agent = make_mock_agent(tmp_path)
    with pytest.raises(ValueError, match="vision capability requires"):
        setup(agent)
