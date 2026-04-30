---
name: xiaomi-mimo
description: >
  Discovery protocol (not a reference) for Xiaomi MiMo (小米MiMo) — an
  OpenAI-/Anthropic-compatible LLM provider whose single API key unlocks
  a family of ~9 models behind one chat-completions endpoint. The family
  spans long-context text reasoning, multimodal-input chat (image / audio
  / video understanding via standard `messages.content[]`), and a
  text-to-speech line that returns base64 audio (with built-in voice
  catalogue, voice-design-from-prompt, and voice-cloning-from-sample
  variants). Marketing surface lives at https://mimo.xiaomi.com; the
  developer docs are at https://platform.xiaomimimo.com — and the agent
  should fetch the live docs rather than trust this manual for anything
  schema-shaped, because the API surface evolves. This skill teaches the
  agent (1) which two URLs to start from, (2) how to enumerate the
  current model lineup, (3) how to source the API key from the
  user's preset library, (4) how to pick the right base URL for their
  key (pay-as-you-go vs Token Plan; three regional clusters), and (5)
  what to do if the live docs contradict what the agent expects (file a
  report via the `lingtai-issue-report` skill). Read when the human
  asks to use MiMo as their LLM, work with multimodal input, generate
  speech audio, or debug a MiMo connection. Do NOT use for image / video
  / music *generation* — that's `minimax-cli`. MiMo ships **no MCP
  servers**; everything is HTTPS chat-completions.
version: 2.0.0
---

# xiaomi-mimo

> **This is a discovery protocol, not a reference.** It teaches you where to look for current MiMo capabilities — it does not mirror them. Anything API-shaped (model IDs, request schemas, voice catalogues, quotas) drifts faster than this manual; fetch the live docs every time.

## Where to look

Two URLs and an LLM-friendly bulk dump are all the agent needs to bootstrap full knowledge of the API:

