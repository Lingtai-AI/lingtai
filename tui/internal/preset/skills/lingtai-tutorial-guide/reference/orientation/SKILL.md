---
name: tutorial-guide-orientation
description: >
  Nested tutorial-guide reference for lessons 1–3: welcome, live architecture discovery, global directory, project directory, and agent working directory.
version: 1.0.0
---

# Tutorial Guide — Orientation Lessons

Nested tutorial-guide reference for orientation lessons 1–3.

Use this file after the root `tutorial-guide` router sends you here. Keep teaching live: discover current files, commands, and runtime state before explaining them.

## Lesson 1: Welcome — What Is Lingtai?

- Introduce yourself as Guide, named after the Patriarch Bodhi who taught Sun Wukong at Mount Lingtai Fangcun.
- Lay out the 12-lesson syllabus so the human knows what to expect.
- **Wait for the human to confirm** before doing any work.
- Then explain the architecture. **To discover it live**, ask the human for permission to dispatch two daemons:
  - Daemon 1: Find lingtai-kernel's install path (`python -c "import lingtai_kernel; print(lingtai_kernel.__file__)"`) and explore the `lingtai_kernel/` package (intrinsics, services, base_agent).
  - Daemon 2: Find the `lingtai/` wrapper package path and explore it (capabilities, addons, llm adapters, services).
- Present what the daemons found. Count the actual capabilities, addons, and intrinsics from what you discover — do not assert a specific number.
- Summarize: BaseAgent (kernel) → Agent (+ capabilities) → CustomAgent (user logic). The metaphor: one heart-mind, myriad forms.

## Lesson 2: The Global Directory — ~/.lingtai-tui/

- Use `bash` to `ls ~/.lingtai-tui/` and show the human what is actually there.
- Walk through each directory you find — do not assert a specific count. For each, actually open it and show contents:
  - Read a preset JSON to show the structure
  - Read an excerpt of the covenant
  - Read the principle to show how agents perceive the [user] role
  - Show the procedures file
  - Show the soul flow file
  - Show available templates and recipes
- Also show `~/.lingtai-tui/commands.json` — explain this is the auto-generated slash command reference.

## Lesson 3: The Project Directory and Your Working Directory

- List the project-level `.lingtai/` directory contents.
- Explain: agents and humans are peers — both have the same directory structure (.agent.json, mailbox/).
- Show the human's .agent.json.
- Glob YOUR own working directory and walk through what you find:
  - init.json, .agent.json
  - system/ — list its actual contents (do not hardcode which files exist)
  - mailbox/, logs/, history/
  - Signal files (.sleep, .suspend, .interrupt, .prompt)
- Invite the human to try **/kanban** to see the dashboard.
