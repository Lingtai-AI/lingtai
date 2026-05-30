<div align="center">

<img src="docs/assets/network-demo.gif" alt="LingTai agent network growing — one soul spawning avatars that communicate and multiply" width="100%">

# LingTai

**A filesystem-native operating system for long-lived AI agents.**
**One mind can become a network. The network learns while it works.**

[English](README.md) · [中文](README.zh.md) · [文言](README.wen.md) · [Website](https://lingtai.ai) · [Releases](https://lingtai.ai/releases/)

[![Homebrew](https://img.shields.io/badge/brew-lingtai--tui-%237dab8f)](https://github.com/Lingtai-AI/homebrew-lingtai)
[![License](https://img.shields.io/github/license/Lingtai-AI/lingtai?color=%237dab8f)](LICENSE)
[![Kernel](https://img.shields.io/badge/kernel-lingtai--kernel-%237dab8f)](https://github.com/Lingtai-AI/lingtai-kernel)
[![Site](https://img.shields.io/badge/site-lingtai.ai-%23d4a853)](https://lingtai.ai)
[![Discord](https://img.shields.io/badge/discord-join-%235865F2?logo=discord&logoColor=white)](https://discord.gg/cMchjXpg)

</div>

---

LingTai is an agent OS: a local runtime, terminal UI, visual portal, mailbox, memory system, skill system, and multi-agent network substrate for autonomous AI agents that persist beyond a single chat window.

It is not a prompt wrapper, workflow graph, or one-shot coding assistant. A LingTai agent is a running process with a filesystem home, durable memory, tools, mail, long-lived identity, and the ability to spawn peers. Agents can sleep, wake, molt their context, write reusable skills, remember project facts, send each other mail, connect to external messaging systems, and continue working after the terminal closes.

The core design is simple:

> **Agent = process + directory + mailbox + memory + tools.**
> **Network = agents that can create, teach, and call one another.**

## Table of contents

- [Why LingTai exists](#why-lingtai-exists)
- [Quick start](#quick-start)
- [What you get after first launch](#what-you-get-after-first-launch)
- [Core concepts](#core-concepts)
- [What LingTai can do](#what-lingtai-can-do)
- [Filesystem model](#filesystem-model)
- [User interface: TUI, slash commands, and portal](#user-interface-tui-slash-commands-and-portal)
- [External channels and MCP addons](#external-channels-and-mcp-addons)
- [Recipes, skills, and knowledge](#recipes-skills-and-knowledge)
- [Architecture](#architecture)
- [Installation and upgrade details](#installation-and-upgrade-details)
- [Development workflow](#development-workflow)
- [Repository map](#repository-map)
- [Troubleshooting](#troubleshooting)
- [Contributing](#contributing)
- [Project philosophy](#project-philosophy)
- [Community](#community)
- [Star history](#star-history)
- [License](#license)

## Why LingTai exists

Most agent tools still behave like chat sessions:

- the model has no durable body;
- long context eventually collapses;
- tools are configured outside the agent's own world;
- memory is either hidden vendor state or an opaque vector store;
- multi-agent work is simulated by a controller, not lived by independent agents;
- when the UI closes, the system stops feeling alive.

LingTai takes a different route. It makes agents concrete.

Each agent owns a directory under `.lingtai/`. That directory contains its identity, prompt layers, mail, logs, memory, skills, tools, runtime state, and summaries. The filesystem is not an implementation detail; it is the API. Humans, coding agents, scripts, and other LingTai agents can inspect and cooperate with the system through files and mail.

The result is an agent network that can grow:

- a primary agent can spawn an avatar to specialize in a task;
- that avatar can keep its own long-term memory;
- completed work can become a skill;
- hard-won project facts can become knowledge;
- the network can be visualized, restarted, debugged, and resumed.

## Quick start

### macOS / Homebrew

```bash
brew install lingtai-ai/lingtai/lingtai-tui
lingtai-tui
```

On first launch, the TUI guides you through setup: model/provider selection, project initialization, recipe selection, and agent creation.

The TUI manages the Python agent runtime for you in a project virtualenv. You normally do **not** install or upgrade the kernel with a bare `pip install`; the TUI creates and maintains the runtime venv used by each project.

### From source

Use this when hacking on the TUI/portal itself or when Homebrew is unavailable:

```bash
git clone https://github.com/Lingtai-AI/lingtai.git
cd lingtai
./install.sh
lingtai-tui
```

The install script builds `lingtai-tui` and, when `npm` is available, `lingtai-portal`. It installs binaries into the Homebrew prefix when `brew` exists, otherwise `/usr/local/bin`.

### First-run tips

- Use a dark terminal theme; LingTai's default palette is optimized for it.
- Press `Ctrl+E` in the TUI when composing a long message to open an external editor.
- Hold `Option` on macOS/iTerm2 or `Shift` on many Linux/Windows terminals to select terminal text.
- Pick the **Adaptive** recipe for progressive feature discovery, or **Tutorial** for a guided walkthrough.
- If something feels broken after an upgrade, run `/doctor` inside the TUI or `lingtai-tui doctor` from a shell.

## What you get after first launch

A new project gains a `.lingtai/` directory. Inside it, each agent has its own home:

```text
project/
└── .lingtai/
    ├── human/                  # mailbox identity for the human/operator
    ├── <agent-name>/            # one running agent
    │   ├── init.json            # model, tools, preset, addon wiring
    │   ├── system/              # prompt layers, pad, summaries, rules
    │   ├── knowledge/           # private durable project memory
    │   ├── inbox/ outbox/       # internal mail transport
    │   ├── logs/                # event logs and runtime diagnostics
    │   ├── delegates/           # spawned-avatar ledger
    │   ├── daemons/             # ephemeral daemon run records
    │   └── .agent.json          # heartbeat, status, identity card
    └── .portal/                 # topology/history data for visualization
```

The agent is not trapped in the terminal. The TUI is the shell; the agent is the directory-backed process. Mail can wake it. The portal can watch it. Scripts can inspect it. Other agents can write to it.

## Core concepts

### Agent

A LingTai agent is a long-lived runtime process backed by a filesystem directory. It receives messages, calls tools, writes durable state, and can keep working asynchronously.

### Avatar

An avatar is a persistent peer agent spawned by another agent. It is not a function call. It has its own address, memory, pad, tools, logs, and life cycle. Use avatars when a capability should persist and learn over time.

### Daemon

A daemon is a short-lived subagent for isolated work: large scans, batch transformations, exploratory research, or code review where the parent only needs the conclusion. Daemons do not retain identity after completion, but their run artifacts remain on disk.

### Mail

Internal LingTai mail is the network transport. Agents talk to each other and to the human mailbox through `.lingtai/` mailboxes. Mail wakes sleeping agents and preserves a durable communication trail.

### Molt

Context is finite. LingTai agents can molt: summarize the current session, preserve durable layers, and shed the transient conversation. Identity, pad, knowledge, skills, and mail survive.

### Pad, knowledge, and skills

LingTai separates memory by purpose:

| Layer | Purpose |
|---|---|
| Conversation | Current transient work. Shed on molt. |
| Pad | Active project index: current tasks, decisions, pointers. |
| Character | Who the agent is: role, working style, learned identity. |
| Knowledge | Private durable facts, decisions, and project memory. |
| Skills | Reusable procedures and tools that can be shared across agents. |

### Soul flow

An idle agent can periodically reflect on its own recent work and past summaries. This is the agent's internal self-review loop: suggestions, blind spots, and next-step nudges. It is advisory, not a command channel.

## What LingTai can do

### Run a durable agent network

- Launch one or more agents in a project.
- Spawn persistent avatars for specialized work.
- Let agents communicate by mail.
- Keep agents alive after the TUI closes.
- Sleep, suspend, revive, refresh, or clear agents when needed.

### Work through files, shells, and tools

Agents can read and write files, run shell commands, search code, browse the web when configured, inspect images, call MCP servers, and delegate to coding CLIs such as Claude Code, Codex, or OpenCode when available.

### Accumulate memory and reusable procedure

Agents can record project facts in `knowledge/`, write reusable workflows as `skills`, pin important files into their pad, and carry summaries across molts.

### Connect to external channels

With MCP addons, LingTai can bridge agents to real external messaging systems such as Telegram, IMAP email, Feishu/Lark, and WeChat. External channels become additional doors into the same agent process, not separate bots with separate memory.

### Visualize the network

The portal records topology and mail edges so you can watch the agent network grow and replay how it evolved.

## Filesystem model

LingTai deliberately makes state inspectable. Important places include:

| Path | Meaning |
|---|---|
| `.lingtai/<agent>/init.json` | Agent configuration: preset, model, capabilities, MCP/addon activation. |
| `.lingtai/<agent>/system/` | Prompt layers, pad, standing rules, summaries, generated system prompt. |
| `.lingtai/<agent>/knowledge/` | Private durable knowledge entries. |
| `.lingtai/<agent>/.library/` | Agent-visible skills: intrinsic, custom, and shared. |
| `.lingtai/<agent>/logs/events.jsonl` | Structured runtime event log. |
| `.lingtai/<agent>/logs/agent.log` | Human-readable runtime log. |
| `.lingtai/<agent>/inbox/` and `outbox/` | Internal mail transport. |
| `.lingtai/<agent>/delegates/ledger.jsonl` | Spawned-avatar ledger. |
| `.lingtai/<agent>/daemons/` | Daemon run directories and transcripts. |
| `.lingtai/.portal/` | Portal topology and replay data. |
| `~/.lingtai-tui/` | Global TUI state: runtime venv, presets, extracted utility skills, command metadata. |

This makes LingTai easy to debug with ordinary tools:

```bash
# See running agents in a project
lingtai-tui list /path/to/project

# Tail an agent log
tail -f /path/to/project/.lingtai/<agent>/logs/agent.log

# Inspect structured runtime events
jq -r '.event' /path/to/project/.lingtai/<agent>/logs/events.jsonl | tail
```

## User interface: TUI, slash commands, and portal

### TUI

`lingtai-tui` is the main human interface. It provides:

- project setup and recipe selection;
- model/preset configuration;
- chat and mail views;
- agent status and token/stamina visibility;
- avatar and daemon visibility;
- markdown rendering;
- command palette and slash commands;
- upgrade and doctor flows.

Useful shell commands:

```bash
lingtai-tui                         # open the interactive TUI in the current project
lingtai-tui list <project-dir>       # list agents and states
lingtai-tui spawn <dir> --preset <name> [--agent-name <name>]
lingtai-tui presets                 # list available presets as JSON
lingtai-tui bootstrap               # re-extract bundled skills/utilities
lingtai-tui doctor                  # repair/update TUI runtime and utilities
```

Common in-TUI slash commands include:

| Command | Use |
|---|---|
| `/setup` | Change model, recipe, language, tools, or behavior. |
| `/kanban` | Inspect agent/project status. |
| `/viz` | Open or focus the network visualization. |
| `/mcp` | Configure external MCP/addon integrations. |
| `/skills` | Browse available skills and capabilities. |
| `/insights` | Ask the agent for a reflective second look. |
| `/sleep` | Put an agent to sleep while preserving state. |
| `/refresh` | Restart/reload an agent configuration. |
| `/cpr` | Revive a suspended or dead agent. |
| `/clear` | Clear an agent context window while preserving durable stores. |
| `/projects` | Switch or inspect known projects. |
| `/export` | Export or share project material. |
| `/doctor` | Diagnose installation/runtime issues. |

### Portal

`lingtai-portal` is the visualization server. It reads project state and portal records to show the agent network, edges, and history. Use it when the network becomes larger than one agent or when you want to see how avatars and messages relate over time.

The TUI can guide portal usage; the portal code lives in `portal/` and embeds a React frontend from `portal/web/`.

## External channels and MCP addons

LingTai uses MCP servers for external integrations. These are runtime bridges: they let the same long-lived agent process receive and send through external services while keeping its normal memory, tools, and mail history. The currently curated addon family includes:

| Addon | Purpose |
|---|---|
| `imap` | Real email through IMAP/SMTP. |
| `telegram` | Telegram bot send/receive. |
| `feishu` | Feishu/Lark messaging. |
| `wechat` | WeChat messaging through iLink/gewechat-style bridges. |

The `wechat` addon is for connecting an agent to WeChat as an external message channel. It is separate from the project community WeChat group listed below.

Design rule: addon-specific setup knowledge belongs to the addon package. The TUI provides the human-facing control panel; agents use MCP resources/tools and the addon's own onboarding resources.

Security notes:

- Credentials live in local `.secrets/` files, not in Git.
- Agents should not print tokens or secrets in messages.
- Unknown external IMAP senders should not receive replies unless the human confirms policy.
- External side effects should be explicit: sending messages, filing PRs/issues, deleting resources, or changing configuration should be treated as real actions.

## Recipes, skills, and knowledge

### Recipes

A recipe shapes a new LingTai project: greeting, behavior, bundled skills, and onboarding style. The default Adaptive recipe progressively reveals features when they become useful. The Tutorial recipe walks a new user through concepts step by step.

### Skills

Skills are portable procedures. They can include markdown instructions, scripts, templates, reference data, and validation checklists. Agents load skills on demand instead of bloating every prompt with every procedure.

Examples of skill domains:

- web browsing and scraping;
- academic paper fetching;
- image/audio understanding;
- LingTai kernel/TUI anatomy;
- MCP registration and debugging;
- release workflows;
- issue reporting;
- daemon and avatar operation.

### Knowledge

Knowledge is private durable memory owned by one agent. It is for project facts, decisions, paths, collaborator preferences, and lessons that should survive context loss but are not portable enough to become a shared skill.

## Architecture

LingTai is split across two primary repositories:

| Repository | Language | Owns |
|---|---|---|
| [`Lingtai-AI/lingtai`](https://github.com/Lingtai-AI/lingtai) | Go + TypeScript | TUI, portal, Homebrew/source install pipeline, shipped utility skills, website-adjacent docs. |
| [`Lingtai-AI/lingtai-kernel`](https://github.com/Lingtai-AI/lingtai-kernel) | Python + Rust sidecar pieces | Agent runtime, LLM turn loop, intrinsic tools, session management, context molt, MCP host, PyPI package `lingtai`. |

This repository contains two Go binaries:

| Tree | Binary | Description |
|---|---|---|
| `tui/` | `lingtai-tui` | Bubble Tea terminal application, setup wizard, process monitor, slash-command shell, preset editor, upgrade/doctor flows. |
| `portal/` | `lingtai-portal` | Go HTTP server with embedded React frontend for topology/replay visualization. |

The Go TUI does not implement the agent mind. It launches and supervises Python kernel agents as subprocesses. Communication between UI and agents happens through the project filesystem: heartbeats, mailboxes, logs, generated prompt files, and portal records.

### Runtime ownership

The kernel package is published to PyPI as `lingtai`, but normal users should let the TUI manage it. The TUI owns the runtime virtualenv under `~/.lingtai-tui/runtime/venv/` and project-specific runtime wiring. Installing `lingtai` into your system Python does not update running LingTai projects.

For kernel development, install the sibling kernel repo into the TUI runtime venv intentionally:

```bash
# from a checkout where ../lingtai-kernel exists
~/.lingtai-tui/runtime/venv/bin/pip3 install -e ../lingtai-kernel
```

That is a development workflow, not the ordinary user upgrade path.

## Installation and upgrade details

### Homebrew upgrade

```bash
brew update
brew upgrade lingtai-ai/lingtai/lingtai-tui
```

After upgrading, restart the TUI so the new binary is used. If agents are running, the TUI will guide safe upgrade handling.

### Source build

```bash
git clone https://github.com/Lingtai-AI/lingtai.git
cd lingtai/tui
go test ./...
go build -o bin/lingtai-tui .

cd ../portal/web
npm ci
npm run build
cd ..
go test ./...
go build -o bin/lingtai-portal .
```

### Runtime repair

```bash
lingtai-tui doctor
```

`doctor` checks the TUI/kernel/runtime relationship, refreshes utility skills, and reports repair steps. Use it after failed startup, broken upgrades, or missing bundled skills.

## Development workflow

For non-trivial changes, work in a Git worktree off `origin/main`:

```bash
cd /path/to/lingtai
git fetch origin main
git worktree add -b docs/my-change .worktrees/my-change origin/main
cd .worktrees/my-change
```

### Validate TUI changes

```bash
cd tui
go test ./...
go vet ./...
go build -o bin/lingtai-tui .
```

### Validate portal changes

```bash
cd portal/web
npm ci
npm run build

cd ..
go test ./...
go build -o bin/lingtai-portal .
```

### Validate docs-only changes

For README-only work, at minimum:

```bash
# check links/paths manually, then verify no accidental code changes
git diff --check
git status --short
```

If documentation references generated UI commands or shipped skills, run the relevant tests or inspect `~/.lingtai-tui/commands.json` after a bootstrap.

## Repository map

```text
.
├── README.md / README.zh.md / README.wen.md
├── ANATOMY.md                 # source-grounded repo map for agents and humans
├── CLAUDE.md                  # coding-agent guidance
├── RELEASING.md               # release checklist notes
├── install.sh                 # source installer
├── tui/                       # lingtai-tui Go module
│   ├── main.go
│   ├── internal/              # TUI implementation packages
│   ├── i18n/                  # en/zh/wen UI strings
│   └── packages/              # npm wrapper package metadata
├── portal/                    # lingtai-portal Go module
│   ├── main.go
│   ├── web/                   # React/Vite frontend
│   └── i18n/
├── docs/                      # design notes, blog material, status, known limitations
├── examples/                  # example init/addon/policy JSONC files
├── scripts/                   # helper scripts
└── discussions/               # design patches and investigation notes
```

For source navigation, start with `ANATOMY.md`, then descend into `tui/ANATOMY.md` or `portal/ANATOMY.md`. Anatomy files cite code and are intended to stay updated when structure changes.

## Troubleshooting

### `lingtai-tui` is not found

Make sure Homebrew's bin directory is on `PATH`:

```bash
brew --prefix
ls "$(brew --prefix)/bin/lingtai-tui"
```

If you used `install.sh`, check `/usr/local/bin/lingtai-tui` or the Homebrew prefix.

### The TUI starts but the agent does not respond

Run:

```bash
lingtai-tui doctor
lingtai-tui list /path/to/project
```

Then inspect the agent logs:

```bash
tail -100 /path/to/project/.lingtai/<agent>/logs/agent.log
```

### A skill or command is missing

Refresh bundled utilities:

```bash
lingtai-tui bootstrap
```

Or use `/doctor` from inside the TUI.

### You upgraded but behavior did not change

Remember that there are two layers:

1. the Go TUI binary installed by Homebrew/source build;
2. the Python kernel/runtime managed by the TUI virtualenv.

Restart the TUI after upgrading the binary. Use `lingtai-tui doctor` if the runtime appears stale. Do not assume system Python packages affect LingTai projects.

### You are developing the kernel and local edits are ignored

Install the kernel checkout into the TUI runtime venv intentionally:

```bash
~/.lingtai-tui/runtime/venv/bin/pip3 install -e /path/to/lingtai-kernel
```

Then refresh or restart the affected agents.

## Contributing

Good LingTai contributions are source-grounded and workflow-aware.

1. Read the relevant anatomy first:
   - root: `ANATOMY.md`
   - TUI: `tui/ANATOMY.md`
   - portal: `portal/ANATOMY.md`
2. Work in a branch/worktree.
3. Keep changes scoped.
4. Run the relevant validation commands.
5. Update anatomy/docs when structural behavior changes.
6. Open a PR with:
   - what changed;
   - why it changed;
   - validation performed;
   - screenshots or terminal output for UI changes when useful.

Areas that commonly need help:

- TUI usability and accessibility;
- portal visualization and replay;
- MCP/addon onboarding resources;
- cross-platform installation polish;
- docs and tutorials;
- runtime diagnostics and issue reproduction;
- high-quality skills that encode reusable workflows.

## Project philosophy

LingTai borrows its name from the heart-mind: the square inch where transformation begins. The system follows three practical beliefs:

1. **Agents need bodies.** A durable filesystem home gives an agent continuity, inspectability, and a place to accumulate tools and memory.
2. **Networks should grow through service.** When a task needs a new capability, spawn an avatar, write a skill, record knowledge, and make the next task easier.
3. **Memory must be layered.** Conversation is temporary; pad, character, knowledge, skills, and mail carry what matters forward.

The goal is not agent theater. The goal is to make useful long-running AI collaborators that can be inspected, restarted, taught, and improved.

## Community

- Website and release notes: <https://lingtai.ai>
- Main repo: <https://github.com/Lingtai-AI/lingtai>
- Kernel repo: <https://github.com/Lingtai-AI/lingtai-kernel>
- Homebrew tap: <https://github.com/Lingtai-AI/homebrew-lingtai>
- Discord: <https://discord.gg/cMchjXpg>
- GitHub issues: <https://github.com/Lingtai-AI/lingtai/issues>
- GitHub discussions: <https://github.com/Lingtai-AI/lingtai/discussions>

For Chinese-language discussion and early testing, join the WeChat group by scanning the QR code below. Add the author on WeChat and mention `lingtai`; if the QR code expires, please open an issue and we will refresh it.

<img src="docs/assets/wechat.png" alt="WeChat QR code for joining the LingTai testing group" width="200">


## Star history

[![Star History Chart](https://api.star-history.com/svg?repos=Lingtai-AI/lingtai&type=Date)](https://www.star-history.com/#Lingtai-AI/lingtai&Date)

## License

Apache-2.0. See [LICENSE](LICENSE).
