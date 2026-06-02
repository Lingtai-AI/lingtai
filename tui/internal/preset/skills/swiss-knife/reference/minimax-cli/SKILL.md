---
name: minimax-cli
description: >
  Nested swiss-knife reference for the MiniMax `mmx` CLI and the canonical
  MiniMax CLI procedure shipped with the TUI: install `mmx-cli`, discover the
  correct TUI-managed MiniMax preset/key slot without leaking secrets, match
  mainland vs international regions, and route image/video/music/TTS generation
  or one-shot shell vision.
version: 2.1.0
tags: [manual, cli, minimax, mmx, image, video, music, speech, tts, vision, media-generation]
---

# minimax-cli

> **This is a manual, not a tool.** It points you at the official MiniMax CLI (`mmx`). The CLI's own `--help` is the source of truth for syntax; the live docs are the source of truth for models, quotas, regions, and evolving flags. This file covers the LingTai-specific glue: install, credential discovery, region matching, and when to route through this CLI instead of another capability.

## 1. Scope and entry points

Use this skill when a task needs MiniMax-backed media generation or one-shot shell vision:

| Need | Primary route |
|---|---|
| Generate image, video, music, or TTS | `mmx image/video/music/speech ...` after reading the subcommand's `--help` |
| Understand an image from a shell script | `mmx vision ...` for ad-hoc one-shots |
| Vision as an in-turn tool call | Use the `vision` skill first; it may expose the kernel `vision` tool or a registered MCP |
| Music tied to project journals | Use the `dj` skill for the workflow, then this skill for the MiniMax provider step |
| Transcribe speech or analyze audio numerically | Use the `listen` skill (local, no MiniMax key needed) |

This skill is the **canonical MiniMax CLI reference** shipped with the TUI. The top-level `minimax-cli` skill is only a discoverability pointer into this nested reference, so keep MiniMax command recipes and credential guidance here rather than duplicating them there.

## 2. Install the CLI

