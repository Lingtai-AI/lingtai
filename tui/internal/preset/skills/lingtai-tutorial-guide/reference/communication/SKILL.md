---
name: tutorial-guide-communication
description: >
  Nested tutorial-guide reference for lesson 7: filesystem email, message flow, and external addon bridges.
version: 1.0.0
---

# Tutorial Guide — Communication Lesson

Nested tutorial-guide reference for communication lesson 7.

Use this file after the root `tutorial-guide` router sends you here. Keep teaching live: discover current files, commands, and runtime state before explaining them.

## Lesson 7: Communication — Email and external bridges

- Explain the design philosophy: text input/output are reserved for the agent's internal processing. Humans communicate only via email/chat channels. This gives agents dignity and private space.
- Walk through the internal message flow: human types in the TUI → TUI writes to the human/agent filesystem mailbox → agent wakes → agent reads → agent replies → reply lands in the human's inbox → TUI displays it.
- Show a raw `message.json` from an inbox so the human can see that internal mail is just local filesystem state under `.lingtai/`.
- Explain the difference between internal mail (filesystem-based, within `.lingtai/`) and external bridges (IMAP, Telegram, Feishu, WeChat, etc. via MCP addons). External bridges translate outside-platform events into the same agent-facing mailbox/notification pattern; they are not a separate mind or a privileged command channel.
- Teach the decentralized contact-book rule: `lingtai-tui list --detailed` / `--admin` can identify running main agents from live local `.agent.json` state, but LingTai does not keep one global IM-handle database. Agents that want humans or peers to reach them through Telegram/Feishu/WeChat/etc. should publish and maintain those handles in their own durable profile, project skill, pad, or other public project context.

## LICC bridge mental model

Teach LICC (LingTai Inbox Callback Contract) as the small contract that lets an MCP bridge hand a human message to the kernel and wake the right agent. A Telegram example is easiest:

```text
Telegram user message
  → Telegram Bot API
  → lingtai-telegram MCP bridge
  → LICC inbox event
  → LingTai kernel writes/wakes the agent mailbox
  → agent reads the message and replies with the Telegram tool
  → Telegram Bot API delivers the reply
```

The important user-facing point is that Telegram/Feishu/IMAP/WeChat messages become ordinary agent work: the agent reads them, reasons about them, uses its tools, and replies on the same channel. LICC is the delivery bridge; it does not automatically execute kernel lifecycle actions and does not make slash commands magical by itself.

## Agent-level custom commands

External chat platforms often make messages that begin with `/` look like bot commands. In LingTai, an agent can still treat `/poem`, `/status`, `/report`, or any other agreed string as a **conversation convention**:

- The platform sends the text to the agent through the bridge.
- The agent recognizes the convention in its normal reasoning.
- The agent chooses the appropriate tool/workflow and replies on the channel.

This means a persona or project can grow its own lightweight commands without platform-native command registration. For example, a poetry agent could agree that `/poem` means “compose and publish today's poem” and `/status` means “summarize the poetry library and key health.” Those commands are agent behavior, not Telegram-native behavior.

Be explicit about the boundary: a human sending `/refresh` in Telegram is still just sending text to the agent. It does **not** bypass permissions or directly call `system(action='refresh')`; the agent may decide to refresh only if it has the capability and the instruction is authorized.

## Teaching checklist

- Show one internal mail message and one external bridge message if available.
- Ask the human to propose one harmless custom command for the current agent.
- Have the agent explain, in-channel, what it will treat that command as meaning.
- Remind the human that command conventions should be documented in the agent's pad/lingtai or a project skill if they should survive molt and collaboration.
