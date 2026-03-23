"""Tests for the listen capability and TranscriptionService."""
from pathlib import Path
from unittest.mock import MagicMock, patch

import pytest

from lingtai.capabilities.listen import ListenManager, setup as setup_listen
from lingtai.services.transcription import (
    TranscriptionResult,
    TranscriptionService,
    create_transcription_service,
)


def make_mock_agent(tmp_path):
    agent = MagicMock()
    agent.working_dir = tmp_path
    return agent


# ── TranscriptionResult dataclass ────────────────────────────────────────

class TestTranscriptionResult:
    def test_minimal(self):
        r = TranscriptionResult(text="hello world")
        assert r.text == "hello world"
        assert r.language is None
        assert r.language_probability is None
        assert r.duration is None
        assert r.segments is None

    def test_full(self):
        r = TranscriptionResult(
            text="hello",
            language="en",
            language_probability=0.99,
            duration=2.5,
            segments=[{"start": 0.0, "end": 2.5, "text": "hello"}],
        )
        assert r.language == "en"
        assert r.segments == [{"start": 0.0, "end": 2.5, "text": "hello"}]


# ── TranscriptionService factory ─────────────────────────────────────────

class TestTranscriptionFactory:
    def test_unknown_provider(self):
        with pytest.raises(ValueError, match="Unknown transcription provider"):
            create_transcription_service("nonexistent")

    def test_whisper_provider(self):
        svc = create_transcription_service("whisper", model_size="tiny")
        from lingtai.services.transcription.whisper import WhisperTranscriptionService
        assert isinstance(svc, WhisperTranscriptionService)
        assert svc._model_size == "tiny"

    def test_gemini_provider(self):
        svc = create_transcription_service("gemini", api_key="test-key")
        from lingtai.services.transcription.gemini import GeminiTranscriptionService
        assert isinstance(svc, GeminiTranscriptionService)
        assert svc._api_key == "test-key"


# ── WhisperTranscriptionService ──────────────────────────────────────────

class TestWhisperTranscriptionService:
    def test_transcribe_success(self, tmp_path):
        from lingtai.services.transcription.whisper import WhisperTranscriptionService

        audio_file = tmp_path / "speech.mp3"
        audio_file.write_bytes(b"FAKE_AUDIO")

        mock_segment = MagicMock()
        mock_segment.start = 0.0
        mock_segment.end = 2.5
        mock_segment.text = " Hello world"

        mock_info = MagicMock()
        mock_info.language = "en"
        mock_info.language_probability = 0.99
        mock_info.duration = 2.5

        mock_model = MagicMock()
        mock_model.transcribe.return_value = ([mock_segment], mock_info)

        svc = WhisperTranscriptionService()
        svc._model = mock_model

        result = svc.transcribe(audio_file)
        assert isinstance(result, TranscriptionResult)
        assert result.text == "Hello world"
        assert result.language == "en"
        assert result.language_probability == 0.99
        assert result.duration == 2.5
        assert len(result.segments) == 1
        assert result.segments[0]["text"] == "Hello world"

    def test_model_lazy_loaded(self):
        from lingtai.services.transcription.whisper import WhisperTranscriptionService

        svc = WhisperTranscriptionService(model_size="tiny", device="cpu")
        assert svc._model is None
        assert svc._model_size == "tiny"
        assert svc._device == "cpu"


# ── GeminiTranscriptionService ───────────────────────────────────────────

class TestGeminiTranscriptionService:
    def test_requires_api_key(self, tmp_path, monkeypatch):
        from lingtai.services.transcription.gemini import GeminiTranscriptionService

        monkeypatch.delenv("GEMINI_API_KEY", raising=False)
        svc = GeminiTranscriptionService()
        audio_file = tmp_path / "test.wav"
        audio_file.write_bytes(b"FAKE")

        with pytest.raises(ValueError, match="API key"):
            svc.transcribe(audio_file)

    def test_mime_type_guessing(self):
        from lingtai.services.transcription.gemini import _guess_mime_type

        assert _guess_mime_type(Path("test.mp3")) == "audio/mpeg"
        assert _guess_mime_type(Path("test.wav")) == "audio/wav"
        assert _guess_mime_type(Path("test.flac")) == "audio/flac"
        assert _guess_mime_type(Path("test.ogg")) == "audio/ogg"
        assert _guess_mime_type(Path("test.unknown")) == "audio/wav"


