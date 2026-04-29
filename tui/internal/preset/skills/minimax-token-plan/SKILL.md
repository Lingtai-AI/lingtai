---
name: minimax-token-plan
description: >
  Use the MiniMax coding-plan subscription as a unified multimedia backend —
  vision, music, video, image, and speech generation. One `sk-cp-…` key
  unlocks all five modalities. This skill is a thin pointer: it tells you
  how to source the key, pick the region, and where the live docs are.
  For tool-call details (model names, parameters, quotas), fetch the docs
  yourself. MCP server registration is owned by the `lingtai-mcp` skill.
version: 1.0.0
---

# minimax-token-plan

> Thin pointer. Live docs are the source of truth — `curl` them when you need depth.

## Live Docs (canonical)

When you need details — current model names, exact parameters, per-tier quotas, MCP package names — fetch these. Both regions publish the same content; pick the one matching the user's account.

| Topic | Mainland (`.com`) | International (`.io`) |
|---|---|---|
| Token plan intro & quotas | [`platform.minimaxi.com/docs/token-plan/intro`](https://platform.minimaxi.com/docs/token-plan/intro) | [`platform.minimax.io/docs/token-plan/intro`](https://platform.minimax.io/docs/token-plan/intro) |
| Coding-plan MCP (`understand_image`, `web_search`) | [`platform.minimaxi.com/docs/token-plan/mcp-guide`](https://platform.minimaxi.com/docs/token-plan/mcp-guide) | [`platform.minimax.io/docs/token-plan/mcp-guide`](https://platform.minimax.io/docs/token-plan/mcp-guide) |
| Full media MCP (TTS, music, video, image) | [`platform.minimaxi.com/docs/guides/mcp-guide`](https://platform.minimaxi.com/docs/guides/mcp-guide) | [`platform.minimax.io/docs/guides/mcp-guide`](https://platform.minimax.io/docs/guides/mcp-guide) |

Always `curl` (or use `web_read`) when you need fresh info — the skill snapshot will go stale.

## What This Skill Tells You

Three things the live docs don't:

1. **Where to get the key.**
2. **How to pick the region.**
3. **When this skill is the right answer vs. another skill.**

Everything else — fetch the docs.

## 1. Sourcing The API Key

**Never hardcode the key into `mcp/servers.json` or any committed file.** The `env` block of an MCP server entry is plain text — leak risk on commit, on backup, on screen-share.

Resolution order:

1. **`~/.lingtai-tui/.env`** — `MINIMAX_API_KEY=…`. The TUI populates this on firstrun.
2. **Process environment** — if already exported, MCP subprocesses inherit it.
3. **Ask the user** — if neither path resolves.

```bash
grep -E '^MINIMAX_API_KEY=' ~/.lingtai-tui/.env | cut -d= -f2- | tr -d ' '
```

## 2. Picking The Region

MiniMax runs two separate ecosystems, **not interchangeable** — a key from one region returns `2049 invalid api key` against the other host. The user may have an account in either or both.

| Region | Portal | API host |
|---|---|---|
| Mainland China | `platform.minimaxi.com` | `api.minimaxi.com` |
| International | `platform.minimax.io` | `api.minimax.io` |

(Yes — the `.com` is mainland and `.io` is international. The names don't follow the usual convention.)

The MCP server registration must match the region of the key being used.

**Auto-detect from the preset library.** Walk *all* presets in `~/.lingtai-tui/presets/` (filename is not reliable — a user's MiniMax preset might be called `cheap.json` or anything else). For each one where `manifest.llm.provider == "minimax"`, inspect `manifest.llm.base_url`:

| `base_url` substring | Region |
|---|---|
| `minimaxi.com` | Mainland China |
| `minimax.io` | International |

```bash
# Scan all presets, list each MiniMax preset's base_url:
for f in ~/.lingtai-tui/presets/*.json ~/.lingtai-tui/presets/*.jsonc; do
  [ -f "$f" ] || continue
  python3 -c "
import json, sys
try:
    d = json.load(open('$f'))
    llm = d.get('manifest', {}).get('llm', {})
    if llm.get('provider') == 'minimax':
        print('$f', '→', llm.get('base_url') or '(null)')
except Exception:
    pass
"
done
```

(`.jsonc` files use `//` line comments; the snippet above will fail on those — strip comments first if needed, or use `python3 -m json.tool` after a sed pass.)

If presets exist for **both** regions, the user has accounts in both — pick the one that matches the key in `~/.lingtai-tui/.env`, or ask the user which one they want for this MCP registration. If no MiniMax preset exists or the result is ambiguous, **ask the user**. Do not guess.

## 3. When To Use This Skill

| Want to … | Use |
|---|---|
| Compose music, generate video, draw image, speak text, analyze image — and you have a coding-plan key | This skill (then fetch the live docs for the specific tool) |
| Transcribe speech or analyze music numerically | `listen` skill (local — no key needed) |
| Analyze an image but no MiniMax key | `vision` skill (local-VLM path) |
| LLM already has a built-in `vision` tool | Use it directly, save the round-trip |
| Plain text or code | Core capabilities, not media |

## Setup (one-time)

MCP server registration — both `minimax-coding-plan-mcp` (vision + web_search) and `minimax-mcp` (full media) — is handled by the **`lingtai-mcp` skill**. Both packages accept the same coding-plan key.

If a `mcp__MiniMax*__…` tool you need is not in your tool list, that's the signal: register the appropriate server via `lingtai-mcp`, then come back here.

## Failure Modes (quick reference)

| Symptom | Look at |
|---|---|
| Tool not in your list | `lingtai-mcp` skill — register the right server |
| `2049 invalid api key` | Region/host mismatch — re-check section 2 above |
| `2056 usage limit exceeded` | Live docs — quotas |
| `2061 token plan doesn't support model` | Live docs — tier limits |
| Tool hangs 1–10 min | Normal for music/video — do not retry |

For everything else, `curl` the live docs.
