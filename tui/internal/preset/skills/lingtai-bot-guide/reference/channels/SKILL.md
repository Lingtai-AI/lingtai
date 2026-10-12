---
name: bot-guide-channels
description: >
  Use when selecting or setting up a LingTai messaging bridge and locating its
  exact current configuration and readiness documentation.
version: 1.0.0
last_changed_at: "2026-10-12T18:16:58Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# Channels: one mind, explicit bridges

Nested lingtai-bot-guide reference. Multiple channels can reach the same agent;
a new channel is not automatically a new bot/process or a privileged command
interface. `/refresh` sent as chat text does not bypass authorization.

## Load the exact owner before editing

Discover `mcp-manual` → `reference/curated-addons.md` in the installed skills
catalog. It owns the four-step curated setup, registry/module mapping, active
activation spec, agent-relative config paths, credentials, and acceptance checks.
Read the selected upstream README when provider details are needed:

| Supported curated bridge | Exact upstream documentation route |
|---|---|
| Telegram | [lingtai-telegram README](https://github.com/Lingtai-AI/lingtai-telegram/blob/main/README.md) |
| Feishu / Lark | [lingtai-feishu README](https://github.com/Lingtai-AI/lingtai-feishu/blob/main/README.md) |
| WeChat | [lingtai-wechat README](https://github.com/Lingtai-AI/lingtai-wechat/blob/main/README.md); curated owner also holds bootstrap/session-expiry checklist |
| WhatsApp | [lingtai-whatsapp README](https://github.com/Lingtai-AI/lingtai-whatsapp/blob/main/README.md) |
| IMAP / SMTP email | [lingtai-imap README](https://github.com/Lingtai-AI/lingtai-imap/blob/main/README.md) |
| Cloud Mail | [cloud-mail README](https://github.com/maillab/cloud-mail/blob/main/README.md); LingTai client fields/action limits belong to `curated-addons.md` and the mounted `cloud_mail` manual |

These homepage names are provenance/docs, **not** a recommendation to install
old standalone wheels. Current curated servers ship in the kernel distribution
as `lingtai.mcp_servers.*`. Check the installed owner's catalog if support or
module mapping differs; upstream main docs alone are not installed-version proof.
No channel secret/auth schemas are copied into this reference.

For another service, discover `mcp-manual` →
`reference/third-party-and-legacy.md` and that server's README/homepage. Do not
claim an unlisted channel is curated or that the Telegram helper implements it.
For protocol/LICC questions use `lingtai-kernel-anatomy` →
`reference/mcp-protocol.md`, not a parallel setup recipe.

## Setup sequence

1. Choose the existing/new target agent and approved account/audience.
2. Follow the curated owner (or third-party owner) for exact sidecar/registration
   and activation changes. Merge intended entries, preserving unrelated config.
   Keep secrets outside Git, argv, logs, screenshots and handoff artifacts;
   maintain restrictive file/directory permissions and provider-specific access
   restrictions. Account login/QR/auth interactions require human participation.
3. Follow `system-manual` → `reference/refresh-precheck/SKILL.md` for one controlled
   lifecycle transition. `/mcp` is **read-only inspection**, not setup; `/addon`
   is retired. Registration is not activation and neither is end-to-end delivery.
4. Verify using [operations](../operations/SKILL.md) and the selected channel's
   mounted manual. Inbound and outbound must both work for the approved audience.

For a fresh Telegram project only, [headless bot](../headless-bot/SKILL.md) owns
the bundled helper. The generic guide routes other channels to their owners.
