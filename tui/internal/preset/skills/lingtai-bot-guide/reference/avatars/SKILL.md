---
name: bot-guide-avatars
description: >
  Use when deciding whether a bot should be a new project, persistent avatar
  or ephemeral daemon and locating the current avatar creation owner.
version: 1.0.0
last_changed_at: "2026-10-12T18:16:58Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# Independent avatars are not headless project bots

Nested lingtai-bot-guide reference. A bot can be a durable specialist, but body
selection and channel selection are different decisions.

- **Project bot:** its own project/orchestrator and maintenance owner. Use
  [initiation](../initiation/SKILL.md) and the supported project creation route.
- **Avatar:** independent persistent agent process inside the network, with its
  own working directory/history/memory and parent relationship. Discover
  `avatar-manual` for the currently mounted tool and its exact spawn contract;
  discover `system-manual` → `reference/substrate-manual/SKILL.md` §1 for body
  selection. Do not create an avatar for a tiny command or one-off test.
- **Daemon:** bounded ephemeral work where only conclusion/artifact is needed.
  Discover `daemon-manual` for completion/checkpoint and async ownership. A daemon
  is not a replacement for an always-on bot with persistent relationships.

## Current route and candidate boundary

Read the available `avatar-manual` and live schema before creating an avatar.
The currently documented tool route is `avatar` spawn; this guide does not copy
its flags or inheritance algorithm. The cross-repository shell `avatar new`
replacement is a **candidate, NOT MERGED / NOT LIVE** for this guide's baseline.
No invocation is prescribed here. If a future installed catalog advertises the
replacement, read that version's manual and help first; do not infer support
from a proposal, source branch or a command remembered from another runtime.

## Before and after authorized creation

Give a specific mission, allowed resources, parent contact, maintenance owner,
acceptance evidence and limits. Preview using the owner's supported mechanism
where available; creation consumes resources and is not an inspection call.
Keep the receipt/address/PID and verify the intended independent process.

Do not assume active-preset, admin, addon/channel secrets, comment or all other
parent fields are inherited. In particular avatars do not automatically inherit
external addons: an independent channel needs separately authorized setup through
[channels](../channels/SKILL.md). Sharing one Telegram long-poll token between
independent listeners can contend; assign ownership rather than starting copies.

Record the relationship/mission durably, and require completion/blocker reports
to the parent. For quiet avatars follow `avatar-manual` escalation, not repeated
probe mail or parallel CPR. A persistent avatar remains a life/resource owner
until an explicit lifecycle decision, not an async job that ends when a caller
returns. [Operations](../operations/SKILL.md) owns bot-level verification.
