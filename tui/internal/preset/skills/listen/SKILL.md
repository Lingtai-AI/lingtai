---
name: listen
description: >
  Analyze audio locally — transcribe speech with Whisper, or extract musical
  features (tempo, key, dynamics, spectral profile) with librosa. Both run
  on the user's machine with no API key. Read this when the human asks you
  to transcribe a voice note, extract lyrics from singing, critique generated
  music, or analyze audio characteristics. For *creating* music or audio,
  use the `media-creation` skill instead.
version: 1.0.0
---

# listen

> Local-only audio analysis. No API key, no network. Two actions: transcribe (speech → text) or appreciate (music → numerical critique).

## Two Actions

| Action | Backend | When |
|---|---|---|
| **transcribe** | `faster-whisper` (local Whisper) | Spoken word, voice notes, podcasts, lectures. Works on singing too but lyrics may be inaccurate. |
| **appreciate** | `librosa` (signal processing) | Music — tempo, key, frequency bands, dynamics. Returns numerical measurements, not subjective descriptions. |

Both actions are wrappers around the bundled scripts. Run them with `bash` like any other command-line tool:

```
bash python <skill-path>/scripts/transcribe.py <audio-file>
bash python <skill-path>/scripts/appreciate.py <audio-file>
```

The scripts auto-install their dependencies via `lingtai.venv_resolve.ensure_package` on first run, so the first invocation may take ~30 s.

## transcribe — speech to text

```
python scripts/transcribe.py <audio-path> [--model base] [--device cpu]
```

| Flag | Default | Notes |
|---|---|---|
| `--model` | `base` | Whisper model size: `tiny`, `base`, `small`, `medium`, `large-v2`, `large-v3`. Larger = more accurate, slower, more RAM. |
| `--device` | `cpu` | Use `cuda` if you have a GPU. |
| `--compute-type` | `int8` | CTranslate2 compute type. `int8` is the fastest CPU mode. Use `float16` on GPU. |

Output: a JSON document on stdout with:

```json
{
  "text": "<full transcript>",
  "language": "en",
  "language_probability": 0.99,
  "duration": 42.3,
  "segments": [
    {"start": 0.0, "end": 4.2, "text": "..."},
    ...
  ]
}
```

**Best for:** Clear spoken word in any of Whisper's supported languages.
**Caveats:** Singing lyrics often mistranscribed — Whisper is trained on speech, not singing. Background music degrades accuracy. For very noisy input, try `--model medium` or `large-v3`.

## appreciate — music analysis

```
python scripts/appreciate.py <audio-path>
```

No flags — purely analytical. Output: a JSON document with:

| Field | Meaning |
|---|---|
| `duration` | Audio length in seconds |
| `tempo_bpm` | Estimated tempo |
| `beat_regularity_std` | Std-dev of inter-beat intervals — small (<0.05) = steady, large = rubato/free |
| `key` | Estimated key (e.g. `D minor`, `G major`) |
| `key_confidence` | 0–1, correlation with Krumhansl key profile |
| `chroma_profile` | Per-pitch-class energy — useful for spotting modal mixture |
| `spectral_centroid_hz` | Brightness — higher = brighter mix |
| `spectral_bandwidth_hz` | Spread of spectrum |
| `spectral_rolloff_hz` | 85th-percentile frequency — "where the highs end" |
| `zero_crossing_rate` | Noisiness measure |
| `dynamic_range_db` | Loud-vs-quiet contrast in dB |
| `frequency_bands_pct` | Percentage of energy in sub_bass/bass/low_mid/mid/upper_mid/presence/brilliance |
| `energy_contour` | RMS energy in 10 equal-time segments (loud-vs-quiet shape over time) |
| `onset_density_per_sec` | How many note-onsets per second — proxy for "busyness" |

These are **measurements, not opinions**. Your job is to translate the numbers into a critique:
- "tempo_bpm: 84, beat_regularity_std: 0.012" → "steady mid-tempo, ballad pacing".
- "spectral_centroid_hz: 3500, presence: 22%" → "bright, vocal-forward mix".
- "energy_contour: monotonically increasing" → "builds throughout".

**Best for:** Music. **Useless for speech** — gives spectral data with no semantic content.

## When to use which

| Input | Action |
|---|---|
| Voice note, lecture, podcast | `transcribe` |
| Music with vocals — want lyrics | `transcribe` (warn human: lyrics may be wrong) |
| Music — want to know if it matches a brief | `appreciate` |
| Generated audio from `media-creation/compose` — QA | `appreciate` |
| TTS output from `media-creation/talk` — verify pronunciation | `transcribe` (round-trip QA) |
| Both (transcript + analysis) | Run both scripts |

## Going Deeper

The bundled scripts are deliberately minimal. If you need:
- **Per-section analysis** (verse vs chorus): segment the file with `librosa.segment` first, then run `appreciate.py` on each segment.
- **Multi-track separation**: use `demucs` or `spleeter` (heavier deps — install on demand via `pip`).
- **Pitch tracking** (melody extraction): use `librosa.pyin` or `crepe`.
- **Lyrics alignment**: the Whisper segments give you word-level timing if you pass `--word-timestamps`.

You can write your own scripts using the same dependencies — `librosa` and `faster-whisper` are already installed once the bundled scripts have run.

## When NOT to use this skill

- Human asked you to *create* audio (music, speech, sound effect) — use `media-creation`.
- Human asked you to *describe* a video or image — use `vision`.
- You only need to play audio for the human — use `media-creation/talk` or the OS-native player.
