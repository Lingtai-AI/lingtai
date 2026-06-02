---
name: swiss-knife
description: >
  Umbrella router for small, focused CLI tools and integrations. Read this
  when a task might need one of the bundled utility references, then load only
  the nested reference that matches the need: claude-code, openai-codex, or
  opencode for coding CLIs; minimax-cli for MiniMax media/TTS/vision; token-usage
  for token/cost reports; html-report for standalone browser deliverables;
  xiaomi-mimo for Xiaomi MiMo provider discovery; zhipu-coding-plan for Z.AI /
  BigModel coding-plan capabilities; headless-bot for provisioning fresh
  LingTai bot projects such as Telegram bots from `lingtai-tui spawn`. This
  parent is the route map; each nested reference is self-contained under
  `reference/<name>/SKILL.md`.
version: 2.0.1
tags: [utilities, umbrella, toolkit, nested-skill]
---

# Swiss Knife — Utility Toolkit Router

Swiss Knife is a top-level router for small, focused utility references. Enter
through this file, choose exactly one nested reference from the catalog below,
then read that nested `SKILL.md` for the actual procedure, examples, scripts, and
assets.

## Nested reference catalog

```yaml
nested_references:
  - name: claude-code
    location: reference/claude-code/SKILL.md
    description: >
      Nested swiss-knife reference for Claude Code CLI. Read this when you need
      to delegate code implementation, patch writing, documentation, refactoring,
      or review to Anthropic's Claude Code CLI, including guidance on async
      supervision, model choice, and responsiveness risks.
  - name: openai-codex
    location: reference/openai-codex/SKILL.md
    description: >
      Nested swiss-knife reference for OpenAI Codex CLI. Read this when the
      human asks to use Codex directly, compare Codex with Claude Code, or use
      Codex-specific local-agent capabilities such as remote control, Vim editing,
      plugins, hooks, and browser integration.
  - name: opencode
    location: reference/opencode/SKILL.md
    description: >
      Nested swiss-knife reference for OpenCode CLI. Read this when the human
      asks to use OpenCode as a local coding-agent CLI, compare coding CLIs, or
      script provider-flexible `opencode run` / `opencode serve` workflows.
  - name: minimax-cli
    location: reference/minimax-cli/SKILL.md
    description: >
      Nested swiss-knife reference for the MiniMax `mmx` CLI. Read this for
      MiniMax-backed image, video, music, TTS, or ad-hoc shell vision tasks.
  - name: token-usage
    location: reference/token-usage/SKILL.md
    description: >
      Nested swiss-knife reference for network-wide token and cost reports. Read
      this when the human asks about token usage, budget, model costs, or spending
      across agent ledgers.
  - name: html-report
    location: reference/html-report/SKILL.md
    description: >
      Nested swiss-knife reference for standalone HTML deliverables. Read this
      when the human asks for a polished browser-openable report, dashboard,
      memo, comparison, or any HTML artifact with MathJax equations.
  - name: xiaomi-mimo
    location: reference/xiaomi-mimo/SKILL.md
    description: >
      Nested swiss-knife reference for Xiaomi MiMo provider discovery. Read this
      when the human asks to use or configure Xiaomi MiMo / 小米MiMo, or when you
      need to understand the MiMo model family and compatible endpoints.
  - name: zhipu-coding-plan
    location: reference/zhipu-coding-plan/SKILL.md
    description: >
      Nested swiss-knife reference for the Zhipu / Z.AI / BigModel coding-plan
      subscription. Read this when the human asks about Zhipu credentials or the
      plan's vision, web search, web-read, and zread MCP capabilities.
  - name: headless-bot
    location: reference/headless-bot/SKILL.md
    description: >
      Nested swiss-knife reference for creating and operating headless LingTai
      bot projects from `lingtai-tui spawn`. The current helper provisions a
      Telegram MCP bot, and the workflow also covers preset-policy replication,
      addon wiring, secret hygiene, refresh/relaunch, and verification.
```

## Routing table

| Need | Read |
|---|---|
| Delegate code work to Claude Code CLI | `reference/claude-code/SKILL.md` |
| Use or compare OpenAI Codex CLI | `reference/openai-codex/SKILL.md` |
| Use or compare OpenCode CLI | `reference/opencode/SKILL.md` |
| Generate images, video, music, TTS, or MiniMax shell vision | `reference/minimax-cli/SKILL.md` |
| Report token usage or model costs | `reference/token-usage/SKILL.md` |
| Produce standalone HTML reports/dashboards/memos | `reference/html-report/SKILL.md` |
| Discover/configure Xiaomi MiMo | `reference/xiaomi-mimo/SKILL.md` |
| Discover/configure Zhipu / Z.AI coding-plan capabilities | `reference/zhipu-coding-plan/SKILL.md` |
| Create or automate a headless LingTai bot project, including Telegram bots | `reference/headless-bot/SKILL.md` |

## How to use this router

1. Match the request to one row in the routing table.
2. Read the nested reference at the relative `location` next to this parent
   `SKILL.md`.
3. Follow that nested reference's procedure. Each nested reference owns its own
   scripts and assets under its folder.

Do not load every nested reference by default. Swiss Knife exists to avoid
polluting working context with unrelated utility manuals.

## Adding a nested utility reference

1. Create `reference/<name>/SKILL.md` with normal skill frontmatter (`name`,
   `description`, optional `version`/`tags`). The description should start with
   the fact that it is a nested swiss-knife reference and should state when to
   read it.
2. Put that reference's `scripts/`, `assets/`, and extra files under the same
   `reference/<name>/` folder.
3. Add the child to the `nested_references` YAML block and routing table above.
4. Run validation/tests. At minimum, verify the parent mentions the child and the
   embedded TUI skill tree still copies every nested file.
5. Re-extract the embedded skill bundle to disk after installing/updating the TUI:
   the human runs `lingtai-tui bootstrap`, then agents call `system(action="refresh")`
   or `skills(action="info")` to see the refreshed utility tree.

## Design philosophy

- **Router first** — Swiss Knife itself exposes `swiss-knife/SKILL.md` as its
  top-level catalog entry; its bundled references are reached through that router
  rather than promoted as sibling `swiss-knife/<child>` skills.
- **Self-contained children** — each nested reference carries its own examples,
  scripts, and assets.
- **Single-purpose children** — if a nested utility grows into a broadly useful
  workflow that agents should discover directly, promote it to a normal top-level
  skill instead of keeping it under Swiss Knife.
- **Small working context** — read one nested reference at a time.

---
> **Found a bug or issue?** If you encounter any problems with this skill, load
> the `lingtai-issue-report` skill and follow its instructions to report it.
