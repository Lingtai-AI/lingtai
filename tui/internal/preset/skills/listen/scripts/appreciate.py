"""Local librosa music analysis — emits a single JSON document on stdout.

Usage:
    python appreciate.py <audio-path>

Auto-installs librosa on first run (via lingtai.venv_resolve.ensure_package
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


NOTES = ["C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"]
MAJOR_PROFILE = [6.35, 2.23, 3.48, 2.33, 4.38, 4.09, 2.52, 5.19, 2.39, 3.66, 2.29, 2.88]
MINOR_PROFILE = [6.33, 2.68, 3.52, 5.38, 2.60, 3.53, 2.54, 4.75, 3.98, 2.69, 3.34, 3.17]
BAND_RANGES = [
    ("sub_bass", 20, 60),
    ("bass", 60, 250),
    ("low_mid", 250, 500),
    ("mid", 500, 2000),
    ("upper_mid", 2000, 4000),
    ("presence", 4000, 6000),
    ("brilliance", 6000, 16000),
]


def analyze(audio_path: Path) -> dict:
    _ensure("librosa")
    _ensure("numpy")
    import librosa
    import numpy as np

    y, sr = librosa.load(str(audio_path))
    duration = float(librosa.get_duration(y=y, sr=sr))

    tempo, beats = librosa.beat.beat_track(y=y, sr=sr)
    tempo_val = float(np.atleast_1d(tempo)[0])
    beat_times = librosa.frames_to_time(beats, sr=sr).tolist()
    beat_regularity = float(np.std(np.diff(beat_times))) if len(beat_times) > 1 else None

    chroma = librosa.feature.chroma_cqt(y=y, sr=sr)
    chroma_avg = np.mean(chroma, axis=1)

    major_profile = np.array(MAJOR_PROFILE)
    minor_profile = np.array(MINOR_PROFILE)

    best_major = max(
        ((np.corrcoef(np.roll(chroma_avg, -s), major_profile)[0, 1], s) for s in range(12)),
        key=lambda x: x[0],
    )
    best_minor = max(
        ((np.corrcoef(np.roll(chroma_avg, -s), minor_profile)[0, 1], s) for s in range(12)),
        key=lambda x: x[0],
    )

    if best_major[0] > best_minor[0]:
        key = f"{NOTES[best_major[1]]} major"
        key_confidence = round(float(best_major[0]), 2)
    else:
        key = f"{NOTES[best_minor[1]]} minor"
        key_confidence = round(float(best_minor[0]), 2)

    spectral_centroid = float(np.mean(librosa.feature.spectral_centroid(y=y, sr=sr)))
    spectral_bandwidth = float(np.mean(librosa.feature.spectral_bandwidth(y=y, sr=sr)))
    spectral_rolloff = float(np.mean(librosa.feature.spectral_rolloff(y=y, sr=sr)))
    zcr = float(np.mean(librosa.feature.zero_crossing_rate(y)))

    rms = librosa.feature.rms(y=y)[0]
    rms_nonzero = rms[rms > 0]
    dynamic_range = (
        float(20 * np.log10(np.max(rms) / (np.min(rms_nonzero) + 1e-10)))
        if len(rms_nonzero) > 0
        else 0.0
    )

    n_segments = 10
    seg_len = len(y) // n_segments
    energy_contour = []
    for i in range(n_segments):
        seg = y[i * seg_len : (i + 1) * seg_len]
        seg_rms = float(np.sqrt(np.mean(seg ** 2)))
        energy_contour.append(
            {
                "start": round(i * seg_len / sr, 1),
                "end": round((i + 1) * seg_len / sr, 1),
                "rms": round(seg_rms, 6),
            }
        )

    N = len(y)
    fft = np.fft.rfft(y)
    freqs = np.fft.rfftfreq(N, 1 / sr)
    magnitude = np.abs(fft) / N
    total_energy = float(np.sum(magnitude ** 2))

    bands: dict[str, float] = {}
    for name, lo, hi in BAND_RANGES:
        mask = (freqs >= lo) & (freqs < hi)
        band_energy = float(np.sum(magnitude[mask] ** 2))
        bands[name] = round(100 * band_energy / total_energy, 1) if total_energy > 0 else 0.0

    onsets = librosa.onset.onset_detect(y=y, sr=sr)
    onset_density = round(len(onsets) / duration, 1) if duration > 0 else 0.0

    return {
        "duration": round(duration, 1),
        "tempo_bpm": round(tempo_val, 0),
        "beat_regularity_std": round(beat_regularity, 3) if beat_regularity is not None else None,
        "key": key,
        "key_confidence": key_confidence,
        "chroma_profile": {NOTES[i]: round(float(chroma_avg[i]), 3) for i in range(12)},
        "spectral_centroid_hz": round(spectral_centroid, 0),
        "spectral_bandwidth_hz": round(spectral_bandwidth, 0),
        "spectral_rolloff_hz": round(spectral_rolloff, 0),
        "zero_crossing_rate": round(zcr, 4),
        "dynamic_range_db": round(dynamic_range, 1),
        "frequency_bands_pct": bands,
        "energy_contour": energy_contour,
        "onset_density_per_sec": onset_density,
    }


def main() -> int:
    p = argparse.ArgumentParser(description="Local librosa music analysis.")
    p.add_argument("audio", type=Path, help="Path to audio file (mp3/wav/...)")
    args = p.parse_args()

    if not args.audio.is_file():
        print(json.dumps({"error": f"audio file not found: {args.audio}"}), file=sys.stderr)
        return 1

    result = analyze(args.audio)
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
