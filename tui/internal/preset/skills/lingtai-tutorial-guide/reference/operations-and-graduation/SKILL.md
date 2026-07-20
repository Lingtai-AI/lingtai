---
name: tutorial-guide-operations-and-graduation
description: >
  Nested tutorial-guide reference for lessons 10–12: TUI commands, lifecycle exercises, addons, external connections, and graduation.
version: 1.0.0
last_changed_at: "2026-07-18T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# Tutorial Guide — Operations and Graduation Lessons

Nested tutorial-guide reference for operations and graduation lessons 10–12, reached from the root `tutorial-guide` router. Teach live per the router's *discover, don't recite* rule: read the real file or run the real command before explaining it.

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

**Addon setup route:**
1. Start with `mcp-manual` and its `reference/curated-addons.md`.
2. Use the relevant provider documentation for exact fields and supported setup steps; do not invent config paths or duplicate the kernel schema here.
3. Require explicit authorization before making each config change.

Key concepts to teach:
- Avatars do NOT inherit addons
- `/mcp` TUI command is read-only inspection of MCP configuration and status; it is not a setup or editor mechanism
- Run `/refresh` only after an authorized config change

## Lesson 12: Graduation

- Congratulate the human.
- Next step: run `lingtai-tui` in a new project to create their own agent.
- Remind them to follow the current MCP/curated-addon documentation for addon setup, with explicit authorization; use `/mcp` only to inspect config/status.
- To resume tutorial: rerun `lingtai-tui` in the same folder. To restart: `/nirvana` then `/setup` with Tutorial recipe.
- The network grows with every avatar spawned.
