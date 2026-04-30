# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What is 灵台

灵台 (Língtái) is a generic agent framework — an "agent operating system" providing the minimal kernel for AI agents: thinking (LLM), perceiving (vision, search), acting (file I/O), and communicating (inter-agent email). Domain tools, coordination, and orchestration are plugged in from outside via MCP-compatible interfaces.

Named after 灵台方寸山 — where 孙悟空 learned his 72 transformations. Each agent (器灵) can spawn avatars (分身) that venture into 三千世界 and return with experiences. The self-growing network of avatars IS the agent itself — memory becomes infinite through multiplication.

### Repository Scope

All Python code (kernel runtime and batteries-included wrapper) now lives in the `lingtai-kernel` repo and is published as the `lingtai` package on PyPI. This repo contains only the Go TUI and portal frontends.

- **`tui/`** — Terminal UI (Go + Bubble Tea). Launches and monitors agents from the command line. Builds to `tui/bin/lingtai-tui`.
- **`portal/`** — Web portal (Go + embedded web frontend). Provides a browser-based interface. Builds to `portal/bin/lingtai-portal`.

Neither binary has a direct Python dependency. Both communicate with Python agents exclusively through the filesystem (`.lingtai/` directory, heartbeat files, signal files). Agents are launched by the TUI via `python -m lingtai run <dir>` as a subprocess.

### Sibling repos (not in this directory)

Other LingTai components live as sibling repos under `~/Documents/GitHub/`:

- **`lingtai-kernel/`** — Python kernel + `lingtai` PyPI package (agent runtime, LLM interface, mailbox core, preset schema validation).
- **`lingtai-skill/`** — Canonical mailbox-protocol skill. Single source of truth for `skills/lingtai/SKILL.md` (the filesystem mailbox protocol). Plugin repos vendor it via `lingtai-claude-code/scripts/sync-from-canonical.sh` — edit here, sync there.
- **`lingtai-claude-code/`** — Claude Code plugin (`claude plugin add Lingtai-AI/claude-code-plugin`). Owns the SessionStart hook and the Claude marketplace manifest. The plugin cache at `~/.claude/plugins/cache/lingtai/` gets overwritten on update — don't edit that.
- **`codex-plugin/`** — OpenAI Codex CLI plugin (`./install.sh` copies into `~/.codex/skills/lingtai/` and `~/.codex/hooks.json`).
- **MCP addon repos** — sibling MCP servers wired in via the kernel's `mcp` capability. Each is a thin adapter exposing one external system as a tool surface plus a LICC inbox listener:
  - **`lingtai-imap/`** — Gmail/IMAP mail
  - **`lingtai-telegram/`** — Telegram bot API
  - **`lingtai-feishu/`** — Feishu/Lark messaging
  - **`lingtai-wechat/`** — WeChat (gewechat) messaging
- **`lingtai-fangcun/`** — standalone skill/tool component.
- **`lingtai-agora/`**, **`lingtai-web/`** — distribution and web surfaces.
- **`lingtai-ad/`** — launch/marketing materials.

## Build

```bash
# Build the TUI
cd tui && make build
# Output: tui/bin/lingtai-tui

# Build the portal (builds embedded web frontend first)
cd portal && make build
# Output: portal/bin/lingtai-portal
```

Cross-compilation targets (darwin/linux/windows, amd64/arm64) are available via `make cross-compile` in each directory.

## Releases

See `RELEASING.md` for the full process. Key points:

1. Tag and push: `git tag v0.X.Y && git push origin v0.X.Y`
2. Create GitHub release: `gh release create v0.X.Y --title "v0.X.Y" --notes "..."` (no binary assets — Homebrew builds from source)
3. Update Homebrew tap: bump version and sha256 in `huangzesen/homebrew-lingtai/lingtai-tui.rb`, commit and push

## Projects

### TUI (`tui/`)

Go + Bubble Tea terminal interface. Key facts:

