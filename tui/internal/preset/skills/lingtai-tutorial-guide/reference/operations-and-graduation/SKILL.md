---
name: tutorial-guide-operations-and-graduation
description: >
  Nested tutorial-guide reference for lessons 10–12: TUI commands, lifecycle exercises, addons, external connections, and graduation.
version: 1.0.0
---

# Tutorial Guide — Operations and Graduation Lessons

Nested tutorial-guide reference for operations and graduation lessons 10–12.

Use this file after the root `tutorial-guide` router sends you here. Keep teaching live: discover current files, commands, and runtime state before explaining them.

## Lesson 10: TUI Commands and Lifecycle

Read `~/.lingtai-tui/commands.json` via bash. Parse the JSON and present each command with its detailed description in the human's language.

Keyboard shortcuts: explain ctrl+o (three verbose modes: off → verbose → extended) and ctrl+e (external editor).

**Hands-on lifecycle exercise:**
1. `/sleep` → agent sleeps → human sends message to wake
2. `/suspend` → agent dies → human uses `/refresh` to revive → sends message to wake
3. Explain `/sleep all` and `/suspend all` for network management

**CLI commands**: run `lingtai-tui --help` to discover and explain available subcommands.

**Critical warning**: closing the TUI does NOT stop agents. They are independent processes. Teach the CLI management commands for headless control.

## Lesson 11: Addons — External Connections

**Discover available addons dynamically** — use `skills({"action": "info"})` to get a full catalog of available skills, then look for addon setup skills following the naming pattern `lingtai-*-setup`. List whatever you find and ask the human which ones interest them.

For each addon the human wants to set up:
1. Use `skills()` to find and read the setup skill's SKILL.md
2. Follow its instructions exactly — do not hardcode setup steps

Key concepts to teach:
- Secrets go in `.env`, not config files (config uses `*_env` references)
- Config lives at `.lingtai/.addons/<addon>/config.json` (project-level, shared by all agents)
- Avatars do NOT inherit addons
- `/mcp` TUI command opens the MCP control panel and shows current configs
- `/refresh` to apply changes

## Lesson 12: Graduation

- Congratulate the human.
- Next step: run `lingtai-tui` in a new project to create their own agent.
- Remind them about addon setup via the `/mcp` control panel or editing configs directly.
- To resume tutorial: rerun `lingtai-tui` in the same folder. To restart: `/nirvana` then `/setup` with Tutorial recipe.
- The network grows with every avatar spawned.
