---
name: zhipu-coding-plan
description: >
  Use the Zhipu / Z.AI GLM coding-plan subscription as a unified backend
  for vision (multimodal screenshot/diagram analysis), web search, web
  page reading, and zread (open-source GitHub repo browsing). One API
  key unlocks all four MCP servers. This skill is a thin pointer: it
  tells you how to source the key, pick the region (Z.AI international
  vs BigModel mainland), and where the live docs are. MCP server
  registration is owned by the `mcp-manual` skill (kernel capability).
version: 1.0.0
---

# zhipu-coding-plan

> Thin pointer. Live docs are the source of truth — `curl` them when you need depth.

## Live Docs (canonical)

When you need details — current models, exact tool parameters, per-tier quotas, MCP package or endpoint changes — fetch these. Both regions publish equivalent content; pick the one matching the user's account. URLs verified live 2026-04-29.

| Topic | International (Z.AI) | Mainland (BigModel / 智谱) |
|---|---|---|
| Coding-plan overview | [`docs.z.ai/devpack/overview`](https://docs.z.ai/devpack/overview) | [`docs.bigmodel.cn/cn/coding-plan/overview`](https://docs.bigmodel.cn/cn/coding-plan/overview) |
| Vision MCP server | [`docs.z.ai/devpack/mcp/vision-mcp-server`](https://docs.z.ai/devpack/mcp/vision-mcp-server) | [`docs.bigmodel.cn/cn/coding-plan/mcp/vision-mcp-server`](https://docs.bigmodel.cn/cn/coding-plan/mcp/vision-mcp-server) |
| Web search MCP | [`docs.z.ai/devpack/mcp/search-mcp-server`](https://docs.z.ai/devpack/mcp/search-mcp-server) | [`docs.bigmodel.cn/cn/coding-plan/mcp/search-mcp-server`](https://docs.bigmodel.cn/cn/coding-plan/mcp/search-mcp-server) |
| Web reader MCP | [`docs.z.ai/devpack/mcp/reader-mcp-server`](https://docs.z.ai/devpack/mcp/reader-mcp-server) | [`docs.bigmodel.cn/cn/coding-plan/mcp/reader-mcp-server`](https://docs.bigmodel.cn/cn/coding-plan/mcp/reader-mcp-server) |
| Zread (GitHub repos) MCP | [`docs.z.ai/devpack/mcp/zread-mcp-server`](https://docs.z.ai/devpack/mcp/zread-mcp-server) | [`docs.bigmodel.cn/cn/coding-plan/mcp/zread-mcp-server`](https://docs.bigmodel.cn/cn/coding-plan/mcp/zread-mcp-server) |
| Doc index (use if any URL above stales) | [`docs.z.ai/llms.txt`](https://docs.z.ai/llms.txt) | [`docs.bigmodel.cn/llms.txt`](https://docs.bigmodel.cn/llms.txt) |

Note the URL pattern asymmetry: international is `/devpack/mcp/...`, mainland is `/cn/coding-plan/mcp/...`. If a path 404s, fetch the `llms.txt` index above to find the new path — they shift.

Always `curl` (or use the `web-browsing` skill) when you need fresh info — the skill snapshot will go stale.

## What You Get With One Key

A single Z.AI / BigModel coding-plan key unlocks four MCP servers. Per-tier quotas (snapshot — verify against the live page):

| Capability | Lite | Pro | Max |
|---|---|---|---|
| **Vision MCP** (8 tools — see below) | shared | shared | shared |
| **Web search** (`webSearchPrime`) | 100 / total | 1,000 / total | 4,000 / total |
| **Web reader** (`webReader`) | 100 / total | 1,000 / total | 4,000 / total |
| **Zread** (3 tools) | 100 / total | 1,000 / total | 4,000 / total |

Quotas are *cumulative-total*, not daily — once exhausted, you're done until the plan period rolls over.

## The Four MCP Servers

| Modality | Server | Tools |
|---|---|---|
| **vision** | stdio: `npx -y @z_ai/mcp-server` | `image_analysis`, `extract_text_from_screenshot`, `ui_to_artifact`, `diagnose_error_screenshot`, `understand_technical_diagram`, `analyze_data_visualization`, `ui_diff_check`, `video_analysis` |
| **web search** | http: `https://api.z.ai/api/mcp/web_search_prime/mcp` | `webSearchPrime` |
| **web reader** | http: `https://api.z.ai/api/mcp/web_reader/mcp` | `webReader` |
| **zread** | http: `https://api.z.ai/api/mcp/zread/mcp` | `search_doc`, `get_repo_structure`, `read_file` |

**Mainland (BigModel) HTTP MCP host** is `https://open.bigmodel.cn/api/mcp/...` — substitute that origin for `https://api.z.ai` in the URLs above. (`api.bigmodel.cn` 301-redirects to `open.bigmodel.cn`.) Verified live 2026-04-29.

**MCP server registration is owned by the `mcp-manual` skill** (kernel `mcp` capability) — read it for how to register, activate, and troubleshoot MCP servers (registry route or legacy `mcp/servers.json`). This skill assumes the tools are already in your tool list. If the tool you need is *not* in your list, that's the signal to register the server first.

## Sourcing The API Key

**Never hardcode the key into `mcp/servers.json` or any committed file.** The `env` block (for stdio) and `headers` block (for http) are plain text — leak risk on commit, on backup, on screen-share.

Resolution order:

1. **`~/.lingtai-tui/.env`** — `ZHIPU_API_KEY=…`. The TUI populates this on firstrun.
2. **Process environment** — if already exported, MCP subprocesses inherit it.
3. **Ask the user** — if neither path resolves.

```bash
grep -E '^ZHIPU_API_KEY=' ~/.lingtai-tui/.env | cut -d= -f2- | tr -d ' '
```

The vision MCP also wants a `Z_AI_MODE` env var (`ZAI` for international, `ZHIPU` for mainland) — derive it from the region detection below.

## Picking The Region

Two ecosystems, **not interchangeable** — a key from one region returns auth errors against the other host.

| Region | Console | API host (HTTP MCP) | `Z_AI_MODE` |
|---|---|---|---|
| International (Z.AI) | `z.ai/manage-apikey/apikey-list` | `api.z.ai` | `ZAI` |
| Mainland (BigModel / 智谱) | `open.bigmodel.cn` | `open.bigmodel.cn` | `ZHIPU` |

The MCP server registration must match the region of the key being used.

**Auto-detect from the preset library.** Walk all presets in `~/.lingtai-tui/presets/`. For each one where `manifest.llm.provider == "zhipu"`, inspect `manifest.llm.base_url`:

| `base_url` substring | Region | Mode |
|---|---|---|
| `z.ai` or `api.z.ai` | International | `ZAI` |
| `bigmodel.cn` | Mainland | `ZHIPU` |

```bash
for f in ~/.lingtai-tui/presets/*.json ~/.lingtai-tui/presets/*.jsonc; do
  [ -f "$f" ] || continue
  python3 -c "
import json
try:
    d = json.load(open('$f'))
    llm = d.get('manifest', {}).get('llm', {})
    if llm.get('provider') == 'zhipu':
        print('$f', '→', llm.get('base_url') or '(null)')
except Exception:
    pass
"
done
```

If presets exist for **both** regions, the user has accounts in both — pick the one matching the key in `~/.lingtai-tui/.env`, or ask the user. If no Zhipu preset exists or the result is ambiguous, **ask the user**. Do not guess.

## When To Use This Skill

| Want to … | Use |
|---|---|
| Analyze image/screenshot/UI/diagram, no vision-capable LLM | This skill — vision MCP (8 specialized tools) |
| Search the web with rich snippets | This skill — `webSearchPrime` (or built-in `web_search` capability if Zhipu is your LLM provider — that path uses the same MCP) |
| Fetch and read a URL | This skill — `webReader` (or `web-browsing` skill for non-Zhipu free tier) |
| Browse a GitHub repo (search docs, list files, read code) | This skill — zread |
| Process a video file (≤8 MB) | This skill — `video_analysis` |
| Pure compute / code generation | Just use your LLM directly |

## Vision MCP — Tool Picker

The vision server exposes 8 tools, more specialized than MiniMax's single `understand_image`. Pick the right one:

| Want to … | Tool |
|---|---|
| General "what's in this image?" | `image_analysis` |
| OCR (extract text from screenshot) | `extract_text_from_screenshot` |
| Convert UI mockup → code/spec | `ui_to_artifact` |
| Debug an error screenshot | `diagnose_error_screenshot` |
| Read an architecture / system diagram | `understand_technical_diagram` |
| Read a chart / dashboard | `analyze_data_visualization` |
| Diff two UI screenshots | `ui_diff_check` |
| Analyze a video file (≤8 MB MP4/MOV/M4V) | `video_analysis` |

Using the right specialized tool gives noticeably better output than `image_analysis` for charts, diagrams, or OCR.

## Failure Modes

| Symptom | Likely cause | Fix |
|---|---|---|
| Tool not in your list | MCP server not registered | Use `mcp-manual` skill |
| `401 Unauthorized` / `Invalid API key` | Region mismatch, or stale key | Verify host matches account region; refresh `~/.lingtai-tui/.env` |
| `429 Rate limited` or quota error | Plan total exhausted | Live docs — check current quota; the cumulative-total clock resets per plan period |
| Vision MCP times out on video | File >8 MB or wrong format | Compress with ffmpeg or use a different format (MP4/MOV/M4V only) |
| `Z_AI_MODE not set` warnings | Missing env var on stdio MCP | Set `ZAI` (international) or `ZHIPU` (mainland) in the server's `env` block |

## Self-Healing

If the live docs contradict this skill, trust the docs and (if the human consents) update this file. The four MCP server URLs and the package name (`@z_ai/mcp-server`) are the highest-churn items — verify them against `docs.z.ai/llms.txt` first.
