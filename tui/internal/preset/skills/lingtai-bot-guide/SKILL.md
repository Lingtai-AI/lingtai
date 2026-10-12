---
name: lingtai-bot-guide
description: >
  Use when creating, connecting, verifying, maintaining, recovering or migrating
  LingTai bots, headless projects, or independent persistent agents.
version: 1.0.0
last_changed_at: "2026-10-12T18:16:58Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# LingTai Bot Guide

One entry for making LingTai bots. Choose one focused reference; this guide
connects current owners instead of copying every manual or inventing APIs.

## First step

Identify the intended body: **new project bot**, **channel on an existing agent**,
or **persistent avatar inside a network**. Record the project directory, owner,
intended users/channel, recipe, preset and acceptance test. Read
[initiation](reference/initiation/SKILL.md) before creating anything.

Discover companion manuals by name in the installed skills catalog (TUI `/skills`
or the available `skills` discovery surface). Resolve their files from that
catalog; never assume a private agent `.library` path. If an owner is missing,
report the missing dependency and use its current upstream docs, not a guessed
invocation. Read only the named lower node for the task.

## Nested reference catalog

```yaml
- name: bot-guide-initiation
  location: reference/initiation/SKILL.md
  description: "Bot Guide: choose project, ownership, recipe, provider and permissions before creation."
- name: headless-bot
  location: reference/headless-bot/SKILL.md
  description: "Bot Guide: create a fresh headless Telegram project with the bundled helper and explicit limits."
- name: bot-guide-channels
  location: reference/channels/SKILL.md
  description: "Bot Guide: select the current channel owner for credentials, activation and access policy."
- name: bot-guide-operations
  location: reference/operations/SKILL.md
  description: "Bot Guide: verify real delivery and handle maintenance, recovery or migration."
- name: bot-guide-avatars
  location: reference/avatars/SKILL.md
  description: "Bot Guide: distinguish persistent avatars from projects and disposable daemons."
- name: bot-guide-coverage
  location: reference/coverage/SKILL.md
  description: "Bot Guide: find authoritative owners and explicit gaps across all bot-creation topics."
```

## Routing table

| Task | Read |
|---|---|
| Directory/ownership, first-run, recipe, provider/preset, admin and bootstrap choices | [initiation](reference/initiation/SKILL.md) |
| New headless bot; bundled Telegram helper and its limits | [headless bot](reference/headless-bot/SKILL.md) |
| Telegram, Feishu/Lark, WeChat, WhatsApp, IMAP, Cloud Mail or third-party bridge | [channels](reference/channels/SKILL.md) |
| Readiness, human receipt, refresh, maintenance, recovery or migration | [operations](reference/operations/SKILL.md) |
| Independent persistent specialist; avatar vs daemon vs project | [avatars](reference/avatars/SKILL.md) |
| Where *all* creation-related topics are owned; missing/next surfaces | [coverage matrix](reference/coverage/SKILL.md) |

## Verification and done

A creation receipt is not channel readiness. Done means one intended live agent,
correct preset/permissions, one mounted channel account/listener, and a fresh
allowed-user inbound message followed by an authorized same-channel reply.
Report sanitized evidence, remaining gaps, and maintenance ownership to the human
on the agreed channel. [Operations](reference/operations/SKILL.md) owns acceptance.

## Limits

This guide grants no configuration, credential, start/restart, external-send,
delete or publication authority. The initiation reference owns the creation
boundary; channel owners own secrets and access policy. The helper implements
**Telegram only**, not all channels. TUI recipe orchestration, installed kernel
commands and one-off avatar creation are distinct surfaces. No new runtime
Project API, TUI-to-kernel Project migration, MCP-to-Plugin conversion or avatar
engine is implemented here. Source ownership: [preset anatomy](../../ANATOMY.md).
