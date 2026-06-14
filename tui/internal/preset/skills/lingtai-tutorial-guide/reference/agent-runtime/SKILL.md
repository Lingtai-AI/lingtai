---
name: tutorial-guide-agent-runtime
description: >
  Nested tutorial-guide reference for lessons 4–6: init.json, lingtai-agent run, heartbeat and signal files, TUI runtime wrapping, and system prompt identity.
version: 1.0.0
---

# Tutorial Guide — Agent Runtime Lessons

Nested tutorial-guide reference for agent runtime lessons 4–6.

Use this file after the root `tutorial-guide` router sends you here. Keep teaching live: discover current files, commands, and runtime state before explaining them.

## Lesson 4: How Agents Are Born — init.json and `lingtai-agent run`

### Part 1: init.json

Read YOUR init.json and walk through every field you find. **Do not recite a list of fields — read the file and explain what is there.** The human sees the real structure.

Key patterns to explain:
- The `_file` convention: for `principle`, `covenant`, `soul`, `procedures`, `pad`, `prompt`, `brief`, `comment` — inline text or a path to a shared file.
- `manifest` contains: llm, agent_name, language, capabilities, soul, stamina, admin, etc.
- `addons` connects to external messaging services.
- `env_file` for secrets.

### Part 2: `lingtai-agent run`

Explain the boot sequence: read init.json → load env → resolve venv → build Agent → clean stale signals → install signal handlers → start in ASLEEP state → `agent.start()` blocks on shutdown.

Emphasize: the agent is a long-running Python process. It does not exit after one task.

### Part 3: Heartbeat and signal files

Show your own `.agent.heartbeat` — read it, wait a second, read again to show the timestamp changes. Explain the signal files: `.interrupt`, `.suspend`, `.sleep`, `.prompt`.

## Lesson 5: The TUI — How lingtai-tui Wraps the Agent Runtime

Explain: **lingtai-tui is a Go frontend, not the agent.** It creates agents (writes init.json), launches them (`python -m lingtai run`), monitors them (.agent.heartbeat, .agent.json), controls them (signal files), and manages communication (reads/writes mailbox/).

Draw the architecture diagram (TUI ↔ filesystem ↔ agent process).

Explain: **you do not need the TUI to run an agent.** A valid init.json + `lingtai-agent run` is sufficient.

Walk through TUI-specific features: preset system, setup wizard, slash commands (read from `~/.lingtai-tui/commands.json` to list them), keyboard shortcuts (ctrl+o, ctrl+e), text selection (Option/Alt+drag), network visualization, human directory.

CLI management commands: run `lingtai-tui --help` via bash to discover the available subcommands and explain each one.

## Lesson 6: Identity — How the System Prompt Works

Read your own `system/system.md` and show the human the fully assembled system prompt.

**To discover the section order**: read the source code. Run:
```bash
python3 -c "from lingtai_kernel.prompt import SystemPromptManager; print(SystemPromptManager._DEFAULT_ORDER)"
```

This gives you the real, current section render order. Walk through each section in that order, explaining what it is and whether it's protected (host-written) or editable (agent-written). Read the actual file for each section under `system/` to show real content.

Key concepts to explain:
- Protected sections (principle, covenant, rules, procedures) cannot be changed by the agent
- Editable sections (identity/lingtai, pad) are how the agent evolves
- Brief is externally maintained by the secretary
- Skills are discovered at runtime
- Comment is app-level instructions (like your tutorial instructions)

Emphasize that **identity/character** (system/lingtai.md) is the key to individuality — it's how agents develop unique personalities through experience.
