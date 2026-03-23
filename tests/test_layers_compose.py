"""Tests for the compose capability and MusicGenService."""
from pathlib import Path
from unittest.mock import MagicMock, patch

import pytest

from lingtai.services.music_gen import MusicGenService, create_music_gen_service
from lingtai.capabilities.compose import ComposeManager, setup as setup_compose


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

class FakeMusicGenService(MusicGenService):
    """In-memory fake for testing."""

    def __init__(self, *, return_path: Path | None = None, error: str | None = None):
        self._return_path = return_path
        self._error = error
        self.calls: list[dict] = []

    def generate(self, prompt, *, lyrics=None, output_dir=None, **kwargs):
        self.calls.append({
            "prompt": prompt, "lyrics": lyrics, "output_dir": output_dir,
        })
        if self._error:
            raise RuntimeError(self._error)
        if self._return_path is not None:
            return self._return_path
        # Default: create a dummy file in output_dir
        assert output_dir is not None
        output_dir.mkdir(parents=True, exist_ok=True)
        path = output_dir / "test_music.mp3"
        path.write_bytes(b"FAKE_MP3")
        return path


def make_mock_agent(tmp_path):
    agent = MagicMock()
    agent.working_dir = tmp_path
    return agent


# ---------------------------------------------------------------------------
# MusicGenService ABC
# ---------------------------------------------------------------------------

class TestMusicGenServiceABC:
    def test_cannot_instantiate_abc(self):
        with pytest.raises(TypeError):
            MusicGenService()

    def test_subclass_must_implement_generate(self):
        class Incomplete(MusicGenService):
            pass
        with pytest.raises(TypeError):
            Incomplete()

    def test_subclass_works(self, tmp_path):
        svc = FakeMusicGenService(return_path=tmp_path / "out.mp3")
        result = svc.generate("jazz piano", lyrics="La la")
        assert result == tmp_path / "out.mp3"


class TestMusicGenFactory:
    def test_unknown_provider_raises(self):
        with pytest.raises(ValueError, match="Unknown music generation provider"):
            create_music_gen_service("nonexistent")

    def test_minimax_provider(self, monkeypatch):
        """Factory with provider='minimax' creates MiniMaxMusicGenService."""
        from lingtai.services.music_gen import minimax as minimax_mod
        created = []

        class FakeMiniMax(MusicGenService):
            def __init__(self, **kw):
                self.kw = kw
                created.append(self)
            def generate(self, prompt, **kw):
                return Path("/fake")

        monkeypatch.setattr(minimax_mod, "MiniMaxMusicGenService", FakeMiniMax)
        svc = create_music_gen_service("minimax", api_key="test-key")
        assert len(created) == 1
        assert created[0].kw["api_key"] == "test-key"


# ---------------------------------------------------------------------------
# ComposeManager
# ---------------------------------------------------------------------------

class TestComposeManager:
    def test_generate_music_success(self, tmp_path):
        """Service returns a path — manager wraps it in status dict."""
        out_file = tmp_path / "media" / "music" / "song.mp3"
        out_file.parent.mkdir(parents=True)
        out_file.write_bytes(b"MP3")

        svc = FakeMusicGenService(return_path=out_file)
        mgr = ComposeManager(working_dir=tmp_path, music_gen_service=svc)
        result = mgr.handle({"prompt": "jazz piano", "lyrics": "La la la"})

        assert result["status"] == "ok"
        assert result["file_path"] == str(out_file)
        assert svc.calls[0]["prompt"] == "jazz piano"
        assert svc.calls[0]["lyrics"] == "La la la"
        assert svc.calls[0]["output_dir"] == tmp_path / "media" / "music"

    def test_service_error(self, tmp_path):
        svc = FakeMusicGenService(error="rate limited")
        mgr = ComposeManager(working_dir=tmp_path, music_gen_service=svc)
        result = mgr.handle({"prompt": "jazz", "lyrics": "La"})
        assert result["status"] == "error"
        assert "rate limited" in result["message"]

    def test_missing_prompt(self, tmp_path):
        svc = FakeMusicGenService()
        mgr = ComposeManager(working_dir=tmp_path, music_gen_service=svc)
        result = mgr.handle({"lyrics": "La la la"})
        assert result["status"] == "error"
        assert "prompt" in result["message"]

    def test_missing_lyrics(self, tmp_path):
        svc = FakeMusicGenService()
        mgr = ComposeManager(working_dir=tmp_path, music_gen_service=svc)
        result = mgr.handle({"prompt": "jazz"})
        assert result["status"] == "error"
        assert "lyrics" in result["message"]


# ---------------------------------------------------------------------------
# setup()
# ---------------------------------------------------------------------------

class TestSetupCompose:
    def test_setup_with_explicit_service(self, tmp_path):
        agent = make_mock_agent(tmp_path)
        svc = FakeMusicGenService()
        mgr = setup_compose(agent, music_gen_service=svc)
        assert isinstance(mgr, ComposeManager)
        agent.add_tool.assert_called_once()

    def test_setup_requires_provider(self, tmp_path):
        """Without explicit service or provider, setup raises ValueError."""
        agent = make_mock_agent(tmp_path)
        with pytest.raises(ValueError, match="compose capability requires"):
            setup_compose(agent)

    def test_setup_with_factory(self, tmp_path, monkeypatch):
        """With provider, setup uses the factory."""
        from lingtai.services import music_gen as music_gen_mod

        fake_svc = FakeMusicGenService()
        monkeypatch.setattr(
            music_gen_mod,
            "create_music_gen_service",
            lambda provider, **kw: fake_svc,
        )
        agent = make_mock_agent(tmp_path)
        mgr = setup_compose(agent, provider="minimax", api_key="test-key")
        assert isinstance(mgr, ComposeManager)
        agent.add_tool.assert_called_once()
