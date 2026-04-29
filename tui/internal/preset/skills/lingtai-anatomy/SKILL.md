---
name: lingtai-anatomy
description: >
  Canonical reference for how LingTai is built — kernel, capabilities, MCP / LICC,
  filesystem, runtime, molt, network, and the breaking-changes log.
  Modular multi-layer skill: this index file points to 10 topical references.
  Load only the reference you need — not the whole skill.
  Use when investigating LingTai mechanics, debugging an agent, building tools
  that interact with .lingtai/ directories, writing a third-party MCP server,
  or chasing down "what does this old name refer to" via the changelog.
version: 2.1.0
---

# LingTai Anatomy

Canonical architecture documentation. Three-layer system, ten topical references. Read what you need; skip the rest.

## Architecture at a Glance

```
lingtai_kernel (pip package)
  └── BaseAgent (~1.7K lines) — turn loop, five-state machine, molt, soul, signals
lingtai (wrapper)
  └── Agent (~860 lines) — capabilities, MCP catalog/registry/loader, LICC, refresh, CPR
User customization
  └── init.json + system/ files — model, prompts, capabilities, addons (curated MCPs)
First-party MCP repos (separate)
  └── lingtai-imap / lingtai-telegram / lingtai-feishu / lingtai-wechat
      — addon protocol implementations, run as MCP subprocesses
```

Source code:
- `lingtai-kernel/src/lingtai_kernel/base_agent.py`
- `lingtai/src/lingtai/agent.py`
- `lingtai/src/lingtai/network.py`
- `lingtai/src/lingtai/core/mcp/` (capability + LICC inbox poller + catalog)

## Quick Reference: Where to Look

| "I want to understand…" | Read this reference | Key content |
|---|---|---|
| How memory persists across molts | `reference/memory-system.md` | 6-layer durability hierarchy, psyche tool dispatch, daemon system |
| What files live where on disk | `reference/filesystem-layout.md` | Directory trees, orchestrator identification, boot chain |
| Exact JSON schemas of key files | `reference/file-formats.md` | .agent.json, init.json, .status.json, mailbox, MCP registry / inbox, signals |
| How each turn runs | `reference/runtime-loop.md` | Turn cycle, five-state machine, signal lifecycle, soul flow |
| How molting works | `reference/molt-protocol.md` | Triggers, warning ladder (70%/95%), four-store ritual, refresh |
| How mail gets delivered | `reference/mail-protocol.md` | Atomic delivery, advanced features, self-send, wake-on-mail |
| How the avatar tree works | `reference/network-topology.md` | Spawn mechanics, three-edge model, contacts, rules propagation |
| **How MCP / LICC works** (or how to write a third-party MCP) | `reference/mcp-protocol.md` | Catalog → registry → activation, env injection, LICC v1 spec, reference impls |
| What changed and when (breaking changes, renames, migrations) | `reference/changelog.md` | Living chronicle, newest-first; load when an old name doesn't match current tools |
| What a 文言 term means in English | `reference/glossary.md` | Full bilingual term map (kernel layer + wrapper tool name) |

## Version History

- **v2.1.0** (2026-04-29): Added `mcp-protocol.md` (canonical MCP capability + LICC v1 spec). Absorbed standalone `lingtai-changelog` skill into `reference/changelog.md`. Stale references updated: `file-formats.md` §2.7 / §6 rewritten + new §6.5 (registry) + §6.6 (LICC events); `filesystem-layout.md` drops legacy `.lingtai/.addons/`, adds per-agent `mcp_registry.jsonl` + `.mcp_inbox/`; `molt-protocol.md` MCP persistence row updated.
- **v2.0.0** (2026-04): Modular rewrite. 8 independent references replace the monolithic 474-line file. 4 errata corrected, 7 missing topics covered.
- **v1.2.0**: Original monolithic SKILL.md (474 lines / 31KB / ~8K tokens).