# ── ListenManager (integration with service) ─────────────────────────────

class TestListenManagerTranscribe:
    def _make_mock_service(self):
        svc = MagicMock(spec=TranscriptionService)
        svc.transcribe.return_value = TranscriptionResult(
            text="Hello world",
            language="en",
            language_probability=0.99,
            duration=2.5,
            segments=[{"start": 0.0, "end": 2.5, "text": "Hello world"}],
        )
        return svc

    def test_transcribe_success(self, tmp_path):
        audio_file = tmp_path / "speech.mp3"
        audio_file.write_bytes(b"FAKE_AUDIO")

        svc = self._make_mock_service()
        mgr = ListenManager(working_dir=tmp_path, transcription_service=svc)

        result = mgr.handle({"audio_path": str(audio_file), "action": "transcribe"})
        assert result["status"] == "ok"
        assert result["action"] == "transcribe"
        assert result["text"] == "Hello world"
        assert result["language"] == "en"
        assert len(result["segments"]) == 1
        svc.transcribe.assert_called_once_with(audio_file)

    def test_transcribe_relative_path(self, tmp_path):
        audio_file = tmp_path / "audio" / "test.mp3"
        audio_file.parent.mkdir()
        audio_file.write_bytes(b"FAKE")

        svc = self._make_mock_service()
        mgr = ListenManager(working_dir=tmp_path, transcription_service=svc)

        result = mgr.handle({"audio_path": "audio/test.mp3", "action": "transcribe"})
        assert result["status"] == "ok"

    def test_transcribe_file_not_found(self, tmp_path):
        svc = self._make_mock_service()
        mgr = ListenManager(working_dir=tmp_path, transcription_service=svc)
        result = mgr.handle({"audio_path": "/nonexistent/file.mp3", "action": "transcribe"})
        assert result["status"] == "error"
        assert "not found" in result["message"]

    def test_transcribe_service_failure(self, tmp_path):
        audio_file = tmp_path / "test.mp3"
        audio_file.write_bytes(b"FAKE")

        svc = MagicMock(spec=TranscriptionService)
        svc.transcribe.side_effect = RuntimeError("model crashed")

        mgr = ListenManager(working_dir=tmp_path, transcription_service=svc)
        result = mgr.handle({"audio_path": str(audio_file), "action": "transcribe"})
        assert result["status"] == "error"
        assert "Transcription failed" in result["message"]

    def test_lazy_default_whisper_service(self, tmp_path):
        """When no service is injected, ListenManager lazy-creates WhisperTranscriptionService."""
        mgr = ListenManager(working_dir=tmp_path)
        assert mgr._transcription_service is None

        # Patch the factory so we don't actually load faster-whisper
        mock_svc = self._make_mock_service()
        with patch(
            "lingtai.capabilities.listen.create_transcription_service",
            return_value=mock_svc,
        ) as factory:
            audio_file = tmp_path / "test.mp3"
            audio_file.write_bytes(b"FAKE")
            result = mgr.handle({"audio_path": str(audio_file), "action": "transcribe"})
            factory.assert_called_once_with("whisper")
            assert result["status"] == "ok"


