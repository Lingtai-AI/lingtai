"""Tests for the talk capability and TTSService."""
from pathlib import Path
from unittest.mock import MagicMock

import pytest

from lingtai.capabilities.talk import TalkManager, setup as setup_talk
from lingtai.services.tts import TTSService


class FakeTTSService(TTSService):
    """Fake TTSService that writes a dummy file and records calls."""

    def __init__(self, *, fail: bool = False, error_msg: str = "boom"):
        self.calls: list[dict] = []
        self._fail = fail
        self._error_msg = error_msg

    def synthesize(self, text, *, voice=None, output_dir=None, **kwargs):
        self.calls.append({"text": text, "voice": voice, "output_dir": output_dir, **kwargs})
        if self._fail:
            raise RuntimeError(self._error_msg)
        assert output_dir is not None
        output_dir.mkdir(parents=True, exist_ok=True)
        out_path = output_dir / "fake_audio.wav"
        out_path.write_bytes(b"FAKE_WAV")
        return out_path


def make_mock_agent(tmp_path):
    agent = MagicMock()
    agent.working_dir = tmp_path
    return agent


class TestTalkManager:
    def test_synthesize_success(self, tmp_path):
        """TalkManager delegates to TTSService and returns file path."""
        svc = FakeTTSService()
        mgr = TalkManager(working_dir=tmp_path, tts_service=svc)
        result = mgr.handle({"text": "Hello world"})
        assert result["status"] == "ok"
        assert Path(result["file_path"]).name == "fake_audio.wav"
        assert len(svc.calls) == 1
        assert svc.calls[0]["text"] == "Hello world"

    def test_voice_id_passed_as_voice(self, tmp_path):
        """voice_id from args is forwarded as voice kwarg."""
        svc = FakeTTSService()
        mgr = TalkManager(working_dir=tmp_path, tts_service=svc)
        mgr.handle({"text": "Hi", "voice_id": "Puck"})
        assert svc.calls[0]["voice"] == "Puck"

    def test_optional_params_forwarded(self, tmp_path):
        """emotion and speed are forwarded as kwargs."""
        svc = FakeTTSService()
        mgr = TalkManager(working_dir=tmp_path, tts_service=svc)
        mgr.handle({"text": "Hi", "emotion": "sad", "speed": 1.5})
        assert svc.calls[0]["emotion"] == "sad"
        assert svc.calls[0]["speed"] == 1.5

    def test_service_error_caught(self, tmp_path):
        """RuntimeError from TTSService is caught and returned as error dict."""
        svc = FakeTTSService(fail=True, error_msg="quota exceeded")
        mgr = TalkManager(working_dir=tmp_path, tts_service=svc)
        result = mgr.handle({"text": "hello"})
        assert result["status"] == "error"
        assert "quota exceeded" in result["message"]

    def test_missing_text(self, tmp_path):
        svc = FakeTTSService()
        mgr = TalkManager(working_dir=tmp_path, tts_service=svc)
        result = mgr.handle({})
        assert result["status"] == "error"
        assert "text" in result["message"]


class TestSetupTalk:
    def test_setup_with_tts_service(self, tmp_path):
        """setup() accepts a tts_service directly."""
        agent = make_mock_agent(tmp_path)
        svc = FakeTTSService()
        mgr = setup_talk(agent, tts_service=svc)
        assert isinstance(mgr, TalkManager)
        agent.add_tool.assert_called_once()

    def test_setup_with_provider(self, tmp_path, monkeypatch):
        """setup() with provider= creates a service via factory."""
        from lingtai.services import tts as tts_mod
        fake_svc = FakeTTSService()
        monkeypatch.setattr(tts_mod, "create_tts_service", lambda provider, **kw: fake_svc)
        agent = make_mock_agent(tmp_path)
        mgr = setup_talk(agent, provider="minimax", api_key="test-key")
        assert isinstance(mgr, TalkManager)
        agent.add_tool.assert_called_once()

    def test_setup_no_provider_no_service_raises(self, tmp_path):
        """setup() with neither provider nor tts_service raises ValueError."""
        agent = make_mock_agent(tmp_path)
        with pytest.raises(ValueError, match="provider"):
            setup_talk(agent)


class TestTTSServiceABC:
    def test_abc_not_instantiable(self):
        with pytest.raises(TypeError):
            TTSService()

    def test_factory_unknown_provider(self):
        from lingtai.services.tts import create_tts_service
        with pytest.raises(ValueError, match="Unknown TTS provider"):
            create_tts_service("nonexistent")