| Purpose | URL |
|---|---|
| **Doc index** (start here for any specific question) | [`platform.xiaomimimo.com/llms.txt`](https://platform.xiaomimimo.com/llms.txt) |
| **Full doc dump** (one curl → every public doc page concatenated) | [`platform.xiaomimimo.com/llms-full.txt`](https://platform.xiaomimimo.com/llms-full.txt) |
| Marketing / model gallery (skim once for the vibe) | [`mimo.xiaomi.com`](https://mimo.xiaomi.com) |

```bash
# Quick orientation — what doc pages exist right now?
curl -s https://platform.xiaomimimo.com/llms.txt | head -60

# Full dump — when you need everything in context (much larger)
curl -s https://platform.xiaomimimo.com/llms-full.txt | wc -l
```

The doc index is structured so you can grep for what you need (e.g. `grep -i tts`, `grep -i pricing`, `grep -i multimodal`). Each entry has a verbatim URL — fetch the specific page once you've located it.

## Roughly what's behind a MiMo API key

Enough context for the agent to know what kind of question to ask the docs. **Verify model IDs and capability claims against the live docs every time** — Xiaomi rotates suffixes and adds new variants regularly.

Three rough families share one `https://<host>/v1/chat/completions` endpoint:

- **Text-only chat models** — long-context reasoning and tool use. Use for plain LLM work; one is the 1M-context flagship.
- **Multimodal-input chat models** — accept image, audio, AND video as content parts (`type: "image_url" | "input_audio" | "video_url"`) alongside text. Output is text. Use for transcription, image OCR, scene description, audio-visual joint reasoning, etc.
- **Text-to-speech models** — accept text, return base64-encoded audio. Three flavours: a built-in voice catalogue, a voice-design variant where you describe the voice in natural language, and a voice-clone variant where you upload a reference audio sample.

To get the *current* exact model IDs and per-model capability matrix:

```bash
# Inspect the OpenAI-compat spec — lists every model the endpoint accepts
curl -s https://platform.xiaomimimo.com/static/docs/api/chat/openai-api.md | head -200

# Or hit the doc index and follow the API path it advertises
curl -s https://platform.xiaomimimo.com/llms.txt | grep -i 'api/chat'
```

For the modality you actually need, fetch the dedicated guide. The doc tree currently follows `static/docs/usage-guide/<topic>.md` — `grep usage-guide` against `llms.txt` to find the current path, then curl it.

## Sourcing the API key

The TUI stores keys in `~/.lingtai-tui/.env` and tells each preset which slot to read via `manifest.llm.api_key_env`. Slots are per-preset, so a user with both pay-as-you-go and a Token Plan account has two distinct env vars.

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
- New user-saved presets: `MIMO_<N>_API_KEY` (no region suffix; `N` is a counter).
- Built-in / legacy presets: `MIMO_API_KEY` for back-compat.

Once you've picked the right slot, export it for any direct API call:

```bash
SLOT=MIMO_1_API_KEY    # whichever slot the preset scan returned
export MIMO_API_KEY=$(grep -E "^${SLOT}=" ~/.lingtai-tui/.env | cut -d= -f2- | tr -d ' ')
export MIMO_BASE_URL=$(...)  # see "Picking the host" below
```

If multiple MiMo presets exist and you can't infer which the human means, **ask** — don't guess.

## Picking the host

MiMo issues two key formats that pair with different host families. They are **not** interchangeable — using a `tp-` key against `api.xiaomimimo.com` or vice versa returns `401 invalid_key`.

| Key prefix | Host family | Notes |
|---|---|---|
| `sk-…` | `api.xiaomimimo.com` (single global host) | Pay-as-you-go, per-token billing |
| `tp-…` | `token-plan-{cn,sgp,ams}.xiaomimimo.com` | Token Plan, three regional clusters |

For Token Plan, the user's assigned cluster is shown on their Subscription page (`platform.xiaomimimo.com/#/console/plan-manage`) — the platform pins each plan to one cluster. Don't guess; ask the user, or read `manifest.llm.base_url` from the preset.

```bash
# Inspect the key prefix
case "$MIMO_API_KEY" in
  sk-*) export MIMO_BASE_URL="https://api.xiaomimimo.com" ;;
  tp-*) echo "Token Plan — set MIMO_BASE_URL to the regional cluster from the user's preset" ;;
  *)    echo "unknown prefix — verify with the user" ;;
esac
```

For the Anthropic-compat surface (Token Plan only), the path is `/anthropic` instead of `/v1`. Both cluster types speak both APIs.

## Switching models in the active preset

The default preset uses one of the multimodal-input models. To swap:

1. Run `/setup`, pick `mimo`, edit the manifest's `llm.model` field.
2. Or clone the preset (`cp ~/.lingtai-tui/presets/mimo.json …/mimo-pro.json`) and edit `model`.

**Important:** if you switch to a text-only model, also remove the `vision` capability from the manifest — otherwise the kernel's vision tool will fire against a text-only model and 400 on image input.

## When the docs contradict this skill, file a report

This skill ages. The live docs are authoritative; if you find a discrepancy — a model that no longer exists, a doc URL that 404s, a request shape that returns a clean error against the documented spec, a new capability MiMo shipped that this skill doesn't mention — **invoke the `lingtai-issue-report` skill** to assemble a structured report. Don't silently work around the drift; surfacing it is how this skill stays accurate for the next agent. Specific signals worth reporting:

- `llms.txt` no longer returns the doc index (or moved)
- A model ID listed in `static/docs/api/chat/openai-api.md` returns "model not found"
- The cluster URL family in this skill's "Picking the host" table no longer matches the platform's current console
- A capability advertised at `mimo.xiaomi.com` (e.g. a new model variant) is missing from the API surface — or vice versa
- Pricing or tier limits in the docs differ from what `usage` fields actually report

## Failure modes

| Symptom | Likely cause | Fix |
|---|---|---|
| `401 invalid_key` | Wrong key prefix for the host (sk- vs tp-) | Match per the "Picking the host" table; `lingtai-issue-report` if the doc no longer matches the real prefixes |
| `404` on chat completions | Token Plan account hitting `api.xiaomimimo.com` (or vice versa) | Swap `MIMO_BASE_URL` to the right family |
| Token Plan latency spikes | Wrong regional cluster | Check the user's Subscription page; switch `base_url` to the assigned cluster |
| `429 Rate limited` | Hit RPM/TPM limit | Live docs — current per-model RPM/TPM caps live in `static/docs/pricing.md` |
| Vision tool 400s after model swap | Switched to a text-only model but kept `vision` capability | Remove `vision` from manifest, or switch model back to a multimodal-input one |
| Anything else weird | The skill is stale, the docs moved, or MiMo changed behaviour | Fetch `llms.txt`, follow the trail; if the trail itself is broken, file via `lingtai-issue-report` |
