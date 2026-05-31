<div align="center">

<img src="docs/assets/network-demo.gif" alt="LingTai — a local AI assistant that lives in your project and can grow into a team" width="100%">

# LingTai

**Your local, always-on AI assistant for real work.**

[English](README.md) · [中文](README.zh.md) · [文言](README.wen.md) · [Website](https://lingtai.ai) · [Releases](https://lingtai.ai/releases/)

[![Homebrew](https://img.shields.io/badge/brew-lingtai--tui-%237dab8f)](https://github.com/Lingtai-AI/homebrew-lingtai)
[![License](https://img.shields.io/github/license/Lingtai-AI/lingtai?color=%237dab8f)](LICENSE)
[![Kernel](https://img.shields.io/badge/kernel-lingtai--kernel-%237dab8f)](https://github.com/Lingtai-AI/lingtai-kernel)
[![Site](https://img.shields.io/badge/site-lingtai.ai-%23d4a853)](https://lingtai.ai)
[![Discord](https://img.shields.io/badge/discord-join-%235865F2?logo=discord&logoColor=white)](https://discord.gg/cMchjXpg)

</div>

---

**LingTai** is a local AI assistant that lives in your project and stays on. It remembers what matters, talks to you through the channels you already use, runs tools and workflows on your behalf, and — when the job gets bigger than one assistant — can grow into a small AI team you supervise.

It is not a chat window, a notebook, or a one-shot coding agent. It is a real process with memory, tools, and a schedule: send it a task in the terminal, on Telegram, or by email, then come back later to find progress waiting for you.

If you want a personal AI workbench that feels yours — local, persistent, scriptable, and able to scale up when the work demands it — this is it.

## Install

Recommended path on macOS and Linux:

```bash
brew install lingtai-ai/lingtai/lingtai-tui
lingtai-tui
```

The TUI handles the rest: it provisions its own Python runtime, walks you through model selection, opens your first project, and gets a working assistant in front of you within a couple of minutes. On first launch pick the **Adaptive** recipe for progressive feature discovery, or **Tutorial** for a guided walkthrough.

> The `lingtai` PyPI package exists, but it is the Python runtime the TUI manages on your behalf. Use Homebrew (or the source build below) to install and upgrade; reach for `pip` only when you are developing the kernel itself.

For source builds, mainland-China mirror setup, and from-tarball install paths, see [Install in detail](#install-in-detail).

## Use it for

Concrete things people run LingTai for today:

- **A daily project briefing.** Each morning the assistant scans what changed, summarizes outstanding work, and posts the brief to Telegram or your TUI before you sit down.
- **Triage GitHub issues and PRs.** It reads new activity, categorizes, drafts replies for review, and queues anything risky for your attention.
- **Prepare a livestream or talk.** Outline, rehearse arguments, gather references, build slide notes, hold them in project memory across sessions.
- **Research and investor memos.** Multi-source web research, web reading, paper fetching, then drafting and revision with a fact trail you can audit.
- **Long-running coding and review work.** It can use Claude Code, Codex, or OpenCode as its hands: the coding CLI makes precise edits, while LingTai owns the plan, memory, review log, and human updates.
- **Schedules and reminders that act, not just notify.** "Every weekday at 9, check the deploy queue and ping me on Telegram if anything is stuck."
- **Personal knowledge that survives.** Decisions, paths, collaborator preferences, lessons — kept as durable knowledge entries owned by the assistant, not by a chat window.

## What it can do

| | |
|---|---|
| **Lives in your project** | One `.lingtai/` folder per project. The assistant is a real process with a directory home — you can `ls`, `cat`, and `tail -f` it. |
| **Long-term memory you can read** | Pad for active context, knowledge for durable facts, character for who the assistant is. Plain Markdown on disk, not a hidden vector store. |
| **Skills and workflows** | Reusable, on-demand procedures: web research, paper fetching, vision/audio understanding, MCP debugging, release pipelines, your own. Skills load when relevant; your prompt stays small. |
| **Multiple channels, one mind** | Talk to the same assistant from the TUI, Telegram, Feishu/Lark, WeChat, or IMAP email. External messages wake the same long-lived process, with the same memory and tools. |
| **Real tools** | Read/write files, run shell commands, browse the web, fetch and extract pages, understand images, work with audio, call any MCP server, and delegate implementation to coding CLIs like Claude Code, Codex, or OpenCode. |
| **Schedules and automation** | Recurring tasks, scheduled checks, and reminders the assistant *acts on*, with delivery to whichever channel you prefer. |
| **Grows into a team when needed** | Spawn persistent avatars (specialist peers with their own memory) or short-lived daemons (focused workers for one batch task) — supervise the whole network from one place. |
| **Visual portal** | `lingtai-portal` renders the live agent network: who is alive, what they are working on, who mailed whom, how it grew. |
| **Lifecycle you control** | Sleep, wake, refresh, revive, or clear an assistant when needed. Crash-safe: `/doctor` repairs the common failure modes. |

## Grows with the work

Most projects start with one assistant and stay there. When that is enough, you ignore everything below. When the work gets bigger, LingTai has a path up: an **AI agent organization** with roles, memory, handoffs, and supervision — not just more chat tabs.

- **Forget noise, keep experience.** Context windows are finite. When a session gets long, the assistant **molts**: it writes a summary, sheds the transient conversation, and the next self picks up with pad + character + knowledge + skills + mail intact. You do not start over.
- **Hand off to a specialist.** Spawn an **avatar** — a peer assistant with its own home, memory, and life cycle. Give it a long-running goal (own the docs site, own the support inbox, own this research thread). It keeps learning even when you close the laptop.
- **Use coding agents as hands.** A daemon can be a normal LingTai worker, or it can run a coding CLI backend such as Claude Code, Codex, or OpenCode. LingTai keeps the goal, memory, and conversation with you; the coding agent performs the verifiable file edits and tests.
- **Split a batch.** Spawn a **daemon** — a short-lived worker for one focused job (scan 200 files, draft 50 replies, run 30 reviews in parallel). Daemons return results and disappear; the parent keeps your attention.
- **Watch it from the portal.** As the network grows, the portal shows who is alive, who has been mailing whom, and how the topology evolved. Use it to debug, to admire, or to plan the next refactor.

You do not have to opt in. A single assistant works fine on its own — the network is the ceiling, not the floor.

## External channels

LingTai bridges the same long-lived assistant to the messaging surfaces you already use. The currently curated MCP addons:

| Addon | Use it for |
|---|---|
| `telegram` | Talk to your assistant from Telegram (DMs, optional allowlist, voice/file passthrough). |
| `feishu` | Feishu/Lark — uses a WebSocket long connection, no public IP required. |
| `wechat` | WeChat through an iLink/gewechat-style bridge. |
| `imap` | Real email through IMAP/SMTP — multi-account, with safety defaults for unknown senders. |

Channels are doors into the *same* assistant, not separate bots. Memory, tools, and history are shared across them. Configure from the TUI's `/mcp` control panel, or declare them in `init.json`.

Credentials live in local `.secrets/` files (never in Git). Unknown external senders do not auto-receive replies. External side effects (sending messages, filing issues, deleting resources) are treated as real actions by default.

## The interface

### TUI

`lingtai-tui` is the main human surface. It gives you setup, model/preset configuration, chat and mail, agent status (token + stamina + heartbeat), avatar and daemon visibility, markdown rendering, a slash-command palette, and upgrade/doctor flows.

Frequently used slash commands:

| Command | Use |
|---|---|
| `/setup` | Change model, recipe, language, tools, or behavior. |
| `/kanban` | Inspect agent + project status. |
| `/mcp` | Configure external channels (Telegram/Feishu/WeChat/IMAP/…). |
| `/skills` | Browse available skills and capabilities. |
| `/viz` | Open the network visualization. |
| `/insights` | Ask the assistant for a reflective second look. |
| `/sleep` · `/refresh` · `/cpr` · `/clear` | Lifecycle: pause, reload, revive, reset context. |
| `/projects` | Switch or inspect known projects. |
| `/doctor` | Diagnose installation/runtime issues. |

Shell entrypoints when useful:

```bash
lingtai-tui                          # open the TUI in the current project
lingtai-tui list <project>            # list agents and states
lingtai-tui spawn <dir> --preset <name> [--agent-name <name>]
lingtai-tui bootstrap                # re-extract bundled skills/utilities
lingtai-tui doctor                   # repair/update TUI runtime
```

### Portal

`lingtai-portal` is the visualization server. It reads project state to show the agent network, mail edges, and history. It becomes useful when you have more than one assistant per project, or when you want to see how the work evolved.

### Tips

- Use a dark terminal theme — LingTai's palette is tuned for it.
- `Ctrl+E` in the TUI opens an external editor for long messages.
- Hold `Option` (macOS/iTerm2) or `Shift` (most Linux/Windows terminals) to select text without the TUI capturing it.
- If anything feels broken after an upgrade, run `/doctor` (or `lingtai-tui doctor` from a shell).

## Filesystem you can read

LingTai keeps state on disk, on purpose. You can debug it with `ls`, `cat`, `tail`, `jq`, `grep`, your editor, or another coding agent. The shape after first launch:

```text
project/
└── .lingtai/
    ├── human/                  # your mailbox identity
    ├── <agent-name>/            # one running assistant
    │   ├── init.json            # model, tools, preset, MCP wiring
    │   ├── system/              # prompt layers, pad, rules, summaries
    │   ├── knowledge/           # durable private memory
    │   ├── inbox/ outbox/       # internal mail
    │   ├── logs/                # event log + human-readable log
    │   ├── delegates/           # spawned-avatar ledger
    │   ├── daemons/             # daemon run records
    │   └── .agent.json          # heartbeat, status, identity card
    └── .portal/                 # topology/history for visualization
```

Useful inspection commands:

```bash
lingtai-tui list /path/to/project                          # running agents and states
tail -f /path/to/project/.lingtai/<agent>/logs/agent.log    # human-readable log
jq -r '.event' /path/to/project/.lingtai/<agent>/logs/events.jsonl | tail   # structured events
```

## Use coding agents as hands

LingTai assistants live in the filesystem, so coding agents can work with them in two ways. As **daemon backends**, Claude Code, Codex, and OpenCode can be launched for focused implementation or review jobs while LingTai keeps the long-running plan and memory. As **peer tools**, any coding agent that can read and write files can collaborate through the shared `.lingtai/human/` mailbox.

- **Claude Code** — `claude plugin add Lingtai-AI/claude-code-plugin`
- **OpenAI Codex CLI** — `git clone https://github.com/Lingtai-AI/codex-plugin.git && cd codex-plugin && ./install.sh`
- **Other coding agents** (OpenCode, OpenClaw, Hermes, …) — vendor the [`lingtai-skill`](https://github.com/Lingtai-AI/lingtai-skill) protocol skill under your tool's skills directory.

The split: a coding agent is precise and verifiable — every tool call visible, every edit reviewable. A LingTai assistant is asynchronous and patient — it remembers the goal, talks to the human, coordinates parallel contexts, and decides when to hand work to a specialist. Use the coding agent as hands. Use LingTai as the long-running collaborator that plans, drafts, monitors, and remembers.

## Install in detail

### Homebrew (recommended)

```bash
brew install lingtai-ai/lingtai/lingtai-tui
lingtai-tui

# upgrade later
brew update
brew upgrade lingtai-ai/lingtai/lingtai-tui
```

After upgrading, restart the TUI so the new binary takes over. The TUI manages the Python runtime under `~/.lingtai-tui/runtime/venv/` — installing `lingtai` into your system Python does not affect a running project.

### From source

Use this when hacking on the TUI/portal itself or when Homebrew is unavailable:

```bash
git clone https://github.com/Lingtai-AI/lingtai.git
cd lingtai
./install.sh
lingtai-tui
```

`install.sh` builds `lingtai-tui` and (when `npm` is available) `lingtai-portal`, then installs binaries into the Homebrew prefix if `brew` exists, otherwise `/usr/local/bin`.

### Kernel dev mode (advanced)

Only if you are editing the kernel checkout and want your edits to take effect immediately in the TUI runtime:

```bash
~/.lingtai-tui/runtime/venv/bin/pip3 install -e /path/to/lingtai-kernel
```

### Runtime repair

```bash
lingtai-tui doctor
```

`doctor` checks the TUI/kernel/runtime relationship, refreshes shipped utility skills, and reports concrete repair steps. Use it after a failed startup or a stale-looking upgrade.

## Architecture

LingTai is split across two repositories.

| Repository | Language | Owns |
|---|---|---|
| [`Lingtai-AI/lingtai`](https://github.com/Lingtai-AI/lingtai) (this one) | Go + TypeScript | TUI, portal, Homebrew/source install, shipped utility skills. |
| [`Lingtai-AI/lingtai-kernel`](https://github.com/Lingtai-AI/lingtai-kernel) | Python (+ Rust sidecar pieces) | Agent runtime, LLM turn loop, intrinsic tools, session/context/molt management, MCP host. Published as the `lingtai` PyPI package. |

The Go TUI does not run the agent mind. It launches and supervises Python kernel agents as subprocesses; everything between UI and agents flows through the project filesystem (`.lingtai/` mailboxes, heartbeats, logs, prompt files, portal records). That is why the state is so easy to inspect — and why other tools can cooperate with it without any SDK.

This repo carries two Go binaries:

| Tree | Binary | Description |
|---|---|---|
| `tui/` | `lingtai-tui` | Bubble Tea terminal app, setup wizard, process monitor, slash-command shell, preset editor, upgrade/doctor flows. |
| `portal/` | `lingtai-portal` | Go HTTP server with an embedded React frontend for topology/replay visualization. |

## Docs by goal

- **New here?** Run `lingtai-tui`, pick the **Tutorial** recipe, follow the prompts.
- **Set up a channel** — `/mcp` inside the TUI, then the addon's own onboarding resource.
- **Write a skill** — see `tui/internal/preset/skills/lingtai-dev-guide/` after first launch.
- **Source layout** — start at [`ANATOMY.md`](ANATOMY.md), then descend into `tui/ANATOMY.md` or `portal/ANATOMY.md`.
- **Release process** — [`RELEASING.md`](RELEASING.md).
- **Contributing** — anatomy-first, worktree-first, with validation in the PR body. See [Contributing](#contributing).

## Repository map

```text
.
├── README.md / README.zh.md / README.wen.md
├── ANATOMY.md                 # source-grounded repo map for agents and humans
├── CLAUDE.md                  # coding-agent guidance
├── RELEASING.md               # release checklist
├── install.sh                 # source installer
├── tui/                       # lingtai-tui Go module
│   ├── main.go
│   ├── internal/              # TUI implementation
│   ├── i18n/                  # en/zh/wen UI strings
│   └── packages/              # npm wrapper metadata
├── portal/                    # lingtai-portal Go module
│   ├── main.go
│   ├── web/                   # React/Vite frontend
│   └── i18n/
├── docs/                      # design notes, blog, status, known limitations
├── examples/                  # example init/addon/policy JSONC files
├── scripts/                   # helper scripts
└── discussions/               # design patches and investigation notes
```

## Troubleshooting

**`lingtai-tui` is not found.** Make sure Homebrew's bin directory is on `PATH` (`brew --prefix`/bin). If you used `install.sh`, check `/usr/local/bin/lingtai-tui` or the Homebrew prefix.

**The TUI starts but the assistant does not respond.** Run `lingtai-tui doctor` and `lingtai-tui list /path/to/project`, then `tail -100 /path/to/project/.lingtai/<agent>/logs/agent.log`.

**A skill or command is missing.** `lingtai-tui bootstrap` (or `/doctor` inside the TUI) re-extracts bundled utilities.

**You upgraded but behavior did not change.** Two layers: the Go TUI binary (Homebrew/source) and the Python runtime (TUI-managed venv). Restart the TUI after upgrading; run `doctor` if the runtime looks stale. Installing the `lingtai` PyPI package into your system Python does not affect projects.

**You are developing the kernel and your edits are ignored.** See [Kernel dev mode](#kernel-dev-mode-advanced).

## Development

For non-trivial changes, work in a Git worktree off `origin/main`:

```bash
cd /path/to/lingtai
git fetch origin main
git worktree add -b docs/my-change .worktrees/my-change origin/main
cd .worktrees/my-change
```

Validation:

```bash
# TUI changes
cd tui && go test ./... && go vet ./... && go build -o bin/lingtai-tui .

# Portal changes
cd portal/web && npm ci && npm run build && cd .. && go test ./... && go build -o bin/lingtai-portal .

# Docs-only
git diff --check && git status --short
```

If a doc change references generated UI commands or shipped skills, regenerate via `lingtai-tui bootstrap` and inspect `~/.lingtai-tui/commands.json`.

## Contributing

LingTai contributions are source-grounded and workflow-aware.

1. Read the relevant anatomy first: root `ANATOMY.md`, then `tui/ANATOMY.md` or `portal/ANATOMY.md`.
2. Work in a branch/worktree.
3. Keep changes scoped.
4. Run the relevant validation commands.
5. Update anatomy/docs when structural behavior changes.
6. Open a PR that says what changed, why, and how you validated it.

Common areas that need help: TUI usability and accessibility, portal visualization and replay, MCP/addon onboarding, cross-platform install polish, docs and tutorials, runtime diagnostics, and high-quality reusable skills.

## Design philosophy

LingTai borrows its name from the heart-mind — the square inch where transformation begins. The product follows three practical beliefs:

1. **Assistants need bodies.** A durable filesystem home gives continuity, inspectability, and a place to accumulate tools and memory.
2. **Networks should grow through service.** When a task needs a new capability, write a skill, record knowledge, or spawn a specialist, and the next task gets easier.
3. **Memory must be layered.** Conversation is temporary. Pad, character, knowledge, skills, and mail carry what matters forward.

The goal is not agent theater. The goal is useful long-running AI collaborators that can be inspected, restarted, taught, and improved.

## Community

- Website and release notes: <https://lingtai.ai>
- Main repo: <https://github.com/Lingtai-AI/lingtai>
- Kernel repo: <https://github.com/Lingtai-AI/lingtai-kernel>
- Homebrew tap: <https://github.com/Lingtai-AI/homebrew-lingtai>
- Discord: <https://discord.gg/cMchjXpg>
- GitHub issues: <https://github.com/Lingtai-AI/lingtai/issues>
- GitHub discussions: <https://github.com/Lingtai-AI/lingtai/discussions>

For Chinese-language discussion and early testing, scan the WeChat QR below. Add the author on WeChat with the note `lingtai`; if the QR has expired, please open an issue and we will refresh it.

<img src="docs/assets/wechat.png" alt="WeChat QR code for joining the LingTai testing group" width="200">

## Star history

[![Star History Chart](https://api.star-history.com/svg?repos=Lingtai-AI/lingtai&type=Date)](https://www.star-history.com/#Lingtai-AI/lingtai&Date)

## License

Apache-2.0 — see [LICENSE](LICENSE).