- Binary name: `lingtai-tui` (never `lingtai` — that is the Python agent CLI)
- Launches agents via `python -m lingtai run <dir>` subprocess
- Communicates with running agents via filesystem only: reads `.lingtai/` metadata, heartbeat files, and signal files inside each agent working directory
- Agent discovery uses `lingtai_kernel.handshake` conventions (`is_agent`, `is_alive` checks on working directories)

### Migrations (`tui/internal/migrate/`)

Versioned, append-only, forward-only migration system. Each migration is a file `m<NNN>_<name>.go` exporting a function `func migrate<Name>(lingtaiDir string) error`. Register it in `migrate.go` by appending to the `migrations` slice and bumping `CurrentVersion`. Migrations run once per project at TUI launch (version tracked in `.lingtai/meta.json`). They can read global state (`globalTUIDir()` helper) but receive the project's `.lingtai/` dir as input. Print warnings directly with `fmt.Println` — no i18n needed since migrations run before the TUI renders.

**IMPORTANT: The TUI and portal share the same `meta.json` version space but have separate migration registries.** When adding migrations to the TUI, you MUST also bump `CurrentVersion` in `portal/internal/migrate/migrate.go`. Migrations that touch shared on-disk state (init.json schema, preset paths, etc.) should be implemented in BOTH packages with identical logic — copy the file across. TUI-only migrations (assets, recipe state, anything portal doesn't read) get a no-op stub `Fn: func(_ string) error { return nil }` in the portal registry to preserve the version slot. Otherwise the portal refuses to open any project the TUI has already touched.

Recent migrations worth knowing about:
- **m029** — `manifest.preset.path` (directory the kernel scanned) → `manifest.preset.allowed` (explicit list of allowed preset path strings). Schema is now declarative.
- **m030** — preset directory split: rewrites flat `~/.lingtai-tui/presets/X.json` paths in init.json to either `presets/templates/X.json` or `presets/saved/X.json` (see "Preset architecture" below).

### Global migrations (`tui/internal/globalmigrate/`)

Per-machine analogue of `tui/internal/migrate/`. Same conventions (versioned, append-only, forward-only, `m<NNN>_<name>.go` files registered in `globalmigrate.go`), but scoped to global state under `~/.lingtai-tui/`. Version tracked in `~/.lingtai-tui/meta.json`. Use this for cleanup that affects the whole machine rather than a single project — e.g. Homebrew tap rename, `tui_config.json` schema bumps, runtime dir relocation, the preset directory split (m002). Failures are best-effort: they go to stderr and don't abort startup. Run from `main.go` via `globalmigrate.Run(globalDir)`.

### Preset architecture (`tui/internal/preset/`)

Presets are atomic `{llm, capabilities}` bundles agents can swap into at runtime. The on-disk layout under `~/.lingtai-tui/presets/`:

- **`templates/`** — TUI-owned. `RefreshTemplates()` rewrites this directory wholesale on every Bootstrap from embedded data, prunes retired entries. Users should never edit files here directly; an upgrade will erase their changes.
- **`saved/`** — user-owned. `Save()` always writes here; Bootstrap never touches it. The wizard's auto-clone-on-edit lands new presets here as `<template>-<N>.json`.

The directory IS the answer to "is this a template?" — there's no in-band marker. Each loaded `Preset` carries a `Source` field (`SourceTemplate` / `SourceSaved`) set by `List()` / `Load()`. Prefer `IsTemplate(p)` over the legacy `IsBuiltin(p.Name)` for any loaded preset; the latter still exists for callers that only have a name.

`manifest.preset` schema in init.json:
```json
"preset": {
  "default": "~/.lingtai-tui/presets/templates/minimax.json",
  "active":  "~/.lingtai-tui/presets/templates/minimax.json",
  "allowed": [
    "~/.lingtai-tui/presets/templates/minimax.json",
    "~/.lingtai-tui/presets/saved/zhipu-1.json"
  ]
}
```

Authorization is declared in `allowed` — the kernel refuses runtime swap to anything not in that list (`system.py:_refresh` allowed-gate). Path normalization in the gate uses `_preset_ref_in` so `~/`-prefixed and absolute forms compare equal. `default` and `active` MUST both appear in `allowed`; `init_schema.validate_init` enforces this.

When writing `manifest.preset.*` paths from Go code, always use `preset.RefFor(p)` — it picks the right subdirectory based on `Source` instead of hardcoding `presets/`.

### Portal (`portal/`)

Go server with an embedded web frontend. Key facts:

- Binary name: `lingtai-portal`
- Web assets are built with `npm run build` inside `portal/web/` and embedded into the Go binary at compile time via `embed.go`
- Communicates with agents via filesystem only (same conventions as TUI)
- `make build` runs the full pipeline: web deps → web build → go build

## TUI patterns and gotchas

These are concrete patterns that have caused regressions when missed. Treat each as a checklist item before commits in the relevant area.

### Bubble Tea v2: paste delivery

Bubbletea v2 delivers terminal events as separate message types: `tea.KeyPressMsg` for individual keypresses, `tea.PasteMsg` for clipboard blobs from bracketed-paste mode, `tea.MouseMsg`, etc. **Any `Update` dispatcher that handles `case tea.KeyPressMsg:` (or v1's `tea.KeyMsg`) needs a fall-through that forwards remaining message types to whichever text widget is focused.** Otherwise paste silently drops.

For embedded sub-models (e.g. `PresetEditorModel` hosted inside `FirstRunModel`), the host's outer dispatcher must forward paste msgs *into* the sub-model. If the host's `default:` branch enumerates focused widgets per-step but misses the step that hosts an embedded model, paste dies before reaching the sub-model — and adding a fall-through inside the sub-model's own `Update` is useless because the msg never arrives. Trace top-down (`tea.Program` → host's outer switch → sub-model's outer switch → widget) and ensure every layer either handles or forwards `tea.PasteMsg`.

Symptom of a missed forwarder: typing into an input works, pasting does nothing.

### Bubble Tea v2: `textarea` vs `textinput`

`textinput` is single-line and drops characters on multi-byte / clipboard pastes. `textarea` is the multi-line cousin that handles paste cleanly via its `pasteCmd` path. **For any paste-friendly field (API keys, base URLs, anything the user might paste from a browser), use `textarea` even when the content is conceptually one line:**

```go
ta := textarea.New()
ta.CharLimit = 512
ta.SetWidth(50)
ta.SetHeight(1)
ta.ShowLineNumbers = false
ta.Prompt = ""
ta.KeyMap.InsertNewline.SetKeys() // single-line: no newline insertion
ta.SetStyles(themedTextareaStyles())
```

`themedTextareaStyles()` is in the `tui` package — always apply it. A bare `textarea.New()` ships dark default cursor/focus colors that render as a black smear against the warm LingTai theme. Before adding any new `textarea.New` instance, grep the package for existing instances and copy their style + keymap setup.

### i18n: three locales always

Adding a new i18n key means updating **all three** of `tui/i18n/en.json`, `tui/i18n/zh.json`, `tui/i18n/wen.json`. Missing translations show as the raw key (`firstrun.preset_cfg.hint`) on screen — they don't fall back. If a key is genuinely English-only (procedural notes, error stacks), document why; otherwise translate.

### TUI binary

The TUI binary is `lingtai-tui`, **never** `lingtai`. `lingtai` is the Python agent CLI (installed by the kernel into the TUI's runtime venv at `~/.lingtai-tui/runtime/venv/bin/lingtai`). Build the TUI to `tui/bin/lingtai-tui`; never to `tui/bin/lingtai`.

### Agent venv

Agents run inside the TUI's runtime venv at `~/.lingtai-tui/runtime/venv/`. When making kernel changes (`lingtai-kernel/src/lingtai/...`), the editable install in this venv picks them up automatically — no reinstall needed. To verify, `~/.lingtai-tui/runtime/venv/bin/python -c "import lingtai; print(lingtai.__file__)"` should resolve to the kernel source tree, not site-packages.
