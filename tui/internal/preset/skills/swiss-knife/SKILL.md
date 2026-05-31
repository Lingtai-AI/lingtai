---
name: swiss-knife
description: >
  Umbrella skill for small, focused CLI tools and integrations. Sub-skills:
  (1) claude-code — multi-turn Claude Code CLI with persistent sessions via
  --session-id/--resume. Supports parallel sessions, model switching
  (haiku/sonnet/opus), budget control, and tool permission management.
  Use for delegating coding tasks, code review, iterative development;
  (2) minimax-cli — MiniMax CLI for text-to-image, text-to-video, music
  generation, TTS, and vision. Read when the human asks for any media
  creation or vision task;
  (3) openai-codex — OpenAI Codex CLI for local coding agent with remote
  control, Vim editing, plugins, hooks, and Chrome browser integration.
  Read when the human asks to use OpenAI Codex or compare with Claude Code;
  (4) opencode — OpenCode CLI for local coding-agent runs with 75+ provider
  routing, non-interactive `opencode run`, reusable `opencode serve`, custom
  agents, MCP integration, and session resume/fork support. Read when the
  human asks to use OpenCode as a CLI tool or compare coding CLIs;
  (5) token-usage — token usage tracking and cost reporting;
  (6) html-report — checklist + template for producing standalone HTML
  research memos, dashboards, and audit reports (with MathJax math
  rendering, anchor navigation, print styles). Read when the human asks
  for an HTML deliverable, especially one containing equations;
  (7) xiaomi-mimo — discovery protocol for the Xiaomi MiMo (小米MiMo)
  LLM provider: one API key, OpenAI/Anthropic-compatible endpoint, family
  of ~9 models spanning long-context reasoning, multimodal chat, and TTS.
  Read when the human asks to use or configure Xiaomi MiMo;
  (8) zhipu-coding-plan — pointer for the Zhipu / Z.AI GLM coding-plan
  subscription that unlocks vision, web search, web page reading, and
  zread MCP servers from one API key. Read when the human asks about
  Zhipu / Z.AI / BigModel credentials or the coding-plan subscription.
  Each sub-skill is independent — read only the one you need.
  Note: if a sub-skill listed below is missing from your on-disk
  utilities (e.g. you pulled a TUI update), ask the human to run
  `lingtai-tui bootstrap` in a shell — that re-extracts shipped skills
  to ~/.lingtai-tui/utilities/ without restarting the TUI — then call
  `system(action="refresh")` to pick them up.
version: 1.7.0
tags: [utilities, umbrella, toolkit]
---

# Swiss Knife — Utility Toolkit

A collection of small, useful skills. Each sub-skill lives in its own folder under `swiss-knife/` and is fully self-contained — scripts, assets, and a SKILL.md with complete instructions.

## Sub-Skills

| Sub-Skill | Description | When to Use |
|-----------|-------------|-------------|
| [token-usage](token-usage/) | Network-wide token cost calculator using litellm + OpenRouter pricing | Human asks about costs, budget, token usage, or spending |
| [claude-code](claude-code/) | Delegate code implementation, patch writing, docs, and refactoring to Claude Code CLI | Human asks to write code, generate patches, refactor, or delegate implementation work |
| [minimax-cli](minimax-cli/) | MiniMax CLI for text-to-image, text-to-video, music generation, TTS, and vision | Human asks for image/video/music generation, TTS narration, or vision tasks |
| [openai-codex](openai-codex/) | OpenAI Codex CLI — local coding agent with remote control, Vim editing, plugins, hooks, and Chrome extension | Human asks to use OpenAI Codex CLI, compare with Claude Code, or needs browser integration |
| [opencode](opencode/) | OpenCode CLI — provider-flexible local coding agent with `opencode run`, `serve`, custom agents, MCPs, and session resume/fork | Human asks to use OpenCode as a CLI tool, compare coding CLIs, or script a provider-flexible coding agent |
| [html-report](html-report/) | Checklist + standalone HTML template (MathJax, nav, print styles) for research memos, dashboards, audit reports | Human asks for an HTML deliverable — especially one with equations, where `<pre>`/`<code>` won't render LaTeX |
| [xiaomi-mimo](xiaomi-mimo/) | Discovery protocol for Xiaomi MiMo (小米MiMo) — OpenAI/Anthropic-compatible LLM provider with one key unlocking ~9 models (reasoning, multimodal, TTS) | Human asks to use or configure Xiaomi MiMo |
| [zhipu-coding-plan](zhipu-coding-plan/) | Pointer for the Zhipu / Z.AI GLM coding-plan subscription (one key → vision, web search, web read, zread MCP servers) | Human asks about Zhipu / Z.AI / BigModel credentials or the coding-plan subscription |

## How to Use

1. **Identify the sub-skill** — match the human's request to the one-liner in the table above.
2. **Read the sub-skill's SKILL.md** — `swiss-knife/<name>/SKILL.md` has full instructions, script paths, and examples.
3. **Run the script** — each sub-skill bundles its own executable scripts. Follow the sub-skill's README for the exact command.

## Adding New Sub-Skills

To add a new utility to the swiss-knife:

1. Create a folder: `swiss-knife/<name>/`
2. Add a `SKILL.md` with frontmatter (`name`, `description`, `version`) and full usage instructions
3. Add any scripts/assets in a `scripts/` subfolder
4. Update the table above with a one-liner
5. Re-extract the embedded skill bundle to disk: human runs `lingtai-tui bootstrap` (no TUI restart needed — `~/.lingtai-tui/utilities/` is rewritten from the TUI binary's embed.FS). Then refresh the catalog: `skills(action='info')` or `system(action='refresh')`. The catalog rescan alone is NOT enough — it only reads what's on disk; bootstrap is what gets new files there.

## Design Philosophy

Each sub-skill follows these principles:
- **Self-contained** — all code and assets live in the sub-skill folder
- **Single-purpose** — one sub-skill does one thing well
- **Documented** — SKILL.md has enough context to use without reading source code
- **Small** — if it's bigger than ~200 lines of code, it probably deserves its own top-level skill

---
> **Found a bug or issue?** If you encounter any problems with this skill, load the `lingtai-issue-report` skill and follow its instructions to report it.
