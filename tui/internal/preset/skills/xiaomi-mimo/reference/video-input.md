# Video Input — Xiaomi MiMo

Same shape as audio — different content type and an optional `fps` field. **Both `mimo-v2.5` and `mimo-v2-omni` accept video input.** Note that v2-omni supports *native audio-video joint input* (synchronized perception of soundtrack + frames per the v2-omni release notes); v2.5 handles silent video frames the same way either model handles a sequence of images.

> Live doc: [Video Understanding](https://platform.xiaomimimo.com/static/docs/usage-guide/multimodal-understanding/video-understanding.md) — `curl` it for the latest format/size limits and `fps` semantics.

## Use cases

From the v2-omni release notes — verify against live docs:

- Scene description / per-frame analysis
- Action recognition and event detection
- Situational awareness ("what is about to happen next?")
- Joint audio-video Q&A (e.g. "Does the speaker's gesture match what they say?") — only useful with v2-omni for true sync

## Request schema

```json
{
  "model": "mimo-v2.5",
  "messages": [{
    "role": "user",
    "content": [
      {"type": "video_url", "video_url": {"url": "data:video/mp4;base64,<BASE64>"}, "fps": 1},
      {"type": "text", "text": "Describe what happens in this video."}
    ]
  }],
  "max_completion_tokens": 800
}
```

The `url` field accepts base64 data URLs or plain HTTPS URLs. Supported formats: MP4, MOV, M4V. The `fps` parameter is optional — it tells MiMo how many frames per second to actually examine. For a 3-second clip with 3 distinct scenes, `fps: 1` is enough; bump it up for fast-motion content.

## Practical limits

Verify against live docs — they shift:

- File size: keep under ~8 MB inline as base64; use HTTPS URLs for larger
- Duration: short clips (≤60s) work reliably. Long-form videos may hit context limits via `video_tokens` (a 3s clip ≈ 240 video tokens at fps:1)

## Working bash recipe

Same shape as the audio recipe in [audio-input.md](audio-input.md), swapping `input_audio` → `video_url` and `audio/mp3` → `video/mp4` in the data URL:

```bash
B64=$(base64 < clip.mp4 | tr -d '\n')
curl -s "${MIMO_BASE_URL:-https://api.xiaomimimo.com}/v1/chat/completions" \
  -H "Authorization: Bearer $MIMO_API_KEY" \
  -H "Content-Type: application/json" \
  -d "$(jq -n --arg b "$B64" '{
    model: "mimo-v2.5",
    messages: [{
      role: "user",
      content: [
        {type: "video_url", video_url: {url: ("data:video/mp4;base64," + $b)}, fps: 1},
        {type: "text", text: "Describe what happens in this video."}
      ]
    }],
    max_completion_tokens: 800
  }')" | jq -r '.choices[0].message.content'
```

For HTTPS-hosted video (no base64), pass `url: "https://example.com/clip.mp4"` directly.
