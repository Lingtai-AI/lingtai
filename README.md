<div align="center">

# LingTai

**Build an AI organization inside your project — not just another agent.**

Local-first · resident agents · soul-flow proactiveness · mailboxes · lifecycle · multi-agent networks

[English](README.md) · [中文](README.zh.md) · [文言](README.wen.md) · [Website](https://lingtai.ai) · [Tutorial](https://lingtai.ai/en/tutorial/) · [Releases](https://lingtai.ai/releases/)

[![License](https://img.shields.io/github/license/Lingtai-AI/lingtai?color=%237dab8f)](LICENSE)
[![Kernel](https://img.shields.io/badge/kernel-lingtai--kernel-%237dab8f)](https://github.com/Lingtai-AI/lingtai-kernel)
[![Site](https://img.shields.io/badge/site-lingtai.ai-%23d4a853)](https://lingtai.ai)
[![Discord](https://img.shields.io/badge/discord-join-%235865F2?logo=discord&logoColor=white)](https://discord.gg/cMchjXpg)

</div>

---

Most agent tools give you a better worker. **LingTai gives you the substrate for an AI organization** — long-lived agents that live in your project's filesystem, with home directories, inbox/outbox mailboxes, durable memory, lifecycle controls, self-reflection, and peers they can spawn or call when the work gets bigger than one mind.

It is **filesystem-native, not a chat window**. Every agent has a home under `.lingtai/`; all state — mail, memory, logs, heartbeats — is plain files you can read with `ls`, `cat`, `jq`, your editor, or another coding agent. Close the terminal and the organization persists: it can be inspected, restarted, taught, and recovered.

```text
You
  "Watch the repo overnight. If a PR breaks, inspect it, draft a fix,
   and send me a morning brief."

LingTai
  wakes from its mailbox → reads durable project memory
  → runs shell / web / file / coding-agent tools
  → reflects via soul flow when idle or stuck
  → writes notes, reports, patches, or schedules
  → asks a specialist avatar or daemon when parallel work helps
  → replies on Telegram / TUI / email with the artifacts
```

Coding tools such as **Claude Code**, **Codex**, **OpenClaw**, and **Hermes** are capable hands. LingTai is the organizational layer around those hands: it uses them as workers while owning the roles, memory, communication, supervision, and recovery that let an agent network keep operating after a single chat or terminal session ends.

## Quick start

```bash
curl -fsSL https://lingtai.ai/install.sh | bash
mkdir my-project && cd my-project
lingtai-tui
```

The installer covers macOS, Linux, and WSL (native Windows/PowerShell is planned). It installs `lingtai-tui` and `lingtai-portal`. From there, **the TUI manages everything else** — on first run it creates `.lingtai/`, provisions its own Python runtime, walks you through model/preset setup, and starts one resident assistant for the project. To upgrade later, re-run the installer (or `lingtai-tui self-update`) and restart the TUI.

> **New here?** Follow the step-by-step [tutorial at lingtai.ai](https://lingtai.ai/en/tutorial/) — install, first task, channels, memory, and lifecycle, walked through end to end.

> Homebrew (`brew install lingtai-ai/lingtai/lingtai-tui`) still works for existing users, but the one-line installer is the recommended path for new installs. The `lingtai` PyPI package is the Python runtime the TUI manages for you — reach for `pip` only when developing or diagnosing the kernel itself.

## Interfaces

**TUI — `lingtai-tui`** is the main human surface: setup, model/preset configuration, chat and mail, agent status (token/context + heartbeat), avatar and daemon visibility, a slash-command palette, and upgrade/doctor flows. Type `/help` inside the TUI for the complete slash-command reference (the canonical catalog is the bundled [`lingtai-tui-help` skill](tui/internal/preset/skills/lingtai-tui-help/assets/slash-commands.en.md); this README does not duplicate it). Run `lingtai-tui doctor` if anything looks broken after an upgrade.

**Portal — `lingtai-portal`** is the visualization server. It reads project state to show the live agent network, mail edges, and history — useful once a project has more than one assistant or when you want to see how the work evolved.

**External channels** bridge the *same* assistant to the platforms you already use — memory, tools, and history are shared across them, and they are doors into one assistant, not separate bots. Configure from the TUI's `/mcp` panel or declare them in `init.json`. Credentials live in local `.secrets/` files (never in Git); external side effects (sending messages, filing issues) are treated as real actions, and unknown senders do not auto-receive replies.

| Addon | Use it for |
|---|---|
| `telegram` | Talk to your assistant from Telegram (DMs, optional allowlist, voice/file passthrough). |
| `feishu` | Feishu/Lark — WebSocket long connection, no public IP required. |
| `wechat` | WeChat through an iLink/gewechat-style bridge. |
| `whatsapp` | WhatsApp through the curated LingTai bridge. |
| `imap` | Real email through IMAP/SMTP — multi-account, with safety defaults for unknown senders. |

**Coding agents as hands.** LingTai assistants live in the filesystem, so any coding agent can drive them — as daemon backends for focused implementation jobs, or as peers through the shared `.lingtai/human/` mailbox. LingTai owns the long-running plan, memory, and coordination; the coding agent does the precise, reviewable work.

- **Claude Code** — `claude plugin add Lingtai-AI/claude-code-plugin`
- **OpenAI Codex CLI** — `git clone https://github.com/Lingtai-AI/codex-plugin.git && cd codex-plugin && ./install.sh`
- **Other agents** (OpenCode, OpenClaw, Hermes, …) — vendor the [`lingtai-skill`](https://github.com/Lingtai-AI/lingtai-skill) protocol skill under your tool's skills directory.

<div align="center">

<img src="docs/assets/network-demo.gif" alt="LingTai portal showing a live local network of long-lived project agents" width="100%">

</div>

## Architecture

LingTai is split across two repositories.

| Repository | Language | Owns |
|---|---|---|
| [`Lingtai-AI/lingtai`](https://github.com/Lingtai-AI/lingtai) (this one) | Go + TypeScript | TUI, portal, install pipeline, shipped utility skills. Ships `lingtai-tui` and `lingtai-portal`. |
| [`Lingtai-AI/lingtai-kernel`](https://github.com/Lingtai-AI/lingtai-kernel) | Python (+ Rust sidecar) | Agent runtime, LLM turn loop, intrinsic tools, session/context/molt management, MCP host. Published as the `lingtai` PyPI package. |

The Go TUI does not run the agent mind. It launches and supervises Python kernel agents as subprocesses; everything between UI and agents flows through the project filesystem (`.lingtai/` mailboxes, heartbeats, logs, prompt files, portal records). That is why the state is so easy to inspect — and why other tools can cooperate with it without any SDK.

For the source-grounded repo map, start at [`ANATOMY.md`](ANATOMY.md), then descend into [`tui/ANATOMY.md`](tui/ANATOMY.md) or [`portal/ANATOMY.md`](portal/ANATOMY.md). For what each layer's interfaces and expected agent behavior promise, read [`CONTRACT.md`](CONTRACT.md). To navigate by knowledge graph, see [`docs/graphify.md`](docs/graphify.md).

## Development & contributing

Build the TUI with `cd tui && make build`; build the portal with `cd portal && make build`. You need Go 1.26+, `make`, and (for the portal) Node.js/npm.

Contributions are source-grounded and workflow-aware. Before any development work, find and read this repository's local dev guide — the repository-root [`dev-guide-skill`](dev-guide-skill/SKILL.md); it routes each task through the baseline, the distributed [`ANATOMY.md`](ANATOMY.md) and [`CONTRACT.md`](CONTRACT.md) systems, validation, and the PR gate without duplicating them.

1. Read the relevant anatomy first — root [`ANATOMY.md`](ANATOMY.md), then `tui/ANATOMY.md` or `portal/ANATOMY.md` — and the paired [`CONTRACT.md`](CONTRACT.md) when changing an interface or expected behavior.
2. Work in a branch or worktree off `origin/main`; keep the change scoped.
3. Run the relevant validation. Update [`ANATOMY.md`](ANATOMY.md) for structural/navigation changes; update [`CONTRACT.md`](CONTRACT.md) and its conformance tests for interface or expected-behavior changes; update both only when both change.
4. Open a PR that says what changed, why, and how you validated it.

```bash
# TUI changes
cd tui && go test ./... && go vet ./... && go build -o bin/lingtai-tui .

# Portal changes
cd portal/web && npm ci && npm run build && cd .. && go test ./... && go build -o bin/lingtai-portal .

# Docs-only
git diff --check && git status --short
```

See [`RELEASING.md`](RELEASING.md) for the release process. Areas that often need help: TUI usability and accessibility, portal visualization, MCP/addon onboarding, cross-platform install polish, docs, runtime diagnostics, and reusable skills.

## Community

- Website, tutorial, and release notes: <https://lingtai.ai>
- Main repo: <https://github.com/Lingtai-AI/lingtai> · Kernel: <https://github.com/Lingtai-AI/lingtai-kernel>
- Discord: <https://discord.gg/cMchjXpg>
- Issues: <https://github.com/Lingtai-AI/lingtai/issues> · Discussions: <https://github.com/Lingtai-AI/lingtai/discussions>

For Chinese-language discussion and early testing, scan the WeChat QR below. Add the author on WeChat with the note `lingtai`; if the QR has expired, open an issue and we will refresh it.

<img src="docs/assets/wechat.png" alt="WeChat QR code for joining the LingTai testing group" width="200">

## License

Apache-2.0 — see [LICENSE](LICENSE).
