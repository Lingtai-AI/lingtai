---
name: bot-guide-coverage
description: >
  Use when finding the authoritative owner for any LingTai bot-creation topic
  or checking the guide’s coverage and known implementation gaps.
version: 1.0.0
last_changed_at: "2026-10-12T18:16:58Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# Coverage matrix and source ownership

Nested lingtai-bot-guide reference. “All related information” means one easy
entry to every current owner, **not** copied full manuals or one universal helper.
Names below resolve through the installed skills catalog. Read the selected
owner's current node; version differences and missing nodes are explicit gaps.

| Creation topic | Authoritative current owner + exact pointer | Bot-guide entry |
|---|---|---|
| Directory, new-vs-existing, interactive/headless creation | `lingtai-tui-help` plus [headless anatomy](../../../../../headless/ANATOMY.md) (`RunSpawn`) and [process anatomy](../../../../../process/ANATOMY.md) (`CreateProject`) | [initiation](../initiation/SKILL.md) |
| Kernel-only create/run/schema, no TUI | Installed kernel CLI help; `system-manual` → `reference/substrate-manual/SKILL.md` §11; [kernel README](https://github.com/Lingtai-AI/lingtai-kernel/blob/main/README.md) | initiation |
| Recipe author/import/export/library and snapshot distinction | `lingtai-recipe` → `reference/recipe-format/SKILL.md`, `reference/export-recipe/SKILL.md`; [preset anatomy](../../../../ANATOMY.md) (`recipe_apply.go`, `recipes.go`, `state.go`) | initiation |
| Provider/model/endpoint/auth choice | `lingtai-preset-skill` → `reference/{codex,claude,openai,anthropic}/SKILL.md`; provider child owns login/secret fields | initiation |
| Template/saved preset storage and allowed policy | `lingtai-preset-skill` → `reference/operations/saved-presets/SKILL.md`; `system-manual` → substrate §11 | initiation / headless bot |
| Endpoint/capability evidence and save gate | `lingtai-preset-skill` → `reference/operations/endpoint-capabilities/SKILL.md`, `reference/operations/availability-save-gate/SKILL.md` | initiation |
| Admin/orchestrator/member ownership | [TUI anatomy](../../../../../../ANATOMY.md) and `system-manual` → substrate §11; current creation/schema owner, not manual `.agent.json` writes | initiation |
| Installation, bootstrap and service supervision | `lingtai-update` → `reference/install/SKILL.md`; `system-manual` → `reference/runtime-update-checks/SKILL.md`; `shell-manual` scheduled-work node | initiation |
| Telegram helper and actual scope | [headless bot](../headless-bot/SKILL.md), which owns the bundled Telegram helper; [preset anatomy](../../../../ANATOMY.md) embeds/materializes it | headless bot |
| Telegram, Feishu, WeChat, WhatsApp, IMAP, Cloud Mail | `mcp-manual` → `reference/curated-addons.md` plus [exact upstream README routes](../channels/SKILL.md) and mounted channel manual | channels |
| Other third-party channels/services | `mcp-manual` → `reference/third-party-and-legacy.md` and that server's registry homepage/README | channels |
| Secret privacy, account login, allowlists, expiry/re-auth | Selected curated/provider README and mounted manual; `curated-addons.md` WeChat checklist and Telegram readiness/security; no schema copied here | channels / headless bot |
| Catalog ≠ registered ≠ active; LICC vs tool calls | `mcp-manual` router and `reference/curated-addons.md`; `lingtai-kernel-anatomy` → `reference/mcp-protocol.md` | channels / operations |
| End-to-end allowed-user inbound/reply and human receipt | Selected channel manual/readiness owner; bot-level acceptance sequencing in [operations](../operations/SKILL.md) | operations |
| Refresh, preset activation, interruption safety | `system-manual` → `reference/refresh-precheck/SKILL.md`; `lingtai-preset-skill` → `reference/operations/activation-session-refresh/SKILL.md` | operations |
| Binary/kernel versions, source/venv drift, MCP child provenance | `lingtai-update` → detection/diagnosis/update-tui nodes; `system-manual` → runtime-update-checks; `mcp-manual` curated child-runtime/venv-swap sections | operations |
| Recovery, partial creation, duplicate start, migration | headless/process anatomies; `system-manual` → substrate/refresh-precheck; `lingtai-preset-skill` → `reference/operations/troubleshooting-migration/SKILL.md` | operations |
| Independent avatar vs daemon, parent/child reporting | `avatar-manual`, `daemon-manual`; `system-manual` → substrate §1 | avatars |
| Detached async/scheduled hazards | `shell-manual` root async-supervision rules and its `reference/scheduled-work/SKILL.md` | operations |
| Durable behavior, identity, memory and reusable skills | `psyche-manual`, `context-manual`, `knowledge-manual`, `skills-manual`; `lingtai-dev-guide` → `reference/skill-stewardship/SKILL.md` | operations |

## Gaps and explicit next work

- The helper is Telegram-only, fresh-project-only and nontransactional. No other
  channel automation or universal dry-run/rollback is claimed.
- TUI recipes remain TUI orchestration; installed kernel creation is a separate
  CLI/schema boundary; avatars are individual agents, not project recipes.
  Moving the Project concept from TUI to kernel needs a **next audit**, not a
  fabricated runtime API in documentation.
- Shell `avatar new` is an unmerged/not-live cross-repository candidate. Current
  installed avatar guidance is the usable owner until a replacement ships.
- MCP consolidation/Plugin changes and avatar-engine implementation are deferred.
  This guide documents current MCP ownership, not the future design.
- Missing installed owner, unavailable access-denial test or version mismatch is
  reported as a gap; do not silently fill it with remembered private paths or
  presumed schemas. Website tutorial alignment is maintained separately.

Source/materialization owner: [preset anatomy](../../../../ANATOMY.md).
