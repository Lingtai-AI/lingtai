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

## Human-facing reports default to self-contained HTML

When you finish a non-trivial piece of development or research work and want to hand the result to a human (PR summary attachment, design review, investigation write-up, postmortem, multi-PR status roll-up), **default to producing a self-contained HTML artifact**. Plain text / Markdown is reserved for the *short summary* that points at the HTML (e.g. one paragraph in an email or chat message with the file path).

**Why this is the default:**

1. Humans read HTML in a browser without a build step. No `pandoc`, no markdown viewer, no IDE-required.
2. A single `.html` file with inline CSS is the most archivable, offline-friendly format we have — it survives being moved, emailed, dropped into a worktree, or opened years later.
3. Information density is higher: tables, status pills, diff blocks, ASCII diagrams, side-by-side cards. The same content as Markdown is harder to scan.
4. It enforces a discipline: if you can't fit the work in a polished HTML, it probably isn't ready to hand off.

**Hard rules — the artifact must be:**

- **Self-contained.** All CSS in a single `<style>` block. No `<script src=...>`, no `<link rel="stylesheet">`, no remote fonts/images. No CDN, no network fetches at render time.
- **Offline-readable.** Open it from `file://` and it must look right. No build step, no server.
- **Inline assets only.** If you must embed an image, base64 it. Prefer ASCII/Unicode diagrams inside `<pre>` — they diff well and never break.
- **Readable in a real browser.** Use a saturated dark theme by default (we live in dark terminals) but keep contrast high. Responsive via `@media (max-width: ...)`.
- **Language: Chinese primary for LingTai project reports** (matches the workspace convention); technical identifiers and code stay in English.

**What every human-facing report HTML should include:**

| Section | Purpose |
|---|---|
| Header + meta pills | Date, author, PR numbers + status, scope |
| TL;DR / 一页结论 | Cards or stats so the reader can stop after 30 seconds and still know what happened |
| Context / baseline | What the system looks like *before* this work — diagrams welcome |
| What was done | Concrete diff-style snippets, file paths, PR + commit links |
| Validation | Test commands run + output; `git diff --check`; smoke imports |
| Risks / decisions / rejected paths | Why this shape and not the obvious alternative |
| Next steps | Follow-up PRs / phases / owners |
| Source index | Links to underlying reports, PRs, commits, source file:line anchors |

**Where to put the file:** under the agent's working directory, in a `reports/` subdirectory, named `<topic>-<date>.html`. For LingTai dev workspaces this is typically `<workdir>/reports/`. Reference it by absolute path in the short text summary you send to the human.

**When to drop down to plain text:** one-liner status, command outputs the user explicitly asked to see verbatim, short Q&A. Anything multi-section or that you'd otherwise reach for Markdown headings for → HTML.

**Reference implementation:** `reports/im-optimization-summary-2026-05-18.html` in the codex-gpt5.5 workspace is a canonical example (executive summary, baseline diagram, PR deep-dive, comparison matrix, risk table, acceptance checklist, source index — all in one self-contained file). Use it as a template.

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

1. **Fork → branch → PR.** All changes go through GitHub PRs.
2. **Anatomy updates are mandatory.** If your change moves, renames, splits, merges, or deletes a file/function/class cited by an `ANATOMY.md`, update the anatomy in the **same commit**. See `lingtai-kernel-anatomy` (Python) or `lingtai-tui-anatomy` (Go) for the full convention.
3. **Three-locale rule.** Adding an i18n key means updating all three of `en.json`, `zh.json`, `wen.json` in both `tui/i18n/` and (where applicable) `portal/i18n/`.
4. **Filesystem-only IPC.** Any new cross-process communication must follow the file-based pattern.
5. **Skill authoring for reusable procedures.** If your change creates a reusable workflow, write it as a skill.

For the full contributing guide (build commands, gotchas, anatomy maintenance, migration contract): read `reference/contributing.md`.

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
