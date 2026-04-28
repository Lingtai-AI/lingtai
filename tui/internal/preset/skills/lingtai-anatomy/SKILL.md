---
name: lingtai-anatomy
description: >
  Reference for how an agent's memory, filesystem, and runtime are organized.
  Modular multi-layer skill: this index file points to 8 topical references.
  Load only the reference you need — not the whole skill.
  Use when investigating LingTai mechanics, debugging an agent, or building
  tools that interact with .lingtai/ directories. Key topics: agent runtime,
  five-state machine, molt protocol, avatar tree, mailbox, durability hierarchy.
version: 2.0.0
---

# LingTai Anatomy

Three-layer architecture, eight topical references. Read what you need; skip the rest.

## Architecture at a Glance

```
lingtai_kernel (pip package)
  └── BaseAgent (~1.7K lines) — turn loop, five-state machine, molt, soul, signals
lingtai (wrapper)
  └── Agent (~860 lines) — capabilities, MCP, refresh, CPR
User customization
  └── init.json + system/ files — model, prompts, capabilities, addons
```

Source code:
- `lingtai-kernel/src/lingtai_kernel/base_agent.py`
- `lingtai/src/lingtai/agent.py`
- `lingtai/src/lingtai/network.py`

## Quick Reference: Where to Look

| "I want to understand…" | Read this reference | Key content |
|---|---|---|
| How memory persists across molts | `reference/memory-system.md` | 6-layer durability hierarchy, psyche tool dispatch, daemon system |
| What files live where on disk | `reference/filesystem-layout.md` | Directory trees, orchestrator identification, boot chain |
| Exact JSON schemas of key files | `reference/file-formats.md` | .agent.json, init.json, .status.json, mailbox, MCP, signals |
| How each turn runs | `reference/runtime-loop.md` | Turn cycle, five-state machine, signal lifecycle, soul flow |
| How molting works | `reference/molt-protocol.md` | Triggers, warning ladder (70%/95%), four-store ritual, refresh |
| How mail gets delivered | `reference/mail-protocol.md` | Atomic delivery, advanced features, self-send, wake-on-mail |
| How the avatar tree works | `reference/network-topology.md` | Spawn mechanics, three-edge model, contacts, rules propagation |
| What a 文言 term means in English | `reference/glossary.md` | Full bilingual term map (kernel layer + wrapper tool name) |

## Version History

- **v2.0.0** (2026-04): Modular rewrite. 8 independent references replace the monolithic 474-line file. 4 errata corrected, 7 missing topics covered.
- **v1.2.0**: Original monolithic SKILL.md (474 lines / 31KB / ~8K tokens).