class TestListenManagerAppreciate:
    def test_appreciate_success(self, tmp_path):
        audio_file = tmp_path / "music.mp3"
        audio_file.write_bytes(b"FAKE")

        import numpy as np
        mock_librosa = MagicMock()
        mock_librosa.load.return_value = (np.random.randn(22050 * 5).astype(np.float32), 22050)
        mock_librosa.get_duration.return_value = 5.0
        mock_librosa.beat.beat_track.return_value = (np.array([120.0]), np.array([10, 20, 30]))
        mock_librosa.frames_to_time.return_value = np.array([0.5, 1.0, 1.5])
        mock_librosa.feature.chroma_cqt.return_value = np.random.rand(12, 100)
        mock_librosa.feature.spectral_centroid.return_value = np.array([[2000.0]])
        mock_librosa.feature.spectral_bandwidth.return_value = np.array([[1500.0]])
        mock_librosa.feature.spectral_rolloff.return_value = np.array([[4000.0]])
        mock_librosa.feature.zero_crossing_rate.return_value = np.array([[0.05]])
        mock_librosa.feature.rms.return_value = np.array([[0.01, 0.05, 0.1]])
        mock_librosa.onset.onset_detect.return_value = np.array([1, 5, 10, 15, 20])

        mgr = ListenManager(working_dir=tmp_path)
        mgr._librosa = mock_librosa

        result = mgr.handle({"audio_path": str(audio_file), "action": "appreciate"})
        assert result["status"] == "ok"
        assert result["action"] == "appreciate"
        assert "tempo_bpm" in result
        assert "key" in result
        assert "frequency_bands_pct" in result
        assert "energy_contour" in result
        assert "spectral_centroid_hz" in result

    def test_appreciate_file_not_found(self, tmp_path):
        mgr = ListenManager(working_dir=tmp_path)
        result = mgr.handle({"audio_path": "/nonexistent/file.mp3", "action": "appreciate"})
        assert result["status"] == "error"
        assert "not found" in result["message"]

    def test_appreciate_librosa_load_failure(self, tmp_path):
        audio_file = tmp_path / "bad.mp3"
        audio_file.write_bytes(b"FAKE")

        mgr = ListenManager(working_dir=tmp_path)
        with patch.object(mgr, "_get_librosa", side_effect=ImportError("no librosa")):
            result = mgr.handle({"audio_path": str(audio_file), "action": "appreciate"})
        assert result["status"] == "error"
        assert "librosa" in result["message"]


class TestListenManagerValidation:
    def test_missing_audio_path(self, tmp_path):
        mgr = ListenManager(working_dir=tmp_path)
        result = mgr.handle({"action": "transcribe"})
        assert result["status"] == "error"
        assert "audio_path" in result["message"]

    def test_invalid_action(self, tmp_path):
        audio_file = tmp_path / "test.mp3"
        audio_file.write_bytes(b"FAKE")
        mgr = ListenManager(working_dir=tmp_path)
        result = mgr.handle({"audio_path": str(audio_file), "action": "invalid"})
        assert result["status"] == "error"
        assert "action" in result["message"]


class TestSetupListen:
    def test_setup_registers_tool(self, tmp_path):
        agent = make_mock_agent(tmp_path)
        mgr = setup_listen(agent)
        assert isinstance(mgr, ListenManager)
        agent.add_tool.assert_called_once()

    def test_setup_no_mcp_needed(self, tmp_path):
        """Listen runs locally — no mcp_client required."""
        agent = make_mock_agent(tmp_path)
        mgr = setup_listen(agent)
        assert isinstance(mgr, ListenManager)

    def test_setup_default_whisper(self, tmp_path):
        """Default provider is whisper."""
        agent = make_mock_agent(tmp_path)
        mgr = setup_listen(agent)
        from lingtai.services.transcription.whisper import WhisperTranscriptionService
        assert isinstance(mgr._transcription_service, WhisperTranscriptionService)

    def test_setup_custom_service(self, tmp_path):
        """Pre-built service is used directly."""
        agent = make_mock_agent(tmp_path)
        custom_svc = MagicMock(spec=TranscriptionService)
        mgr = setup_listen(agent, transcription_service=custom_svc)
        assert mgr._transcription_service is custom_svc

    def test_setup_gemini_provider(self, tmp_path):
        """Provider='gemini' creates GeminiTranscriptionService."""
        agent = make_mock_agent(tmp_path)
        mgr = setup_listen(agent, provider="gemini", api_key="test-key")
        from lingtai.services.transcription.gemini import GeminiTranscriptionService
        assert isinstance(mgr._transcription_service, GeminiTranscriptionService)

    def test_setup_whisper_kwargs(self, tmp_path):
        """Whisper kwargs are forwarded."""
        agent = make_mock_agent(tmp_path)
        mgr = setup_listen(agent, provider="whisper", model_size="large-v3", device="cuda")
        from lingtai.services.transcription.whisper import WhisperTranscriptionService
        svc = mgr._transcription_service
        assert isinstance(svc, WhisperTranscriptionService)
        assert svc._model_size == "large-v3"
        assert svc._device == "cuda"
