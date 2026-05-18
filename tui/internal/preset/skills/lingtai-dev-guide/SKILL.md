---
name: lingtai-dev-guide
description: >
  Comprehensive developer guide for contributing to the LingTai project — the
  agent operating system. Covers the full landscape: three codebases (Python
  kernel, Go TUI/portal, Python MCPs), architecture overview, dev environment
  setup, contributing workflows, MCP development, skill authoring, progressive
  disclosure pattern, and release process. Read this when you are about to make
  code changes to any part of LingTai, set up a dev environment, understand how
  the pieces fit together, or develop a new MCP addon. Do NOT use for
  operational agent tasks (use lingtai-kernel-anatomy or lingtai-tui-anatomy
  instead) or for using LingTai as an end user (use the tutorial-guide skill).
version: 1.2.0
tags: [python, golang, typescript, agent, architecture, contributing, reference, mcp]
---

# LingTai Developer Guide

This skill is the single entry point for anyone developing on or contributing to LingTai. It uses **progressive disclosure** — start here for the big picture, then drill into specifics via the reference files and linked skills.

## Progressive Disclosure Pattern

| Level | What | When to use |
|---|---|---|
| **Level 1** | This guide — the 10,000-foot view | First time, orientation |
| **Level 2** | Reference files — deep dives on specific topics | When you need detail on one area |
| **Level 3** | Anatomy skills — navigate by structure | When you need to find specific code |
| **Level 4** | Code + tests — the ground truth | When you're making changes |

**Rule: never jump levels.** Read the guide first, then the reference, then the anatomy, then the code.

## Decision tree — where do I start?

| I want to... | Read this |
|---|---|
| Understand the project structure and how the pieces fit together | `reference/architecture.md` |
| Set up a local dev environment from scratch | `reference/setup.md` |
| Make changes to the TUI, portal, kernel, or addons | `reference/contributing.md` |
| Avoid known pitfalls and footguns | `reference/gotchas.md` |
| Ship a release | `reference/releasing.md` |
| Navigate the kernel source code by structure | `lingtai-kernel-anatomy` skill (separate skill) |
| Navigate the TUI/portal source code by structure | `lingtai-tui-anatomy` skill (separate skill) |
| Understand how agents work at runtime | `lingtai-kernel-anatomy` skill → descend from `src/lingtai_kernel/ANATOMY.md` |
| Set up or develop an MCP addon | `mcp-manual` skill → then `lingtai-kernel-anatomy reference/mcp-protocol.md` |
| Author or publish a skill | `library-manual` skill |
| Understand recipes and export networks | `lingtai-recipe` skill |

## Quick orientation

LingTai is an **agent operating system** — a minimal kernel that gives AI agents thinking (LLM), perceiving (vision, search), acting (file I/O), and communicating (inter-agent email). Everything else is plugged in from outside via MCP-compatible interfaces.

### The three codebases

