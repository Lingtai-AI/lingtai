---
name: xiaomi-mimo
description: >
  Manual for Xiaomi MiMo (小米MiMo) — an OpenAI-/Anthropic-compatible LLM
  provider with a flagship 1M-context line (`mimo-v2.5-pro`, text-only)
  and an omni-modal variant (`mimo-v2.5` / `mimo-v2-omni`) that accepts
  image, audio, AND video input via plain chat-completion calls. This
  skill does NOT expose tools; it tells the agent how to (1) source the
  key by scanning presets for the right `manifest.llm.api_key_env` slot,
  (2) pick a regional cluster (CN / Singapore / Amsterdam for Token Plan,
  single global host for pay-as-you-go), and (3) craft direct
  chat-completions requests for audio or video input. Body covers the
  models matrix, key sourcing, cluster selection, model switching,
  failure modes, and a self-healing protocol for when MiMo's docs drift.
  Two `reference/` files cover the modalities the kernel doesn't wrap:
  audio-input.md (transcription, diarization, audio QA) and
  video-input.md (scene description, action recognition, joint A/V).
  Read this skill when the human asks to use MiMo as their LLM, work
  with audio/video input, or debug a MiMo connection. Do NOT use for
  image input (the agent's `vision` tool already wires through to MiMo
  via the kernel's `MiMoVisionService` when the active model supports
  it), web search (use `web-browsing`), or media generation (use
  `minimax-cli` — MiMo doesn't generate media). MiMo currently ships
  **no MCP servers**, so everything is HTTPS / chat-completions.
version: 1.1.0
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

**Never hardcode the key in any committed file.** The TUI stores keys in `~/.lingtai-tui/.env` and tells each preset which slot to read via `manifest.llm.api_key_env`. Slots are per-preset, so a user with both pay-as-you-go and a Token Plan account has two distinct env vars.

The MiMo platform issues two key formats — they are not interchangeable:

| Format | What it is | Base URL family |
|---|---|---|
| `sk-xxxxx` | Pay-as-you-go (per-token billing) | `api.xiaomimimo.com` |
| `tp-xxxxx` | Token Plan subscription (fixed monthly credits) | `token-plan-{cn,sgp,ams}.xiaomimimo.com` |

**Resolution: scan presets, find MiMo ones, read their declared slot.**

```bash
# Walk every preset; for each one whose provider is mimo, print
# (slot-name, base_url) so you can pick the right account/region.
python3 - <<'PY'
import json, os, glob
for path in glob.glob(os.path.expanduser("~/.lingtai-tui/presets/*.json")):
    try:
        with open(path) as f:
            doc = json.load(f)
    except Exception:
        continue
    llm = doc.get("manifest", {}).get("llm", {}) or {}
    if llm.get("provider") != "mimo":
        continue
    slot = llm.get("api_key_env") or "MIMO_API_KEY"  # built-ins may leave empty → legacy default
    base = llm.get("base_url") or ""
    print(f"{os.path.basename(path):30s}  slot={slot:30s}  base_url={base}")
PY
```

Slot naming (per `tui/internal/preset/preset.go::AutoEnvVarName`):
- New user-saved presets: `MIMO_<N>_API_KEY` (no region suffix — MiMo doesn't split CN/INTL the way MiniMax does; `N` is a counter).
- Built-in / legacy presets: `MIMO_API_KEY` for back-compat.

Once you've picked the right slot, export it for the bash recipes in `reference/`:

```bash
SLOT=MIMO_1_API_KEY    # whichever slot the preset scan returned
export MIMO_API_KEY=$(grep -E "^${SLOT}=" ~/.lingtai-tui/.env | cut -d= -f2- | tr -d ' ')
export MIMO_BASE_URL=$(...)  # see "Picking The Cluster" below for the right value
```

If multiple MiMo presets exist and you can't infer which the human means, **ask** — don't guess. Inspect the key prefix to confirm which base URL family it pairs with:

```bash
case "$MIMO_API_KEY" in
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
| **Audio** input (transcribe, describe a sound) | This skill — see [`reference/audio-input.md`](reference/audio-input.md) for the chat-completions request shape and a working bash recipe |
| **Video** input (describe a clip, summarize frames) | This skill — see [`reference/video-input.md`](reference/video-input.md) for schema, fps semantics, and the bash recipe by analogy from audio |
| TTS / ASR locally | `listen` skill if local; otherwise the MiMo TTS models via the standard chat-completions endpoint (no MCP wrapper) |
| Web search / web reading | `web-browsing` skill — MiMo has no MCP search tool |
| Image / video / music *generation* | `minimax-cli` skill — MiMo doesn't generate media |

## Multimodal Input (audio + video)

The kernel's vision capability handles **image** input automatically when the active model supports it (`mimo-v2.5`, `mimo-v2-omni` — see Models table). For **audio** and **video** input there's no in-process wrapper; the agent crafts a direct chat-completions call. Two reference files cover the request shape, working bash recipes, capabilities, and cost notes:

- **Audio** — transcription, diarization, audio QA, audio-visual joint reasoning. See [`reference/audio-input.md`](reference/audio-input.md).
- **Video** — scene description, action recognition, joint A/V perception (v2-omni). See [`reference/video-input.md`](reference/video-input.md).

Both reference files use `MIMO_API_KEY` and `MIMO_BASE_URL` env vars (sourced per the section above), so the bash recipes work as-is once those are exported.

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
