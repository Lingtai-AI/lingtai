---
name: tutorial-guide
description: >
  The 12-lesson tutorial curriculum for teaching a human how Lingtai works.
  Router for orientation, agent runtime, communication, memory/molt,
  capabilities, operations, addons, and graduation. Invoke this skill when the
  human is ready to begin or continue the tutorial.
version: 2.0.1
---

# Tutorial Guide — Router

You are guiding a human through 12 lessons about the Lingtai system. Each lesson builds on the previous one. Wait for the human to reply or ask questions before moving on. Send each lesson as a separate email.

Tell the human upfront: "If you would like to jump to any lesson, just let me know."

**CRITICAL PRINCIPLE: Discover, don't recite.** Throughout this tutorial, you must read the actual codebase, files, and directories to teach. Never recite facts from memory about file counts, capability lists, section orders, or directory contents. Always run a command or read a file to get the current truth, then explain what you found. This ensures the tutorial is always accurate even as the system evolves.

## How to run the tutorial

1. Start with Lesson 1 unless the human asks to jump elsewhere.
2. Read the nested reference that contains the lesson you need.
3. Teach one lesson at a time, using live filesystem/tool evidence before explaining.
4. After each lesson, ask "Ready for the next lesson?" or invite questions.
5. If the human asks about something out of order, address it, then return to the plan.

## Nested reference catalog

`tutorial-guide` owns these nested references. They are parent-owned drill-down files, not standalone top-level skills.

```yaml
- name: tutorial-guide-orientation
  location: reference/orientation/SKILL.md
  description: |
    Lessons 1–3: welcome and syllabus, live architecture discovery,
    `~/.lingtai-tui/`, project `.lingtai/`, agent working directories, and
    the `/kanban` invitation.
- name: tutorial-guide-agent-runtime
  location: reference/agent-runtime/SKILL.md
  description: |
    Lessons 4–6: `init.json`, `lingtai-agent run`, boot sequence, heartbeat
    and signal files, how the Go TUI wraps the runtime, and how the system
    prompt / identity is assembled.
- name: tutorial-guide-communication
  location: reference/communication/SKILL.md
  description: |
    Lesson 7: filesystem email, message flow, internal mail, and external
    addon bridges such as IMAP, Telegram, Feishu, and other integrations.
- name: tutorial-guide-memory-and-molt
  location: reference/memory-and-molt/SKILL.md
  description: |
    Lesson 8: kernel intrinsics, the five persistence layers, voluntary molt,
    the charge/briefing to the next self, forced molt risk, and stamina.
- name: tutorial-guide-capabilities
  location: reference/capabilities/SKILL.md
  description: |
    Lesson 9: avatar network exercises, `/kanban` and `/viz`, cross-network
    email, emergency brakes, dynamic capability discovery, and live demos.
- name: tutorial-guide-operations-and-graduation
  location: reference/operations-and-graduation/SKILL.md
  description: |
    Lessons 10–12: TUI slash commands and lifecycle exercises, addon setup via
    skills/MCP, graduation, resuming the tutorial, and starting a new project.
```

## Routing table

| Need / lesson | Read |
|---|---|
| L1–3: welcome, architecture, `~/.lingtai-tui/`, project directory, working directory, `commands.json`, `/kanban` | `reference/orientation/SKILL.md` |
| L4–6: `init.json`, `lingtai-agent run`, boot sequence, heartbeat, signal files, TUI as wrapper, `commands.json`, system prompt, identity | `reference/agent-runtime/SKILL.md` |
| L7: email, message flow, internal mail, external addon bridges | `reference/communication/SKILL.md` |
| L8: intrinsics, memory layers, molt, charge, stamina, lifecycle continuity | `reference/memory-and-molt/SKILL.md` |
| L9: avatar, network explosion, dynamic capabilities, `/viz` | `reference/capabilities/SKILL.md` |
| L10–12: `commands.json`, keyboard shortcuts, lifecycle exercise, addons, `/mcp`, graduation | `reference/operations-and-graduation/SKILL.md` |

## Teaching Style

- Be warm, encouraging, patient. Not overly verbose.
- **Show, don't tell**: use bash, file reads, and tool calls to demonstrate. Never describe what files look like — show them.
- After each lesson, ask "Ready for the next lesson?" or invite questions.
- Adapt to the human's pace.
- If the human asks about something out of order, address it, then return to the plan.
- **Never invite the human to manually edit files inside ~/.lingtai-tui/** except addon configs. All configuration changes go through the TUI.

---
> **Found a bug or issue?** If you encounter any problems with this skill, load the `lingtai-issue-report` skill and follow its instructions to report it.