| Repo | Language | What it ships | Key entry points |
|---|---|---|---|
| [`lingtai`](https://github.com/Lingtai-AI/lingtai) | Go + TypeScript | `lingtai-tui` (terminal UI) and `lingtai-portal` (web portal) | `tui/main.go`, `portal/main.go` |
| [`lingtai-kernel`](https://github.com/Lingtai-AI/lingtai-kernel) | Python | `lingtai` PyPI package (agent runtime, LLM interface, mailbox, tools) | `src/lingtai_kernel/ANATOMY.md` |
| MCP addons (×4) | Python | `lingtai-imap`, `lingtai-telegram`, `lingtai-feishu`, `lingtai-wechat` | Each repo's `README.md` |

### The agent model: kernel → agent → network

```
┌─────────────────────────────────────────────────────┐
│                    lingtai (Go)                      │
│                                                     │
│  ┌─────────────┐    ┌──────────────┐               │
│  │  lingtai-tui │    │ lingtai-portal│              │
│  │  (terminal)  │    │  (web)       │               │
│  └──────┬───────┘    └──────┬───────┘               │
│         └─────────┬─────────┘                        │
│                   │                                  │
│          Filesystem only                             │
│          (.lingtai/<agent>/)                         │
│                   │                                  │
└───────────────────┼──────────────────────────────────┘
                    │
┌───────────────────┼──────────────────────────────────┐
│          lingtai-kernel (Python)                      │
│                   │                                  │
│  ┌────────────────┴────────────────┐                 │
│  │         Agent runtime           │                 │
│  │  turn loop · tools · mailbox    │                 │
│  │  soul · molt · notifications    │                 │
│  └────────────────┬────────────────┘                 │
│                   │                                  │
│  ┌────────────────┴────────────────┐                 │
│  │         MCP tools               │                 │
│  │  imap · telegram · feishu · wechat                │
│  └─────────────────────────────────┘                 │
└──────────────────────────────────────────────────────┘
```

**Key architectural decisions:**

1. **Filesystem-only IPC.** The TUI/portal never open a socket to a running agent. All communication is through files: manifests, heartbeats, signal files, mailbox folders, `.notification/`.
2. **Kernel is standalone.** `lingtai_kernel` never imports from the wrapper `lingtai`. The wrapper depends on the kernel one-directionally.
3. **MCP is the extension point.** Domain tools are plugged in via MCP servers. The 4 first-party addons (imap, telegram, feishu, wechat) each ship as a standalone PyPI package with a LICC v1 inbox callback.

### Key concepts

| Concept | What it is | Where to learn more |
|---|---|---|
| **Avatar** (他我) | An independent agent process spawned by another agent | Covenant §I |
| **Molt** (凝蜕) | Context shedding — crystallize worth, shed ephemera | Covenant §V, `procedures.md` |
| **Lingtai** (灵台) | Your character/identity — persists across molts | `psyche` tool, `lingtai-kernel-anatomy` |
| **Pad** | Working sketchboard — living index of current work | `psyche` tool, `procedures.md` |
| **Codex** | Durable self-memory — verifiable truths | `codex` tool |
| **Library** | Skill catalog — reusable procedures | `library-manual` skill |
| **Preset** | Atomic `{llm, capabilities}` bundle | `tui/internal/preset/`, `lingtai-tui-anatomy` |
| **LICC** | LingTai Inbox Callback Contract — MCP→agent event delivery | `lingtai-kernel-anatomy reference/mcp-protocol.md` |

### The utility layer: skills

Skills are reusable procedures, workflows, and reference material that agents load on-demand. They use **progressive disclosure** — a routing hub (`SKILL.md`) with reference files loaded only when needed.

| Location | Who owns it | Editable? |
|---|---|---|
| `<agent>/.library/intrinsic/` | CLI-managed. Wiped and rewritten on every refresh. | No |
| `<agent>/.library/custom/` | You. CLI never touches this. | Yes |
| `../.library_shared/` | Network-shared. Add with `cp -r`. | Admin only |
| `~/.lingtai-tui/utilities/` | TUI-shipped utilities. | Depends on the skill |

To author a new skill: read the `library-manual` skill for the full workflow (frontmatter schema, template, validator, publishing). To publish to the shared library: `cp -r .library/custom/<name> ../.library_shared/<name>` then `system(action='refresh')`.

## MCP addon development

The 4 first-party MCP addons are:

| Addon | Repo | What it does |
|---|---|---|
| `lingtai-imap` | [GitHub](https://github.com/Lingtai-AI/lingtai-imap) | IMAP/SMTP email integration |
| `lingtai-telegram` | [GitHub](https://github.com/Lingtai-AI/lingtai-telegram) | Telegram Bot API messaging |
| `lingtai-feishu` | [GitHub](https://github.com/Lingtai-AI/lingtai-feishu) | Feishu/Lark messaging |
| `lingtai-wechat` | [GitHub](https://github.com/Lingtai-AI/lingtai-wechat) | WeChat (iLink) messaging |

**To develop a new MCP addon:**

1. Read the `mcp-manual` skill for the registration workflow
2. Read `lingtai-kernel-anatomy reference/mcp-protocol.md` for the LICC v1 protocol spec
3. Each addon is a standalone Python package with its own `README.md` — fetch it via `find_readme.py <pkg-name>`
4. Key contract: the MCP server must implement the LICC v1 inbox callback for event delivery to agents
5. Register via `init.json` `mcp.<name>` entries

## Contributing workflow

1. **Orchestrator + daemons, not hand-coding.** For any non-trivial coding, research, or change task, the orchestrator's job is to *plan, dispatch, and review* — not to hand-code. Decompose the work into daemon-sized tasks and dispatch them to Claude Code / Codex daemon backends, which are the right tools for code reading, modification, testing, refactoring, PR preparation, batch scanning, and mechanical validation. Use as much safe parallelism as the decomposition allows: independent daemons run concurrently in their own worktrees/branches, each with a scoped brief and a do-not-touch list.
2. **Portfolio sweep before broad planning.** Before planning any broad LingTai dev work, run (or dispatch) a read-only org-wide issues/PRs scan via the `lingtai-repo-watch` skill or a `gh` org sweep, and let the current PR/issue surface guide what to pick up. Summarize stale, unreviewed, and relevant items.
3. **Issue → worktree/branch → PR → merge.** Non-trivial changes are tracked end-to-end: open or pick an issue, work in an isolated worktree on a topic branch, push, open a PR, review, merge. No long-lived ad-hoc branches; no edits in the main checkout.
4. **Anatomy updates are mandatory.** If your change moves, renames, splits, merges, or deletes a file/function/class cited by an `ANATOMY.md`, update the anatomy in the **same commit**. See `lingtai-kernel-anatomy` (Python) or `lingtai-tui-anatomy` (Go) for the full convention.
5. **Three-locale rule.** Adding an i18n key means updating all three of `en.json`, `zh.json`, `wen.json` in both `tui/i18n/` and (where applicable) `portal/i18n/`.
6. **Filesystem-only IPC.** Any new cross-process communication must follow the file-based pattern.
7. **Skill authoring for reusable procedures.** If your change creates a reusable workflow, write it as a skill.

For the full contributing guide (orchestrator/daemon discipline, portfolio sweep, build commands, gotchas, anatomy maintenance, migration contract): read `reference/contributing.md`.

## Reference files

| File | What it covers |
|---|---|
| `reference/architecture.md` | Two repos, components, IPC, state layout, cross-repo dependencies |
| `reference/setup.md` | Dev environment prerequisites, cloning, building, dev mode, editable installs |
| `reference/contributing.md` | How to change TUI, portal, kernel, addons, skills; anatomy maintenance |
| `reference/gotchas.md` | Known pitfalls (Bubble Tea paste, migrations, auto-upgrader, three-locale rule) |
| `reference/releasing.md` | Release process for TUI/portal and kernel |

## Related skills

- **`lingtai-kernel-anatomy`** — the convention for `ANATOMY.md` files in the kernel. Read this when navigating kernel source.
- **`lingtai-tui-anatomy`** — the convention for `ANATOMY.md` files in the Go monorepo. Read this when navigating TUI/portal source.
- **`library-manual`** — how the skill library works. Read this when authoring or publishing skills.
- **`mcp-manual`** — how MCP servers are registered and activated. Read this when working on addons.
- **`lingtai-recipe`** — recipe authoring and network export. Read this when packaging or sharing methodologies.
- **`tutorial-guide`** — the 12-lesson curriculum for end users. Not for developers.
