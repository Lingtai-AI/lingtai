# TUI Design Spec

## Overview

A minimalist terminal UI for the lingtai agent framework. Go + Bubble Tea. Three screens plus a setup wizard sub-system.

## Core Design Principle: One Admin Agent

Each `~/.lingtai/` directory has exactly **one admin agent**. The admin is the only agent the human talks to directly. All other agents are avatars spawned by the admin (or by other avatars), inheriting the admin's full config minus admin privileges.

The human does not launch agents from the TUI. The admin spawns avatars via the avatar capability. The TUI monitors all agents (admin + avatars) and allows the human to quell or purge them.

## Architecture

```
Home (Agents)
├── Chat overlay (Esc back) — always with admin agent
└── Settings overlay (Esc back)
    └── Setup Wizard (multi-page, returns to Settings)
```

No tabs, no tab bar. Home is the hub. Chat and Settings are full-screen overlays entered via hotkey, Esc to return.

## Screen 1: Home (Agents)

The default screen. Lists the admin agent and all its avatars with live status.

```
  Name            Role          Status      Uptime
  ─────────────────────────────────────────────────
  agent-a3f2c1    Admin         ● Active    2h 13m
  agent-8b1e4d    Avatar        ● Active    45m
  agent-c7d9a0    Avatar        ○ Dormant   —

  Enter: actions | C: chat | S: settings | Q: quit
```

### Status Detection

Scan `~/.lingtai/` for directories containing `.agent.json`. Check `.agent.heartbeat` file freshness (< 2 seconds old = Active, otherwise Dormant).

### Action Menu

Press Enter on a selected agent to open:

```
  ┌─────────────┐
  │ Details     │  → show agent info (dir, uptime, config summary)
  │ Quell       │  → touch .quell file, with Y/N confirmation
  │ Purge       │  → rm -rf agent dir, with Y/N confirmation
  └─────────────┘
```

Navigate with Up/Down, Enter to select, Esc to close.

- **Quell** is available for any agent (admin or avatar).
- **Purge** removes the agent's working directory entirely. For avatars only — the admin cannot be purged from this menu.

### No Launch

The human does not launch agents. The admin agent spawns avatars via the avatar capability. Avatars inherit the admin's full config (LLM, capabilities, multimodal) automatically.

### First Run

On first run (no admin agent exists in `~/.lingtai/`), the TUI opens the Setup Wizard automatically. After the wizard saves the config, the TUI generates `init.json` in `~/.lingtai/`, starts the admin agent process (`lingtai run ~/.lingtai/{agent_id}/`), and returns to Home.

## Screen 2: Chat

Full-screen overlay. Entered via `C` hotkey from Home. Always connected to the admin agent — no agent selection needed.

```
  Chat with Admin                               [Esc: back]
  ─────────────────────────────────────────────────────────

  [agent] Hello, I'm ready to help.            14:23:01
  [you]   What's the status of the project?    14:23:15
  [agent] I've been analyzing the codebase...  14:23:18

  > _
```

### Behavior

- Based on existing ChatModel implementation
- Mail polling from admin agent's `mailbox/inbox/` via MailPoller
- Mail sending to admin via MailWriter
- Human participant uses `human-tui` working dir in `~/.lingtai/`
- Contact exchange between human and admin on first connection
- Verbose mode toggle (Ctrl+O) for JSONL event streaming
- No agent cycling, no `/connect` command — always admin
- Esc returns to Home

## Screen 3: Settings

Full-screen overlay. Entered via `S` from Home.

```
  Settings
  ─────────────────────────
  Language:   ◀ English ▶       ← Left/Right to cycle

  [Enter Setup Wizard]          ← Enter to open wizard sub-system

  Esc: back
```

Minimal. Future settings added here as needed.

## Sub-system: Setup Wizard

Entered from Settings. Multi-page flow that configures the admin agent and saves the config as a combo (for backup/portability).

### Step Flow

```
Lang → Combo → QuickStart → Model → Multimodal → Messaging → General → Review
                   │                                   ▲
                   │ (MiniMax Coding Plan)              │
                   └───────────────────────────────────┘
                          skips Model + Multimodal
```

