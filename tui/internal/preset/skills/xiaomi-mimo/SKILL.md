---
name: xiaomi-mimo
description: >
  Use Xiaomi MiMo (小米MiMo) — an OpenAI-/Anthropic-compatible LLM provider
  with a flagship 1M-context model line and an omni-modal variant
  (`mimo-v2-omni`) that accepts image, audio, AND video input via plain
  chat-completion calls. This skill is a thin pointer: it tells you how
  to source the key, pick a regional cluster, choose between pay-as-you-go
  and Token Plan, and how to craft multimodal requests for audio/video
  (which the kernel's vision capability does not expose — those go through
  bash/curl). Image input IS exposed through the `vision` capability via
  the kernel's first-class `MiMoVisionService`. Unlike the MiniMax / Zhipu
  coding plans, MiMo currently ships **no MCP servers**.
version: 1.0.0
---

# xiaomi-mimo

> Thin pointer. Live docs are the source of truth — `curl` them when you need depth.

## Live Docs (canonical)

When you need details — current model names, exact parameters, per-tier
quotas, regional cluster URLs — fetch these. URLs verified live 2026-04-29.

| Topic | URL |
|---|---|
| Doc index (start here if anything below 404s) | [`platform.xiaomimimo.com/llms.txt`](https://platform.xiaomimimo.com/llms.txt) |
| First API call (base URLs, auth, examples) | [`platform.xiaomimimo.com/static/docs/quick-start/first-api-call.md`](https://platform.xiaomimimo.com/static/docs/quick-start/first-api-call.md) |
| Pricing (pay-as-you-go) | [`platform.xiaomimimo.com/static/docs/pricing.md`](https://platform.xiaomimimo.com/static/docs/pricing.md) |
| OpenAI-compat API spec | [`platform.xiaomimimo.com/static/docs/api/chat/openai-api.md`](https://platform.xiaomimimo.com/static/docs/api/chat/openai-api.md) |
| Anthropic-compat API spec | [`platform.xiaomimimo.com/static/docs/api/chat/anthropic-api.md`](https://platform.xiaomimimo.com/static/docs/api/chat/anthropic-api.md) |
| **Image understanding guide** | [`platform.xiaomimimo.com/static/docs/usage-guide/multimodal-understanding/image-understanding.md`](https://platform.xiaomimimo.com/static/docs/usage-guide/multimodal-understanding/image-understanding.md) |
| **Audio understanding guide** | [`platform.xiaomimimo.com/static/docs/usage-guide/multimodal-understanding/audio-understanding.md`](https://platform.xiaomimimo.com/static/docs/usage-guide/multimodal-understanding/audio-understanding.md) |
| **Video understanding guide** | [`platform.xiaomimimo.com/static/docs/usage-guide/multimodal-understanding/video-understanding.md`](https://platform.xiaomimimo.com/static/docs/usage-guide/multimodal-understanding/video-understanding.md) |
| MiMo-V2-Omni release notes (capability claims) | [`platform.xiaomimimo.com/static/docs/news/previous-news/v2-omni-release.md`](https://platform.xiaomimimo.com/static/docs/news/previous-news/v2-omni-release.md) |
| Token Plan subscription tiers | [`platform.xiaomimimo.com/static/docs/tokenplan/subscription.md`](https://platform.xiaomimimo.com/static/docs/tokenplan/subscription.md) |
| Token Plan quick-access (cluster URLs) | [`platform.xiaomimimo.com/static/docs/tokenplan/quick-access.md`](https://platform.xiaomimimo.com/static/docs/tokenplan/quick-access.md) |
| Console (API keys) | [`platform.xiaomimimo.com/#/console/api-keys`](https://platform.xiaomimimo.com/#/console/api-keys) |

If a path 404s, fetch `llms.txt` to find the new path — paths shift.

Always `curl` when you need fresh info — the skill snapshot will go stale.

## Models (2026-04-29 snapshot — verify against live pricing page)

"Multimodal input" below covers image + audio + video — the docs treat
these as one capability ("currently, only mimo-v2.5 and mimo-v2-omni
support image, audio, or video input"), and live testing confirms both
models accept all three content types via standard chat completions.

| Model | Context | Multimodal input | Tool calls | Notes |
|---|---|---|---|---|
| `mimo-v2.5-pro` | 1M | ❌ | ✅ | Flagship (text-only). 2× credit consumption on Token Plan. |
| `mimo-v2.5` | 1M | ✅ image + audio + video | ✅ | **Default for the `mimo` preset** — sweet spot. |
| `mimo-v2-omni` | 256K | ✅ image + audio + video (native A/V joint) | ✅ | Specifically tuned for agentic GUI/browser-use; native synchronized audio+video perception. |
| `mimo-v2-flash` | 256K | ❌ | ✅ | Cheapest. Recommended `temperature` 0.3. |
| `mimo-v2.5-tts*` | — | — | ❌ | TTS (text→audio output, separate request shape) — limited-time free. |

Tool-call **thinking mode** returns `reasoning_content` alongside `tool_calls`.
The docs *recommend* keeping it in subsequent messages for best multi-turn
performance — the kernel currently doesn't round-trip this (so quality may
degrade on long tool loops). This is a soft recommendation, not a hard
contract like DeepSeek's.

## Sourcing The API Key

**Never hardcode the key into `mcp/servers.json` or any committed file.**
The MiMo platform issues two key formats — they are not interchangeable:

| Format | What it is | Base URL family |
|---|---|---|
| `sk-xxxxx` | Pay-as-you-go (per-token billing) | `api.xiaomimimo.com` |
| `tp-xxxxx` | Token Plan subscription (fixed monthly credits) | `token-plan-{cn,sgp,ams}.xiaomimimo.com` |

Resolution order:

1. **`~/.lingtai-tui/.env`** — `MIMO_API_KEY=…`. The TUI populates this on firstrun (firstrun stores whatever the user pasted; both `sk-` and `tp-` keys go in the same env var).
2. **Process environment** — if already exported, the kernel inherits it.
3. **Ask the user** — if neither path resolves.

```bash
grep -E '^MIMO_API_KEY=' ~/.lingtai-tui/.env | cut -d= -f2- | tr -d ' '
```

Inspect the prefix to know which base URL family to pair it with:

```bash
key=$(grep -E '^MIMO_API_KEY=' ~/.lingtai-tui/.env | cut -d= -f2- | tr -d ' ')
case "$key" in
  sk-*) echo "pay-as-you-go — use api.xiaomimimo.com" ;;
  tp-*) echo "Token Plan — use token-plan-{cn,sgp,ams}.xiaomimimo.com (pick by latency)" ;;
  *)    echo "unknown prefix — verify with the user" ;;
esac
```

## Picking The Cluster (Token Plan only)

Pay-as-you-go has a single global host (`api.xiaomimimo.com`). Token Plan has **three regional clusters**, all OpenAI-compatible at `…/v1` and Anthropic-compatible at `…/anthropic`:

| Cluster | OpenAI base URL | Anthropic base URL |
|---|---|---|
| China (default) | `https://token-plan-cn.xiaomimimo.com/v1` | `https://token-plan-cn.xiaomimimo.com/anthropic` |
| Singapore | `https://token-plan-sgp.xiaomimimo.com/v1` | `https://token-plan-sgp.xiaomimimo.com/anthropic` |
| Europe (Amsterdam) | `https://token-plan-ams.xiaomimimo.com/v1` | `https://token-plan-ams.xiaomimimo.com/anthropic` |

The cluster the user must use is **shown on their Subscription page** (`platform.xiaomimimo.com/#/console/plan-manage`) — the platform pins their plan to one cluster. Don't guess; ask the user, or have them edit the preset's `manifest.llm.base_url` to match.

The default preset ships with `https://api.xiaomimimo.com/v1` (pay-as-you-go). If the user's key is `tp-…`, swap the base URL via `/setup` or by editing the preset.

## When To Use This Skill

| Want to … | Use |
|---|---|
| Run an agent on a Xiaomi MiMo model | This skill (use the `mimo` preset) |
| Use long context (1M) on a Chinese-friendly LLM | `mimo-v2.5` or `mimo-v2.5-pro` (this skill) |
| **Image** input | The agent's `vision` tool — wired to MiMo via the kernel's `MiMoVisionService` (model `mimo-v2.5`). No bash needed. |
| **Audio** input (transcribe, describe a sound) | This skill — craft a curl POST against `mimo-v2-omni` (see "Audio Input" below) |
| **Video** input (describe a clip, summarize frames) | This skill — craft a curl POST against `mimo-v2-omni` (see "Video Input" below) |
| TTS / ASR locally | `listen` skill if local; otherwise the MiMo TTS models via the standard chat-completions endpoint (no MCP wrapper) |
| Web search / web reading | `web-browsing` skill — MiMo has no MCP search tool |
| Image / video / music *generation* | `minimax-token-plan` skill — MiMo doesn't generate media |

## Audio Input

> Live doc: [Audio Understanding](https://platform.xiaomimimo.com/static/docs/usage-guide/multimodal-understanding/audio-understanding.md)
> — `curl` it for the latest format support, sample-rate limits, and any
> new modalities (URL list shifts; verify before claiming a feature).

Audio understanding is **not** wired into the kernel's vision capability —
it lives here. Agents craft a direct chat-completions call when they need
to work with audio. **Both `mimo-v2.5` and `mimo-v2-omni` accept audio
input**, so if the agent is already running on `mimo-v2.5` (the default
preset), they hit their own LLM endpoint with audio content — no separate
model, no extra round-trip.

### Capabilities (per the audio-understanding guide + v2-omni release notes)

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

These are MiMo's own claims from the v2-omni release notes; the live audio
guide is authoritative if anything contradicts.

### Request schema

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

The `data` field accepts **either** a base64 data URL (shown above) **or**
a plain HTTPS URL to an audio file. Supported formats per docs: MP3, WAV,
M4A, AIFF. Live-tested working: MP3 base64.

### Working bash recipe — transcription

```bash
B64=$(base64 < recording.mp3 | tr -d '\n')
curl -s https://api.xiaomimimo.com/v1/chat/completions \
  -H "Authorization: Bearer $XIAOMI_API_KEY" \
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

Swap `mp3`/`audio/mp3` for `wav`/`audio/wav` etc. as needed. For HTTPS-hosted
audio (no base64), pass `data: "https://example.com/clip.mp3"` directly.

### Cost

Audio is metered separately: `usage.prompt_tokens_details.audio_tokens`
in the response. A 4-second MP3 ≈ 24 audio tokens. The text prompt and
reasoning typically dominate total cost; expect ~500 total tokens for a
short "transcribe this" call.

Add `/no_think` to the prompt to suppress the reasoning chain and cut
output cost on simple transcription tasks (verify against live behavior —
prompt-side toggle conventions evolve).

## Video Input

> Live doc: [Video Understanding](https://platform.xiaomimimo.com/static/docs/usage-guide/multimodal-understanding/video-understanding.md)
> — `curl` it for the latest format/size limits and `fps` semantics.

Same shape as audio — different content type and an optional `fps` field.
**Both `mimo-v2.5` and `mimo-v2-omni` accept video input.** Note that
v2-omni supports *native audio-video joint input* (synchronized perception
of soundtrack + frames per the v2-omni release notes); v2.5 handles
silent video frames the same way either model handles a sequence of images.

Use cases (from the v2-omni release notes — verify against live docs):
- Scene description / per-frame analysis
- Action recognition and event detection
- Situational awareness ("what is about to happen next?")
- Joint audio-video Q&A (e.g. "Does the speaker's gesture match what they
  say?") — only useful with v2-omni for true sync

**Schema:**

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

The `url` field accepts base64 data URLs or plain HTTPS URLs. Supported
formats: MP4, MOV, M4V. The `fps` parameter is optional — it tells MiMo
how many frames per second to actually examine. For a 3-second clip with
3 distinct scenes, `fps: 1` is enough; bump it up for fast-motion content.

Practical limits (verify against live docs — they shift):
- File size: keep under ~8 MB inline as base64; use HTTPS URLs for larger
- Duration: short clips (≤60s) work reliably. Long-form videos may hit
  context limits via `video_tokens` (a 3s clip ≈ 240 video tokens at fps:1)

Same bash recipe as audio, swapping `input_audio` → `video_url` and
`audio/mp3` → `video/mp4` in the data URL.

## Switching Models

The default preset uses `mimo-v2.5` (vision-capable, 1M context). To swap:

1. Run `/setup` and pick `mimo`, then edit the manifest's `llm.model` field
2. Or clone the preset (e.g. `cp ~/.lingtai-tui/presets/mimo.json ~/.lingtai-tui/presets/mimo-pro.json`) and change `model`

**Important:** if you switch to a text-only model (`mimo-v2.5-pro`, `mimo-v2-flash`), also remove the `vision` capability from the manifest — otherwise the vision tool will fire against a text-only model and 400 on image input.

## Failure Modes

| Symptom | Likely cause | Fix |
|---|---|---|
| `401 invalid_key` | Wrong key format for the URL (sk-… against token-plan host, or tp-… against api.xiaomimimo.com) | Match key prefix to host family per the table above |
| `404` on chat completions | Token Plan account but using `api.xiaomimimo.com` (or vice versa) | Swap `manifest.llm.base_url` to the right family |
| Token Plan latency spikes | Wrong regional cluster | Have the user check their Subscription page; switch `base_url` to the assigned cluster |
| Vision tool 400s | Switched to a text-only model but kept `vision` capability | Remove `vision` from manifest, or switch model back to `mimo-v2.5` / `mimo-v2-omni` |
| `429 Rate limited` | Hit RPM/TPM limit on free tier | Live docs — check current per-model RPM/TPM caps |
| Token Plan quota exhausted | Monthly credits used up | Subscription page — top up or wait for renewal |

## Self-Healing

If the live docs contradict this skill, **trust the docs** and (if the
human consents) update this file. Start the verification trail from the
canonical doc index:

```bash
curl -s https://platform.xiaomimimo.com/llms.txt | head -120
```

Highest-churn items (verify these first when something feels off):

1. **Model names** — `mimo-v2.5` / `mimo-v2-omni` etc. Xiaomi rotates
   suffixes (`-pro`, `-flash`, `-omni`, `-tts-*`) and adds new ones every
   few months. Authoritative list lives at
   `platform.xiaomimimo.com/static/docs/news/` (latest release post)
   and the OpenAI-compat spec page.
2. **Multimodal capability matrix** — which models accept image / audio /
   video has shifted at least once (v2-omni added native A/V joint mid-2026).
   Verify against the three multimodal-understanding guides:
   - [`image-understanding.md`](https://platform.xiaomimimo.com/static/docs/usage-guide/multimodal-understanding/image-understanding.md)
   - [`audio-understanding.md`](https://platform.xiaomimimo.com/static/docs/usage-guide/multimodal-understanding/audio-understanding.md)
   - [`video-understanding.md`](https://platform.xiaomimimo.com/static/docs/usage-guide/multimodal-understanding/video-understanding.md)
3. **Token Plan cluster URLs** (`token-plan-{cn,sgp,ams}.xiaomimimo.com`) —
   may add regions or change paths.
4. **Pricing** — `pricing.md` and `tokenplan/subscription.md` move
   independently; don't trust pricing snapshots in this file beyond a few
   months old.

If a doc page 404s, hit `llms.txt` first to find the new path before
declaring a feature gone.
