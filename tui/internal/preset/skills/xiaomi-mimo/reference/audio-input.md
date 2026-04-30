# Audio Input — Xiaomi MiMo

Audio understanding is **not** wired into the kernel's vision capability — it lives here. Agents craft a direct chat-completions call when they need to work with audio. **Both `mimo-v2.5` and `mimo-v2-omni` accept audio input**, so if the agent is already running on `mimo-v2.5` (the default preset), they hit their own LLM endpoint with audio content — no separate model, no extra round-trip.

> Live doc: [Audio Understanding](https://platform.xiaomimimo.com/static/docs/usage-guide/multimodal-understanding/audio-understanding.md) — `curl` it for the latest format support, sample-rate limits, and any new modalities. URL list shifts; verify before claiming a feature.

## Capabilities

Per the audio-understanding guide + v2-omni release notes:

| Task | How to phrase the prompt |
|---|---|
| **Verbatim transcription** | "Transcribe this audio. Quote each speaker's words exactly." |
| **Multilingual transcription** | "Transcribe in the original language of the audio." |
| **Translation** | "Transcribe and translate to English." |
| **Speaker diarization / multi-speaker separation** | "Identify each distinct speaker and label their turns as Speaker A, Speaker B, etc." |
| **Environmental sound classification** | "What environmental sounds do you hear? Identify each one." |
| **Audio-visual joint reasoning** | Pass an `input_audio` AND an `image_url` content part in the same message — useful for e.g. "Does the person in the image match the voice in the audio?" |
| **Long-form comprehension (≤10h)** | "Summarize this audio. Identify key topics and roughly when each is discussed." (Costs scale with `audio_tokens` — verify with a small clip first.) |
| **Question-answering over audio** | "Listen to this lecture and answer: [question]" |

These are MiMo's own claims from the v2-omni release notes; the live audio guide is authoritative if anything contradicts.

## Request schema

Per `docs/usage-guide/multimodal-understanding/audio-understanding.md`:

```json
{
  "model": "mimo-v2.5",
  "messages": [{
    "role": "user",
    "content": [
      {"type": "input_audio", "input_audio": {"data": "data:audio/mp3;base64,<BASE64>"}},
      {"type": "text", "text": "What does the speaker say? Quote verbatim."}
    ]
  }],
  "max_completion_tokens": 800
}
```

The `data` field accepts **either** a base64 data URL (shown above) **or** a plain HTTPS URL to an audio file. Supported formats per docs: MP3, WAV, M4A, AIFF. Live-tested working: MP3 base64.

## Working bash recipe — transcription

```bash
B64=$(base64 < recording.mp3 | tr -d '\n')
curl -s "${MIMO_BASE_URL:-https://api.xiaomimimo.com}/v1/chat/completions" \
  -H "Authorization: Bearer $MIMO_API_KEY" \
  -H "Content-Type: application/json" \
  -d "$(jq -n --arg b "$B64" '{
    model: "mimo-v2.5",
    messages: [{
      role: "user",
      content: [
        {type: "input_audio", input_audio: {data: ("data:audio/mp3;base64," + $b)}},
        {type: "text", text: "Transcribe this audio verbatim."}
      ]
    }],
    max_completion_tokens: 800
  }')" | jq -r '.choices[0].message.content'
```

Swap `mp3`/`audio/mp3` for `wav`/`audio/wav` etc. as needed. For HTTPS-hosted audio (no base64), pass `data: "https://example.com/clip.mp3"` directly.

`MIMO_API_KEY` resolution — see the SKILL.md key-sourcing section for the preset-scan pattern. `MIMO_BASE_URL` defaults to the pay-as-you-go host; for Token Plan, override to the regional cluster (`token-plan-cn.xiaomimimo.com` / `…sgp` / `…ams`).

## Cost

Audio is metered separately: `usage.prompt_tokens_details.audio_tokens` in the response. A 4-second MP3 ≈ 24 audio tokens. The text prompt and reasoning typically dominate total cost; expect ~500 total tokens for a short "transcribe this" call.

Add `/no_think` to the prompt to suppress the reasoning chain and cut output cost on simple transcription tasks (verify against live behavior — prompt-side toggle conventions evolve).
