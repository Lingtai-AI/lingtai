---
name: tutorial-guide-communication
description: >
  Nested tutorial-guide reference for lesson 7: filesystem email, message flow, and external addon bridges.
version: 1.0.0
---

# Tutorial Guide — Communication Lesson

Nested tutorial-guide reference for communication lesson 7.

Use this file after the root `tutorial-guide` router sends you here. Keep teaching live: discover current files, commands, and runtime state before explaining them.

## Lesson 7: Communication — Email

- Explain the design philosophy: text input/output are reserved for the agent's internal processing. Humans communicate only via email. This gives agents dignity and private space.
- Walk through the message flow: human types → TUI writes to inbox → agent wakes → agent reads → agent replies → reply lands in human's inbox → TUI displays it.
- Show a raw message.json from your inbox.
- Explain the difference between internal mail (filesystem-based, within .lingtai/) and external bridges (IMAP, Telegram, Feishu, etc. via addons).