- **Lang**: wizard language (may differ from TUI language)
- **Combo**: load existing combo or create new
- **QuickStart**: MiniMax Coding Plan (1 API key + endpoint) or Configure Manually
- **Model**: LLM provider / model / API key / endpoint
- **Multimodal**: capability grid — Manual Configuration or Skip
- **Messaging**: IMAP / Telegram addons
- **General**: agent defaults (port, language, etc.)
- **Review**: confirmation, save combo, generate/update admin `init.json`

### MiniMax Coding Plan Shortcut

Detailed in `docs/superpowers/specs/2026-03-23-minimax-coding-plan-shortcut-design.md`. Summary: single API key + endpoint selector fills main LLM config and all multimodal capabilities, skips Model + Multimodal steps.

### Navigation

- Esc: back to previous step (Esc at first step returns to Settings)
- Enter: confirm and advance
- Tab: cycle fields within a step

### Combo Storage

Combos saved to `~/.lingtai/combos/` as JSON files. The existing `combo.Combo` struct and `Load`/`Save` functions handle persistence.

### Avatar Config Inheritance

Avatars spawned by the admin inherit the admin's full config: LLM provider/model/key, all capabilities, multimodal settings. This is handled by the avatar capability in the Python agent runtime, not by the TUI. The TUI only configures the admin; avatars are automatic.

## Key Bindings Summary

### Home
| Key | Action |
|-----|--------|
| Up/Down | Navigate agent list |
| Enter | Open action menu on selected agent |
| C | Open Chat with admin |
| S | Open Settings |
| Q | Quit TUI |

### Action Menu
| Key | Action |
|-----|--------|
| Up/Down | Navigate options |
| Enter | Select action |
| Esc | Close menu |

### Chat
| Key | Action |
|-----|--------|
| Esc | Return to Home |
| Enter | Send message |
| Ctrl+O | Toggle verbose mode |

### Settings
| Key | Action |
|-----|--------|
| Up/Down | Navigate options |
| Left/Right | Cycle language (when focused) |
| Enter | Enter Setup Wizard (when focused) |
| Esc | Return to Home |

## What Exists vs. What's New

| Component | Status |
|-----------|--------|
| Setup Wizard | Exists in `tui/internal/setup/wizard.go`, needs MiniMax shortcut |
| Agent status list | Exists in `tui/internal/tui/status.go`, needs action menu |
| Chat | Exists in `tui/internal/tui/app.go`, needs simplification (remove agent cycling) |
| Root routing | Exists in `tui/internal/tui/root.go`, needs restructuring |
| Settings screen | **New** |
| Action menu | **New** |
| First-run detection + admin auto-start | **New** |

## Existing Code Changes

### `tui/internal/tui/root.go`
- Replace `ViewStatus`/`ViewChat`/`ViewWizard` with `ViewHome`/`ViewChat`/`ViewSettings`/`ViewWizard`
- `ViewChat` and `ViewSettings` are overlays (Esc returns to `ViewHome`)
- `ViewWizard` entered from `ViewSettings`, returns to `ViewSettings` on completion
- Add first-run detection: if no admin agent exists, auto-enter `ViewWizard`

### `tui/internal/tui/status.go`
- Rename to home screen role
- Remove `L` (language cycling) and `S` (setup wizard) hotkeys — replaced by `C` (chat) and `S` (settings)
- Remove `K` (quell all) — replaced by per-agent quell via action menu
- Add action menu (Enter on agent → Details/Quell/Purge)
- Add "Role" column (Admin vs Avatar)

### `tui/internal/tui/app.go`
- Remove `cycleNextSpirit()` and Tab agent cycling
- Remove `/connect` command
- Hardwire to admin agent (no agent selection)

### `tui/internal/i18n/strings.go`
- Add keys for Settings screen, action menu options, confirmation prompts
- Add MiniMax QuickStart keys (per existing shortcut spec)
- Remove obsolete quick-setup keys

## Deferred

- **Network topology tree** — agent spawn relationship visualization.
- **Customize page** — per-agent config editing outside the wizard.

## Tech Stack

- Go, module `lingtai-tui`
- Bubble Tea TUI framework
- Lipgloss styling (existing `styles.go`)
- i18n: 3 locales — en, zh, lzh (existing `i18n/strings.go`)
