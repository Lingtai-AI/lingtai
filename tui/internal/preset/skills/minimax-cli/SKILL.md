---
name: minimax-cli
description: >
  Manual (not a tool). Points you at the official MiniMax CLI `mmx` —
  one binary that, given a MiniMax key, handles every modality MiniMax
  offers: text-to-image generation, text-to-video generation, music
  generation (with-lyrics or instrumental), text-to-speech (TTS), and
  vision (image understanding). This skill does NOT expose tools; it
  tells you how to install `mmx-cli` (via npm), where the API key lives
  in this environment, and which region flag to use. Once installed,
  `mmx --help` / `mmx <subcommand> --help` is the source of truth for
  syntax — not this manual. Read this skill when the human asks for any
  of: image / video / music generation, TTS narration, or ad-hoc shell
  vision (`mmx vision …`). Note: MiniMax-backed vision in LingTai has
  two paths — this CLI route AND the kernel's built-in `vision` tool /
  MCP `understand_image` route covered by the `vision` skill. Use this
  for one-shot bash; use `vision` when the agent needs vision as a tool
  call inside a reasoning loop.
version: 2.0.0
tags: [manual, cli, minimax, mmx, image, video, music, speech, tts, vision, media-generation]
---

# minimax-cli

> **This is a manual, not a tool.** It points you at the official MiniMax CLI (`mmx`). The CLI's own `--help` is the source of truth for syntax; the live docs are the source of truth for models and quotas. This file only covers the LingTai-specific glue: install, key, region.

## 1. Install The CLI

The official CLI is **`mmx-cli`** on npm (source: [`MiniMax-AI/cli`](https://github.com/MiniMax-AI/cli)). Check first, install if missing:

```bash
command -v mmx >/dev/null || npm install -g mmx-cli
```

Requires `node` + `npm` on `PATH`. If neither is installed, ask the user to install Node — don't try to bootstrap a Node runtime yourself.

## 2. Source The API Key

**Never hardcode the key in any committed file.** The TUI stores keys in `~/.lingtai-tui/.env` and tells each preset which slot to read via `manifest.llm.api_key_env`. Slots are per-preset, so a user with two MiniMax accounts (e.g. mainland + international) has two distinct env vars.

Resolution: **scan presets, find MiniMax ones, read their declared slot.**

```bash
# Walk every preset; for each one whose provider is minimax, print
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
    if llm.get("provider") != "minimax":
        continue
    slot = llm.get("api_key_env") or "MINIMAX_API_KEY"  # built-ins may leave it empty → legacy default
    base = llm.get("base_url") or ""
    print(f"{os.path.basename(path):30s}  slot={slot:30s}  base_url={base}")
PY
```

Slot naming (per `tui/internal/preset/preset.go::AutoEnvVarName`):
- New user-saved presets: `MINIMAX_<CN|INTL>_<N>_API_KEY` — region is read from `base_url` (`minimaxi.com` → CN, `minimax.io` → INTL); `N` is a per-region counter.
- Built-in / legacy presets: leave `api_key_env` empty → fall back to plain `MINIMAX_API_KEY`.

Once you've picked the right slot, export it as `MINIMAX_API_KEY` for `mmx`:

```bash
SLOT=MINIMAX_CN_1_API_KEY    # whichever slot the preset scan returned
export MINIMAX_API_KEY=$(grep -E "^${SLOT}=" ~/.lingtai-tui/.env | cut -d= -f2- | tr -d ' ')
```

If multiple MiniMax presets exist and you can't infer which the human means, **ask** — don't guess. If no MiniMax preset exists at all, ask the human to save one through the TUI's preset library (the TUI will populate the slot for them).

Token-plan keys have prefix `sk-cp-…`. Pay-as-you-go keys (`sk-…` without `cp`) work too but are billed per call.

## 3. Pick The Region

MiniMax runs two non-interchangeable ecosystems. The CLI defaults to **international** (`api.minimax.io`); for a mainland China key, override:

```bash
export MINIMAX_BASE_URL=https://api.minimaxi.com   # mainland
# (leave unset for international — that's the default)
```

The region is already encoded in the preset you picked in §2 — it's the `base_url` field. `minimaxi.com` → mainland, `minimax.io` → international. Match the env override to that. A region/key mismatch returns `2049 invalid api key`.

## 4. Discover Subcommands

Don't memorize syntax — ask the CLI:

```bash
mmx --help
mmx <subcommand> --help     # e.g. mmx music --help, mmx video --help
mmx doctor                  # health check (key, network, version)
```

As of this writing the CLI exposes `text`, `image`, `video`, `music`, `speech`, and `vision` subcommands. By default outputs land in `./minimax-output/`; most subcommands accept `--out <path>` to override. Verify against `--help` — flags evolve.

Live docs (when `--help` isn't enough):
[`platform.minimax.io/docs/token-plan/minimax-cli`](https://platform.minimax.io/docs/token-plan/minimax-cli) (international) · [`platform.minimaxi.com/docs/token-plan/minimax-cli`](https://platform.minimaxi.com/docs/token-plan/minimax-cli) (mainland).

## 5. When To Use This Skill

| Want to … | Use |
|---|---|
| Generate music, video, image, or TTS | This skill — `mmx music/video/image/speech generate …` |
| One-off image understanding from the shell | This skill — `mmx vision …` (cheap, scriptable, no MCP setup) |
| Vision as a tool call inside a reasoning loop | The `vision` skill — uses the kernel's built-in `vision` tool or the `understand_image` MCP tool |
| Transcribe speech / analyse music numerically | `listen` skill (local, no key needed) |
| Plain text or code | Core capabilities, not this |

**On vision specifically:** MiniMax can serve vision in two paths in LingTai. Path A — this CLI (`mmx vision …`) — is best for one-shot bash use. Path B — the `vision` skill, backed by either the kernel's built-in `vision` tool or the `understand_image` MCP tool — is best when an agent needs vision as a tool call inside a longer reasoning loop. Same backend, different shape.

## 6. Failure Modes

| Symptom | Look at |
|---|---|
| `mmx: command not found` | Re-run the install in §1; verify `npm bin -g` is on `PATH` |
| `2049 invalid api key` | Region/host mismatch — check §3 |
| `2056 usage limit exceeded` | Live docs — quotas |
| Tool hangs 1–10 min | Normal for music/video — do not retry |
| Anything else | `mmx doctor`, then the live docs |

The MCP-server route (`minimax-mcp`, `minimax-coding-plan-mcp`) still exists and is owned by the `mcp-manual` skill (kernel `mcp` capability). Prefer the CLI here — it's first-party, simpler, and one binary covers everything.
