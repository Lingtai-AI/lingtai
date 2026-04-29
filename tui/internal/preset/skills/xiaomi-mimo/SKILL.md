---
name: xiaomi-mimo
description: >
  Use Xiaomi MiMo (小米MiMo) — an OpenAI-/Anthropic-compatible LLM provider
  with a flagship 1M-context model line. This skill is a thin pointer: it
  tells you how to source the key, pick a regional cluster, choose between
  pay-as-you-go and Token Plan, and where the live docs are. Unlike the
  MiniMax / Zhipu coding plans, MiMo currently ships **no MCP servers** —
  it is purely a chat-completion (and TTS/ASR) backend.
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
| Token Plan subscription tiers | [`platform.xiaomimimo.com/static/docs/tokenplan/subscription.md`](https://platform.xiaomimimo.com/static/docs/tokenplan/subscription.md) |
| Token Plan quick-access (cluster URLs) | [`platform.xiaomimimo.com/static/docs/tokenplan/quick-access.md`](https://platform.xiaomimimo.com/static/docs/tokenplan/quick-access.md) |
| Console (API keys) | [`platform.xiaomimimo.com/#/console/api-keys`](https://platform.xiaomimimo.com/#/console/api-keys) |

If a path 404s, fetch `llms.txt` to find the new path — paths shift.

Always `curl` when you need fresh info — the skill snapshot will go stale.

## Models (2026-04-29 snapshot — verify against live pricing page)

| Model | Context | Vision input | Tool calls | Notes |
|---|---|---|---|---|
| `mimo-v2.5-pro` | 1M | ❌ | ✅ | Flagship (text-only). 2× credit consumption on Token Plan. |
| `mimo-v2.5` | 1M | ✅ | ✅ | **Default for the `mimo` preset** — sweet spot. |
| `mimo-v2-omni` | 256K | ✅ | ✅ | Omni-modal (vision/audio/video input). |
| `mimo-v2-flash` | 256K | ❌ | ✅ | Cheapest. Recommended `temperature` 0.3. |
| `mimo-v2.5-tts*` | — | — | ❌ | TTS — limited-time free, no Token Plan credit consumption. |

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
| Vision input on a non-vision LLM | `vision` skill (router) — MiMo's `mimo-v2.5`/`mimo-v2-omni` are an option there too |
| TTS / ASR | `listen` skill if local; otherwise the MiMo TTS models via Anthropic-compat endpoint (no MCP wrapper) |
| Web search / web reading | `web-browsing` skill — MiMo has no MCP search tool |
| Image / video / music *generation* | `minimax-token-plan` skill — MiMo doesn't generate media |

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

If the live docs contradict this skill, trust the docs and (if the human consents) update this file. The cluster URLs and `mimo-v2-*` model names are the highest-churn items — verify against `platform.xiaomimimo.com/llms.txt` first.
