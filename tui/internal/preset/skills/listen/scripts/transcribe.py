"""Local Whisper transcription — emits a single JSON document on stdout.

Usage:
    python transcribe.py <audio-path> [--model base] [--device cpu] [--compute-type int8]

Auto-installs faster-whisper on first run (via lingtai.venv_resolve.ensure_package
when available, otherwise falls back to pip).
"""
from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path


def _ensure(pkg: str, import_name: str | None = None) -> None:
    try:
        __import__(import_name or pkg.replace("-", "_"))
        return
    except ImportError:
        pass
    try:
        from lingtai.venv_resolve import ensure_package  # type: ignore
        ensure_package(pkg, import_name)
        return
    except Exception:
        pass
    import subprocess
    subprocess.check_call([sys.executable, "-m", "pip", "install", pkg])


def main() -> int:
    p = argparse.ArgumentParser(description="Local Whisper transcription.")
    p.add_argument("audio", type=Path, help="Path to audio file (mp3/wav/m4a/...)")
    p.add_argument("--model", default="base", help="Whisper model size (default: base)")
    p.add_argument("--device", default="cpu", help="cpu or cuda (default: cpu)")
    p.add_argument("--compute-type", default="int8", help="CTranslate2 compute type (default: int8)")
    p.add_argument("--word-timestamps", action="store_true", help="Include per-word timing")
    args = p.parse_args()

    if not args.audio.is_file():
        print(json.dumps({"error": f"audio file not found: {args.audio}"}), file=sys.stderr)
        return 1

    _ensure("faster-whisper", "faster_whisper")
    from faster_whisper import WhisperModel

    model = WhisperModel(args.model, device=args.device, compute_type=args.compute_type)

    segments_iter, info = model.transcribe(
        str(args.audio),
        word_timestamps=args.word_timestamps,
    )
    segments_list = list(segments_iter)

    transcript_segments = []
    for seg in segments_list:
        entry = {
            "start": round(seg.start, 2),
            "end": round(seg.end, 2),
            "text": seg.text.strip(),
        }
        if args.word_timestamps and getattr(seg, "words", None):
            entry["words"] = [
                {"start": round(w.start, 2), "end": round(w.end, 2), "word": w.word}
                for w in seg.words
            ]
        transcript_segments.append(entry)

    full_text = " ".join(s["text"] for s in transcript_segments).strip()

    out = {
        "text": full_text,
        "language": info.language,
        "language_probability": round(info.language_probability, 3),
        "duration": round(info.duration, 2),
        "segments": transcript_segments,
    }
    print(json.dumps(out, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