The official CLI is **`mmx-cli`** on npm (source: [`MiniMax-AI/cli`](https://github.com/MiniMax-AI/cli)). Check first, install if missing:

```bash
command -v mmx >/dev/null || npm install -g mmx-cli
```

Requires `node` + `npm` on `PATH`. If neither is installed, ask the user to install Node; do not bootstrap a Node runtime yourself inside the project.

After install, verify the binary and let the CLI describe its current surface:

```bash
mmx --help
mmx doctor --help || true
```

## 3. Discover credentials without leaking them

**Never print, commit, or paste MiniMax keys.** The TUI stores keys in `~/.lingtai-tui/.env`; presets declare which env slot they use through `manifest.llm.api_key_env`. Modern saved presets usually live under `~/.lingtai-tui/presets/saved/`, so scan recursively instead of only checking the top-level presets directory.

Resolution order:

1. Find MiniMax presets and their declared key slot.
2. Match the slot to `~/.lingtai-tui/.env` without printing the key value.
3. Export the selected value as `MINIMAX_API_KEY` for `mmx`.
4. Match the region/base URL to the preset; do not guess a host.

List candidate presets (prints slot names and base URLs only, never secret values):

```bash
python3 - <<'PY'
import glob, json, os

root = os.path.expanduser("~/.lingtai-tui/presets")
paths = sorted(set(
    glob.glob(os.path.join(root, "**", "*.json"), recursive=True)
    + glob.glob(os.path.join(root, "**", "*.jsonc"), recursive=True)
))

def strip_jsonc(text: str) -> str:
    # Remove // and /* */ comments outside strings. A regex comment stripper
    # would corrupt normal preset URLs such as "https://api.minimaxi.com/anthropic".
    out = []
    i = 0
    in_string = False
    escape = False
    while i < len(text):
        ch = text[i]
        nxt = text[i + 1] if i + 1 < len(text) else ""
        if in_string:
            out.append(ch)
            if escape:
                escape = False
            elif ch == "\\":
                escape = True
            elif ch == '"':
                in_string = False
            i += 1
            continue
        if ch == '"':
            in_string = True
            out.append(ch)
            i += 1
            continue
        if ch == "/" and nxt == "/":
            i += 2
            while i < len(text) and text[i] not in "\r\n":
                i += 1
            continue
        if ch == "/" and nxt == "*":
            i += 2
            while i + 1 < len(text) and not (text[i] == "*" and text[i + 1] == "/"):
                i += 1
            i += 2 if i + 1 < len(text) else 0
            continue
        out.append(ch)
        i += 1
    return "".join(out)

for path in paths:
    try:
        doc = json.loads(strip_jsonc(open(path, encoding="utf-8").read()))
    except Exception:
        continue
    llm = doc.get("manifest", {}).get("llm", {}) or {}
    if (llm.get("provider") or "").lower() != "minimax":
        continue
    slot = llm.get("api_key_env") or "MINIMAX_API_KEY"  # legacy/built-in fallback
    base = llm.get("base_url") or "(default CLI region)"
    region = "CN" if "minimaxi.com" in base else "INTL" if "minimax.io" in base else "unknown"
    rel = os.path.relpath(path, os.path.expanduser("~/.lingtai-tui"))
    print(f"{rel:55s} slot={slot:32s} region={region:7s} base_url={base}")
PY
```

Export the chosen slot without echoing the key. Python is used here so quoted or whitespace-padded `.env` values are handled consistently; for ordinary API-key lines a simple `grep` extraction is also fine, but do not print the value:

```bash
SLOT=MINIMAX_CN_1_API_KEY  # replace with the slot printed above
export MINIMAX_API_KEY="$({
  python3 - "$SLOT" <<'PY'
import os, sys
slot = sys.argv[1]
env_path = os.path.expanduser("~/.lingtai-tui/.env")
value = None
try:
    with open(env_path, encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if not line or line.startswith("#") or "=" not in line:
                continue
            k, v = line.split("=", 1)
            if k == slot:
                value = v.strip().strip('"').strip("'")
                break
except FileNotFoundError:
    pass
if not value:
    raise SystemExit(f"missing {slot} in {env_path}")
print(value)
PY
})"
```

If multiple MiniMax presets exist and the user did not specify which account/region to use, ask. If no MiniMax preset exists, ask the user to save one through the TUI preset library; the TUI will populate the appropriate slot in `~/.lingtai-tui/.env`.

Token-plan keys often have prefix `sk-cp-...`; pay-as-you-go keys (`sk-...` without `cp`) may also work but are billed per call. Treat both as secrets.

## 4. Match the region/base URL

MiniMax has mainland China and international ecosystems. The credential, API host, and quota plan must match. The preset's `base_url` is the best local hint:

- `minimaxi.com` -> mainland China account.
- `minimax.io` -> international account.
- Empty/unknown -> use the CLI default unless the user or live docs say otherwise.

Do **not** hardcode an unverified host. If the CLI supports a region flag or `MINIMAX_BASE_URL`, set it from the preset's `base_url` or the current official docs, not from memory:

```bash
mmx doctor --help || true
mmx --help | sed -n '1,120p'
# If the CLI documents MINIMAX_BASE_URL and your selected preset has a base_url:
# export MINIMAX_BASE_URL="<base_url from the selected preset>"
```

A region/key mismatch commonly appears as `2049 invalid api key`.

## 5. Discover subcommands at call time

Do not memorize command syntax. Ask the installed CLI immediately before generating:

```bash
mmx --help
mmx image --help
mmx video --help
mmx music --help
mmx speech --help
mmx vision --help
mmx doctor
```

As of this writing the CLI commonly exposes `text`, `image`, `video`, `music`, `speech`, and `vision` subcommands. Outputs usually land in `./minimax-output/`, and most subcommands have an output flag such as `--out`; verify the current flag with `--help` before every scripted use.

Live docs when `--help` is not enough:

- International: <https://platform.minimax.io/docs/token-plan/minimax-cli>
- Mainland China: <https://platform.minimaxi.com/docs/token-plan/minimax-cli>

## 6. Generation workflow

1. **Confirm the requested modality and cost.** Media calls can take minutes and may consume paid quota. For ambiguous prompts, ask before calling.
2. **Create an artifact directory** inside the project or requested destination, e.g. `artifacts/minimax/<task-slug>/`.
3. **Run `mmx <subcommand> --help`** and build the command from the live flags.
4. **Send a progress update** for human-facing long calls (>5 seconds) before waiting.
5. **Save outputs explicitly** with an output flag/path when available; otherwise move files out of `./minimax-output/` after the call.
6. **Report only paths and a concise summary.** Do not include keys, raw auth headers, or huge provider payloads.

Skeleton:

```bash
mkdir -p artifacts/minimax/example
mmx music --help
# Build the real command from the help output; example flags may change:
# mmx music generate --prompt "..." --out artifacts/minimax/example/
```

## 7. Vision-specific routing

MiniMax vision can appear through multiple LingTai paths:

- **Kernel `vision` tool**: best when present in the agent's tool list; use the `vision` skill's decision tree.
- **MiniMax CLI (`mmx vision ...`)**: best for one-shot shell/OCR/image-description jobs.
- **MCP vision tools**: only if an MCP is already registered; MCP setup belongs to `mcp-manual`.

If a user asks for image understanding and you already have the built-in `vision` tool, use it first. Load this skill for CLI fallback or batch shell work.

## 8. Failure modes

| Symptom | Likely cause / action |
|---|---|
| `mmx: command not found` | Install `mmx-cli`; verify npm's global bin directory is on `PATH` |
| No MiniMax preset found | Ask the user to save a MiniMax preset through the TUI preset library |
| Slot printed by preset scan but missing from `.env` | Preset/key state is incomplete; ask the user to re-save the preset or add the key through the TUI |
| `2049 invalid api key` | Region/key mismatch or wrong slot; re-check selected preset base URL and slot |
| `2056 usage limit exceeded` | Quota/plan exhausted; stop and report plainly |
| Media call appears to hang | Music/video can take 1-10 minutes; wait rather than retrying immediately |
| CLI flags differ from examples | Trust `mmx <subcommand> --help`; update this skill if the drift is persistent |

The older MCP-server route (`minimax-mcp`, `minimax-coding-plan-mcp`) still exists for MCP workflows and is owned by `mcp-manual`. Prefer the official CLI here when a shell command is sufficient: one binary covers media, speech, text, and ad-hoc vision.

---
> **Found a bug or issue?** If you encounter any problems with this skill, load the `lingtai-issue-report` skill and follow its instructions to report it.
