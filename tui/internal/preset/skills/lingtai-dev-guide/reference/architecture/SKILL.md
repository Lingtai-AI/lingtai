---
name: dev-guide-architecture
description: >
  Nested lingtai-dev-guide reference for the project architecture: Go TUI/portal monorepo, Python kernel, MCP addon repos, filesystem IPC, and where per-project/per-machine state lives.
version: 1.0.0
last_changed_at: "2026-06-02T00:05:51-07:00"
---

# Architecture


Nested lingtai-dev-guide reference. Read this after the top-level router sends you here.
This document maps the LingTai project: what the pieces are, how they connect, and where state lives.

## The two repos

### `lingtai` — Go monorepo (TUI + portal)

**Location:** `github.com/Lingtai-AI/lingtai`

Two binary targets in one repo:

| Binary | Source | Build output | Role |
|---|---|---|---|
| `lingtai-tui` | `tui/` | `tui/bin/lingtai-tui` | Terminal UI — Bubble Tea v2 + lipgloss v2. Agent launcher, monitor, mail viewer, preset editor, first-run wizard. |
| `lingtai-portal` | `portal/` | `portal/bin/lingtai-portal` | Web portal — Go HTTP server with embedded React 19 frontend. Network visualization, mail/replay UI, topology recorder. |

Key packages in `tui/internal/`:

| Package | Role |
|---|---|
| `tui/` | Bubble Tea models for every screen (~22k LOC) |
| `preset/` | Atomic `{llm, capabilities}` bundle layer |
| `migrate/` | Versioned, append-only migration system for `.lingtai/` state |
| `globalmigrate/` | Per-machine migrations under `~/.lingtai-tui/` |
| `fs/` | Filesystem read accessors into agent working directories |
| `config/` | Global TUI config under `~/.lingtai-tui/` |
| `process/` | Subprocess launcher for `python -m lingtai run <dir>` |
| `postman/` | UDP/IPv6 cross-internet agent mesh |
| `i18n/` | en/zh/wen JSON tables (three locales always) |

Key packages in `portal/internal/`:

| Package | Role |
|---|---|
| `api/` | HTTP server, handlers, replay endpoint |
| `fs/` | Filesystem accessors (same shape as TUI's, portal-tailored) |
| `migrate/` | Versioned migrations (shares `meta.json` version space with TUI) |
| `web/` | React 19 + TypeScript + Vite frontend (embedded into Go binary) |

### `lingtai-kernel` — Python kernel

**Location:** `github.com/Lingtai-AI/lingtai-kernel`

Published as the `lingtai` package on PyPI. Contains:

- `src/lingtai_kernel/` — the minimal agent runtime (turn loop, lifecycle, tool dispatch, mailbox, soul/molt orchestration)
- `src/lingtai/` — the batteries-included wrapper (MCP, FileIO, Vision, Search, CLI)

The wrapper depends on the kernel one-directionally. The kernel never imports from the wrapper.

## How they connect

```
┌─────────────────────────────────────────────────────┐
│                    lingtai (Go)                      │
│                                                     │
│  ┌─────────────┐    ┌──────────────┐               │
│  │  lingtai-tui │    │ lingtai-portal│              │
│  │  (terminal)  │    │  (web)       │               │
│  └──────┬───────┘    └──────┬───────┘               │
│         │                   │                        │
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
│  └─────────────────────────────────┘                 │
└──────────────────────────────────────────────────────┘
```

**TUI → kernel:** The TUI launches agents via `python -m lingtai run <dir>` as a subprocess (`tui/internal/process/launcher.go`). After spawn, the TUI never talks to the agent process directly — only via the agent's working directory.

**TUI → filesystem (read):** `.agent.json`, `.agent.heartbeat`, `mailbox/`, `logs/token_ledger.jsonl`, `history/chat_history.jsonl`, `system/*.md`, `.notification/*.json`.

**TUI → filesystem (write):** Signal files only: `.sleep`, `.suspend`, `.interrupt`, `.clear`, `.prompt`, `.refresh`, `.inquiry`, `.forget`. Plus `init.json` via explicit user actions.

**TUI ↔ Homebrew tap:** Pushing a release tag runs the root release workflow, which updates `Lingtai-AI/homebrew-lingtai/lingtai-tui.rb`.

**Portal ↔ TUI:** The TUI discovers an installed `lingtai-portal` to launch on `/viz`; otherwise the binaries are independent.

## Cross-repo dependencies

| Repo | Relationship to `lingtai` |
|---|---|
| `lingtai-kernel` | Runtime dependency only (the Python agent the TUI launches). Not a build-time dependency. |
| `lingtai-skill` | Canonical mailbox-protocol `SKILL.md`. Vendored into plugin repos. |
| `lingtai-claude-code` | Claude Code plugin (SessionStart hook, marketplace manifest). |
| `codex-plugin` | OpenAI Codex CLI plugin. |
| `lingtai-imap` / `lingtai-telegram` / `lingtai-feishu` / `lingtai-wechat` | MCP server addons. Each is a separate PyPI package. |
| `Lingtai-AI/homebrew-lingtai` | Homebrew tap for `lingtai-tui`. |

## Where state lives

### Per-project state: `<project>/.lingtai/`

```
.lingtai/
├── meta.json                    # migration version stamp (shared TUI + portal)
├── <agent>/
│   ├── init.json                # agent's preset manifest
│   ├── .agent.json              # written by agent, read by TUI/portal
│   ├── .agent.heartbeat         # liveness signal
│   ├── .status.json             # agent status
│   ├── mailbox/                 # filesystem mailbox
│   │   ├── inbox/
│   │   ├── outbox/
│   │   ├── sent/
│   │   └── archive/
│   ├── .notification/           # notification producer files
│   │   ├── email.json
│   │   ├── soul.json
│   │   └── system.json
│   ├── logs/                    # token ledger, events
│   ├── history/                 # chat history, snapshots
│   ├── system/                  # pad, summaries, config fragments
│   ├── .library/                # skill library
│   └── delegates/               # avatar ledger
├── human/                       # user's pseudo-agent (no admin, no heartbeat)
├── .tui-asset/                  # TUI-owned per-project caches
└── .portal/                     # portal-owned files (port, recordings)
```

### Per-machine state: `~/.lingtai-tui/`

```
~/.lingtai-tui/
├── meta.json                    # global migration version stamp
├── tui_config.json              # global TUI preferences
├── runtime/venv/                # Python venv with `lingtai` installed
├── presets/
│   ├── templates/               # TUI-owned, rewritten on Bootstrap
│   └── saved/                   # user-owned, Bootstrap never touches
├── utilities/                   # optional library paths for agents
└── ...
```

## Filesystem-only IPC

The TUI and portal never open a socket or RPC channel to a running agent. All communication is through files: agent manifests, heartbeats, signal files, mailbox folders, `.notification/`. This is a deliberate design choice — any new cross-process communication should follow the same pattern: write a file, let the other side poll.
