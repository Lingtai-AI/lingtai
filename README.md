<div align="center">

# LingTai

**The self-evolving Digital Scientist — a lifelong agent that grows with you and your work.**

Digital Scientist · lifelong agent · self-growing memory · durable knowledge & skills · local-first · multi-agent networks

[English](README.md) · [中文](README.zh.md) · [文言](README.wen.md) · [Website](https://lingtai.ai) · [Tutorial](https://lingtai.ai/en/tutorial/) · [Releases](https://lingtai.ai/releases/)

[![License](https://img.shields.io/github/license/Lingtai-AI/lingtai?color=%237dab8f)](LICENSE)
[![Kernel](https://img.shields.io/badge/kernel-lingtai--kernel-%237dab8f)](https://github.com/Lingtai-AI/lingtai-kernel)
[![Site](https://img.shields.io/badge/site-lingtai.ai-%23d4a853)](https://lingtai.ai)
[![Discord](https://img.shields.io/badge/discord-join-%235865F2?logo=discord&logoColor=white)](https://discord.gg/8KBGVYMS)

</div>

---

Most agent tools give you a better one-shot worker: a chat window that forgets, or a coding agent that closes with the terminal. **LingTai is different — it is a Digital Scientist that lives in your project and gets better over time.** It holds a question or a codebase for weeks, works with evidence and tools, records what it learns as durable knowledge and reusable skills, forms its own operating style, and delegates deep sub-problems to specialists it spawns. The work you do together becomes state the next session starts from.

It is **filesystem-native, not a chat window.** Every agent has a home under `.lingtai/`; its durable state — mail, memory, knowledge, skills, logs, heartbeats — lives in local files and directories you can inspect with standard tools, your editor, or another coding agent. Close the terminal and the scientist persists: it can be inspected, restarted, taught, and recovered.

<div align="center">

<img src="docs/assets/network-demo.gif" alt="LingTai portal showing a live local network of long-lived project agents" width="100%">

</div>

## A day (and a month) with a Digital Scientist

```text
You
  "Hold this research question for me: does our solar-wind classifier
   drift across instruments? Read the literature and our data, run
   experiments, and keep me posted."

LingTai
  reads the literature with web search and research tools
  → inspects the datasets and the classifier code in the repo
  → runs experiments, verifies every claim against evidence
  → records findings in its durable knowledge library
  → spawns a specialist avatar to go deep on one instrument's calibration
  → over weeks, refines its own operating style and reusable skills
  → sends you a brief on Telegram / TUI / email with the artifacts
```

Nothing above is a one-off. The literature notes, the verified findings, the calibration specialist, the working style it settled on — all of it is durable. When you come back next week, the scientist resumes from that accumulated state instead of starting cold. The same loop serves engineering just as well: hold a codebase, reproduce a bug with evidence, patch it, and remember why.

## Why a lifelong, self-evolving scientist?

A good scientist is defined not only by results, but by the practice that produces them: **evidence over assumption, tools mastered deliberately, experiments recorded, findings reviewed and iterated.** LingTai turns that practice into a growth loop, backed by real files on disk:

- **Work produces experience.** Tasks use real tools when action is needed — shell, file I/O, web search, vision, coding-agent hands — and every assertion is expected to rest on evidence, not guesswork.
- **Experience is distilled into durable state.** When the context window fills, the agent *molts* (凝蜕 — "crystallize the essence, shed the chaff"): it saves what matters and resets the window. Across molts, that experience accumulates as four inspectable forms of growth —
  - **Knowledge** — its private library of accumulated research, findings, and notes.
  - **Skills** — reusable procedures it can invoke on demand and share with peers.
  - **Character** — its evolving operating style, expertise, and goals.
  - **Avatars** — persistent specialist agents it spawned to master one sub-problem, recorded in an append-only ledger.
- **Future work starts from that state.** The next session reloads character, knowledge, and skills — so the scientist is a little sharper each time, in a direction you can inspect and steer.

This is growth you can read and audit, not a black box. The loop is explicit, inspectable, and steerable; **you stay in charge of direction**, and external side effects (sending mail, filing issues) are treated as real actions that respect your authorization.

## Capabilities, as outcomes

- **Keeps a long-running question or project** — durable memory and goals survive sessions, restarts, and closing the terminal.
- **Works like a scientist** — evidence-first tool use, experiments, verified findings, and durable records you can review.
- **Grows its own toolkit** — distills what it learns into reusable skills and a private knowledge library.
- **Scales beyond one mind** — spawns persistent specialist *avatars* for deep sub-problems and lightweight *daemons* for temporary parallel work.
- **Reaches you where you are** — you talk to the same scientist through the TUI and external channels like Telegram, Feishu, WeChat, WhatsApp, and email, while the portal shows the network and history.
- **Stays inspectable and recoverable** — durable project state lives locally under `.lingtai/` as inspectable files, rather than trapped in a hosted chat transcript.

## Quick start

```bash
curl -fsSL https://lingtai.ai/install.sh | bash
mkdir my-project && cd my-project
lingtai-tui
```

The installer covers macOS, Linux, and WSL. It installs `lingtai-tui` and `lingtai-portal`. From there, **the TUI manages everything else** — on first run it creates `.lingtai/`, provisions its own Python runtime, walks you through model/preset setup, and starts one resident scientist for the project. To upgrade later, re-run the installer (or `lingtai-tui self-update`) and restart the TUI.

Native Windows/PowerShell is also available:

```powershell
irm https://lingtai.ai/install.ps1 | iex
```

This resolves the latest tagged release, verifies the Windows binary archive and the pinned kernel release against their published checksums, and installs both `lingtai-tui`/`lingtai-portal` and the Python runtime venv. Pass `-SkipVenv` to install the TUI/portal binaries only. See [`RELEASING.md`](RELEASING.md) for the exact contract.

> **New here?** Follow the step-by-step [tutorial at lingtai.ai](https://lingtai.ai/en/tutorial/) — install, first task, channels, memory, and lifecycle, walked through end to end.

> Homebrew (`brew install lingtai-ai/lingtai/lingtai-tui`) still works for existing users, but the one-line installer is the recommended path for new installs. The `lingtai` PyPI package is the Python runtime the TUI manages for you — reach for `pip` only when developing or diagnosing the kernel itself.

For deeper TUI/portal update operations, install-method detection, Homebrew, and mainland-China build routing, see the bundled [`lingtai-update` skill](tui/internal/preset/skills/lingtai-update/SKILL.md).

## Ways to work with it

**TUI — `lingtai-tui`** is the main human surface: setup, model/preset configuration, chat and mail, scientist status (token/context + heartbeat), and views into the durable state — `/knowledge` for its library, `/skills` for its skill catalog, `/system` for its character and covenant, `/daemons` for background runs, `/goal` to set a long-running goal. Type `/help` for the complete slash-command reference (the canonical catalog is the bundled [`lingtai-tui-help` skill](tui/internal/preset/skills/lingtai-tui-help/assets/slash-commands.en.md); this README does not duplicate it). Run `lingtai-tui doctor` if anything looks broken after an upgrade.

**Portal — `lingtai-portal`** is the visualization server. It reads project state to show the live agent network, mail edges, and history — useful once a project has more than one agent or when you want to see how the work evolved.

**External channels** bridge the *same* scientist to the platforms you already use — memory, tools, and history are shared across them, and they are doors into one assistant, not separate bots. Setup follows the current MCP/curated-addon documentation and requires explicit authorization; the TUI's `/mcp` panel is read-only and only inspects configured bridges and their status. Credentials live in local `.secrets/` files (never in Git); external side effects are treated as real actions, and unknown senders do not auto-receive replies.

| Addon | Use it for |
|---|---|
| `telegram` | Talk to your scientist from Telegram (DMs, optional allowlist, voice/file passthrough). |
| `feishu` | Feishu/Lark — WebSocket long connection, no public IP or webhook required. |
| `wechat` | WeChat through an iLink/gewechat-style bridge. |
| `whatsapp` | WhatsApp through the curated LingTai bridge. |
| `imap` | Real email through IMAP/SMTP — multi-account, with safety defaults for unknown senders. |

**Coding agents as hands.** Coding CLIs are capable hands for precise implementation, and LingTai is the mind around those hands — it owns the long-running plan, memory, and coordination. Supported coding CLIs (such as **Claude Code** and **Codex**) can run as daemon backends for focused implementation jobs; other agents can collaborate as peers through the shared `.lingtai/human/` mailbox protocol.

- **Claude Code** — `claude plugin add Lingtai-AI/claude-code-plugin`
- **OpenAI Codex CLI** — `git clone https://github.com/Lingtai-AI/codex-plugin.git && cd codex-plugin && ./install.sh`
- **Other agents** (OpenCode, OpenClaw, Hermes, …) — vendor the [`lingtai-skill`](https://github.com/Lingtai-AI/lingtai-skill) protocol skill under your tool's skills directory.

## Inspectable architecture

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
- Discord: <https://discord.gg/8KBGVYMS>
- Issues: <https://github.com/Lingtai-AI/lingtai/issues> · Discussions: <https://github.com/Lingtai-AI/lingtai/discussions>

For Chinese-language discussion and early testing, scan the WeChat QR below. Add the author on WeChat with the note `lingtai`; if the QR has expired, open an issue and we will refresh it.

<img src="docs/assets/wechat.png" alt="WeChat QR code for joining the LingTai testing group" width="200">

## License

Apache-2.0 — see [LICENSE](LICENSE).
