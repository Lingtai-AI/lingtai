# Unified TUI — Spec

## Problem

The current CLI has the wizard and chat as separate bubbletea programs stitched together in main.go. This creates awkward transitions, no way to reconfigure without restarting, and no status overview. Additionally, avatar spawning uses free-text provider/model fields which can fail if keys aren't configured.

## Design

### Single Bubbletea App, Three Views

```
┌─────────────────────────────────────────┐
│               lingtai TUI               │
│                                         │
│  ┌──────────┐  ┌──────┐                │
│  │  Status   │  │ Chat │                │
│  │  (home)   │  │      │                │
│  └──────────┘  └──────┘                │
│       ↕                                 │
│  ┌──────────┐                           │
│  │  Wizard  │                           │
│  │ (setup)  │                           │
│  └──────────┘                           │
└─────────────────────────────────────────┘
```

### View 1: Status (Home)

The landing page. Shows all agents in this project's `.lingtai/`.

```
  灵台 LingTai

  Agents:
    ● orchestrator (本我)    active    gemini-pro     :8501
    ● researcher             active    claude-work    :8502
    ○ analyst                idle      gemini-pro     :8503
    ✗ writer                 dead      —

  [Enter] Chat   [S] Setup   [K] Kill all   [Q] Quit
```

- Shows all agents with status (active/idle/dead), combo name, and port
- Combo name read from `combo.json` in each agent's working dir
- **Setup is always accessible** — combo change takes effect on next start/restart
- **Enter** starts 本我 if stopped, then transitions to Chat
- Agent list is the topology — no separate topology view needed

### View 2: Wizard (Setup)

The existing wizard (StepCombo → StepLang → StepModel → ... → StepReview).

- Accessible from Status via `[S]`
- On completion, writes config files, transitions back to Status
- First-time launch (no `.lingtai/`) goes directly to Wizard

### View 3: Chat

The existing chat TUI (text input, message history, verbose mode).

- Accessible from Status via `[Enter]`
- If 本我 is stopped, starts it first
- `Esc` returns to Status (agent keeps running)

### Key Transitions

| From | To | Trigger | Action |
|------|-----|---------|--------|
| (launch) | Wizard | no `.lingtai/` | — |
| (launch) | Status | `.lingtai/` exists | scans agent dirs |
| Status | Chat | Enter | starts 本我 if stopped |
| Status | Wizard | S | — |
| Status | (quit) | Q / Ctrl+C | stops all agents |
| Wizard | Status | setup completes | — |
| Chat | Status | Esc | agent keeps running |

---

## Combo-Based Avatar Spawning

### Current (free-text)

```json
{
  "name": "researcher",
  "provider": "anthropic",
  "model": "claude-sonnet-4.6"
}
```

Problem: arbitrary provider/model strings can fail if keys aren't configured.

### New (combo-based)

```json
{
  "name": "researcher",
  "combo": "claude-work"
}
```

- **Default**: omit `combo` → inherit parent's combo
- **Optional**: pick from `~/.lingtai/combos/` if they exist
- **If no combos exist** in `~/.lingtai/combos/`: only parent's own is available, `combo` field hidden from schema
- **If combos exist**: schema shows `combo` as an enum of available combo names

### Combo Resolution at Spawn

1. Avatar tool called with `combo="claude-work"` (or omitted)
2. If omitted, use parent's combo (read from parent's `combo.json`)
3. If specified, load from `~/.lingtai/combos/<name>.json`
4. Extract provider, model, api_key_env from combo
5. Resolve API key from combo's env section (set in process env)
6. Pass resolved provider/model/key to child Agent constructor
7. Copy combo.json to child's working dir

### Per-Agent combo.json

Each agent's working dir gets a `combo.json` — a copy of the combo used at creation.

```
.lingtai/
  configs/
    config.json
    model.json
    .env
  <agent_id>/          ← 本我
    combo.json         ← copy of combo used at creation
    covenant.md
    .agent.json
  <agent_id>/          ← 他我
    combo.json         ← may differ from 本我's
    covenant.md
    .agent.json
```

The Status page reads `combo.json` from each agent dir to display the combo name column.

### Avatar Tool Schema Changes

**Old schema properties:**
```json
{
  "provider": { "type": "string", "enum": [...providers...] },
  "model": { "type": "string" }
}
```

**New schema properties:**
```json
{
  "combo": { "type": "string", "enum": [...combo names...] }
}
```

`_build_schema()` reads available combos from `~/.lingtai/combos/`. If none exist, the `combo` field is omitted entirely (parent's combo is the only option).

### What Changes

- `avatar.py` — `_spawn()` resolves combo → provider/model/key, copies combo.json to child dir
- `avatar.py` — `get_schema()` / `_build_schema()` replaces provider/model with combo enum
- `avatar.py` — `AvatarManager.__init__()` loads parent's combo.json for default
- `i18n/{en,zh,wen}.json` — replace `avatar.provider`/`avatar.model` keys with `avatar.combo`
- `app/__init__.py` — copy combo.json to 本我's working dir at startup

### What Stays

- Agent/BaseAgent constructor still takes provider/model internally (combo is resolved before construction)
- LLMService adapter registry unchanged
- Combo files at `~/.lingtai/combos/` unchanged

---

## Architecture

### Root Model

```go
type RootModel struct {
    view       View
    status     StatusModel
    wizard     *setup.WizardModel   // embedded from setup package
    chat       *ChatModel

    config     *config.Config
    proc       *agent.Process       // nil if not running

    lingtaiDir string               // .lingtai/ path
}

type View int
const (
    ViewStatus View = iota
    ViewWizard
    ViewChat
)
```

### File Structure

```
daemon/internal/tui/
    root.go         — RootModel, view routing, transitions
    status.go       — StatusModel (agent list, combo display)
    chat.go         — ChatModel (existing app.go, refactored)
```

The wizard model stays in `daemon/internal/setup/wizard.go` — exported and embedded in RootModel.

### Agent Status Discovery

The Status page scans `.lingtai/` for agent working dirs:
1. Find all directories containing `.agent.json`
2. Read `.agent.json` for agent_id, agent_name, port, status
3. Read `combo.json` for combo name
4. Check if process is alive (PID from agent.pid)
5. Display in table

---

## Summary of All Changes

| Area | Change |
|------|--------|
| **TUI** | Single bubbletea app with Status/Wizard/Chat views |
| **Status page** | Shows all agents with combo name, status, port |
| **Avatar spawning** | Combo-based instead of free-text provider/model |
| **Per-agent combo.json** | Each agent dir gets a copy of its combo |
| **Schema** | `provider`/`model` fields → `combo` field |
| **main.go** | Just creates RootModel and runs one tea.Program |
| **setup/wizard.go** | Export model for embedding in TUI |
| **tui/app.go** | Rename to chat.go, extract as sub-model |
