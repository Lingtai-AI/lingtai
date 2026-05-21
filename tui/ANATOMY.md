# tui — the `lingtai-tui` binary

> **Maintenance:** see `lingtai-tui-anatomy` (at `tui/internal/preset/skills/lingtai-tui-anatomy/SKILL.md`).

This folder is the self-contained Go module for the `lingtai-tui` terminal UI binary. It ships as a single executable built from `main.go` with platform-specific companions, embedding the i18n tables. All substantive logic lives under `internal/`. The binary has two faces: a subcommand surface (`purge`, `list`, `clean`, `suspend`, `timemachine`, `postman`, `bootstrap`, `presets`, `spawn`, `doctor`) and an interactive Bubble Tea v2 UI that launches Python agents as subprocesses and observes them via the filesystem.

## Components

- **`main.go:28-975`** — single-file `package main`. The version stamp (`main.go:29`, set via `-ldflags`), welcome/help text, and interactive entry (`main.go:31-360`). After parsing subcommands, it runs global migrations, checks invariants (init.json all-or-nothing, exactly-one-orchestrator), handles upgrade prompts and first-run wizard routing, then launches Bubble Tea.
- **`main.go:31-91`** — subcommand dispatch. Each subcommand returns early; the fallthrough path starts the interactive TUI.
- **`upgrade.go`** — startup TUI binary upgrade flow: detects newer Homebrew releases, asks about other running TUI windows, puts affected agents to sleep, stops old TUI processes, runs `brew upgrade`, and asks the user to relaunch.
- **`main.go:763-837`** — `cleanMain`: suspend agents in `.lingtai/` (10s timeout), then `os.RemoveAll`.
- **`main.go:839-887`** — `postmanMain`: parse `--port`, collect watch directories, call `postman.Run`.
- **`purge_unix.go:17-122`** — `purgeMain` (unix): `ps aux | grep "lingtai run"`, SIGTERM → SIGKILL survivors. Build tag `!windows`.
- **`purge_windows.go`** — `purgeMain` (windows): equivalent via Windows process enumeration.
- **`list_unix.go:15-161`** — `listMain` (unix): enumerate running agents with uptime, phantom detection (`.lingtai/` deleted but process still running). Build tag `!windows`.
- **`list_windows.go`** — `listMain` (windows): equivalent.
- **`suspend_unix.go:14-86`** — `suspendMain` (unix): discover agents via `.agent.json`, write `.suspend` files, wait 5s. Build tag `!windows`.
- **`suspend_windows.go`** — `suspendMain` (windows): equivalent.
- **`agent_count_unix.go:16-38`** — `countRunningAgents` (unix): lightweight `ps aux` scan used by `maybeShowAgentCount`. Build tag `!windows`.
- **`agent_count_windows.go`** — `countRunningAgents` (windows): equivalent.
- **`tui_process_unix.go` / `tui_process_windows.go`** — platform helpers for detecting and stopping other running `lingtai-tui` binaries before an in-app Homebrew upgrade.
- **`Makefile:1-23`** — build, dev (fast local), cross-compile (darwin/linux × arm64/amd64), clean. Version stamp via `-ldflags "-X main.version=$(VERSION)"` where `VERSION` is `git describe --tags --always`.
- **`i18n/i18n.go:10`** — `//go:embed en.json zh.json wen.json`. The only embed target in the root `tui/` package; all other embeds are in `internal/preset/`.
- **`tui/internal/`** — all substantive packages (tui screens, preset engine, migration system, filesystem readers, process launcher, headless JSON CLI surface, postman, timemachine, lock shims).
- **`tui/internal/headless/`** — JSON-emitting non-interactive surface. `RunPresets` (lists templates/saved presets as JSON), `RunSpawn` (creates a project + launches an agent), and `ExitError` (structured error codes). Wired from `main.go` via `bootstrapMain` (`main.go:895`), `presetsMain` (`main.go:905`), and `spawnMain` (`main.go:929`). For agents and scripts that drive `lingtai-tui` without the Bubble Tea UI.

## Connections

- **Called from:** the shell (`lingtai-tui`), Homebrew tap (`lingtai-ai/lingtai/lingtai-tui`), `install.sh`.
- **Calls out:** `tui/internal/tui` (Bubble Tea app), `tui/internal/migrate` (per-project migrations), `tui/internal/globalmigrate` (per-machine migrations), `tui/internal/preset` (bootstrap + utility skill population), `tui/internal/process` (agent launch), `tui/internal/config` (global config, venv, upgrade checks), `tui/internal/postman` (mail relay daemon), `tui/internal/timemachine` (git history daemon).
- **Bootstrap sequence** (`main.go:277-295`): on every launch, the TUI runs `config.MigrateLegacyLanguage` → `config.NeedsVenv` (for setup banner) → `config.EnsureRuntime` (create/repair venv if needed, then always run the non-blocking `CheckUpgrade`) → `config.EnsureAddons` → `preset.Bootstrap` → `tui.ExportCommandsJSON`. `CheckUpgrade` auto-upgrades the `lingtai` meta-package from PyPI, which bundles `lingtai-kernel` + all addon MCPs. See `tui/internal/config/ANATOMY.md`.
- **`lingtai-tui doctor` subcommand** (`main.go:911-964`): runs `config.RunDoctorUpdate` with both `ForceTUI=true` and `ForcePython=true`, then refreshes presets, utility skills, and `commands.json`. Designed to be usable when the TUI cannot start (broken venv, missing migrations) — it never touches `.lingtai/`. Exit nonzero only on unrecoverable failures.
- **Version flow:** `Makefile:4` injects `git describe` into `main.go:29`. On startup, `main.go:131` calls `tui.SetTUIVersion(version)`, which stores it for `/doctor` drift detection.
- **TUI binary upgrade:** `main.go:100-107` delegates newer-release handling to `upgrade.go`, which detects other running TUI binaries before running Homebrew; with confirmation, it puts agents in their projects to sleep, stops those TUI processes, upgrades, then asks the user to relaunch.
- **i18n loading:** `i18n/i18n.go` embeds the three locale JSONs; `main.go:249` sets the active locale from `tuiCfg.Language`.

## Composition

- **Parent:** repo root (`../ANATOMY.md`)
- **Subfolders:**
  - `tui/internal/tui/` — Bubble Tea screens (~19k LOC; `tui/internal/tui/ANATOMY.md`)
  - `tui/internal/preset/` — preset load/save/apply, embeds templates/recipes
  - `tui/internal/migrate/` — per-project migrations (shared version space with portal)
  - `tui/internal/globalmigrate/` — per-machine migrations (`~/.lingtai-tui/`)
  - `tui/internal/fs/` — filesystem readers for agent state
  - `tui/internal/config/` — bootstrap, venv, global config (`tui/internal/config/ANATOMY.md`)
  - `tui/internal/process/` — subprocess launcher
  - `tui/internal/headless/` — JSON-emitting non-interactive CLI surface (`bootstrap`, `presets`, `spawn` subcommands)
  - `tui/internal/postman/` — UDP cross-internet mail relay
  - `tui/internal/timemachine/` — git-backed history daemon
  - `tui/i18n/` — en/zh/wen locale tables
  - `tui/scripts/` — build helpers
- **Build output:** `tui/bin/lingtai-tui` (single binary)
- **Sibling binaries:** `portal/` — `lingtai-portal` web server

## State

- **Writes:** `tui/bin/lingtai-tui` (build artifact). Subcommands write signal files (`.suspend`) and can remove `.lingtai/` (`cleanMain`).
- **Reads:** `~/.lingtai-tui/` (global config, venv, presets), `<project>/.lingtai/` (agent state), `~/.lingtai-tui/config.json` (TUI preferences).
- **Version stamp:** `main.go:29` — set at build time, never changes at runtime.
- **Upgrade sentinels:** `~/.lingtai-tui/.firstrun` (one-time welcome), `~/.lingtai-tui/.last_agent_check` (periodic agent count reminder).

## Notes

- **Binary naming is `lingtai-tui`, never `lingtai`.** `lingtai` is the Python agent CLI inside the runtime venv.
- **`main.go` is intentionally flat** — every subcommand's `*Main()` function is defined inline in `main.go` or platform-specific `*_unix.go`/`*_windows.go` files. Don't refactor subcommands into `internal/` packages; the flat `main.go` is the contract.
- **Platform shims follow the `//go:build !windows` pattern.** Unix is the primary target; Windows files mirror the same function signatures. Every subcommand (`purge`, `list`, `suspend`) plus `countRunningAgents` and TUI-process upgrade helpers have paired platform files.
- **The platform split** covers: `purge`, `list`, `suspend`, `agent_count`, and `tui_process`. The `timemachine` and `postman` subcommands live in `internal/` and share no platform-specific `main.go` surface.
- **Version stamping:** `Makefile:4` uses `git describe --tags --always`. Dev builds get `-X main.version=dev`. The upgrade check in `main.go:79-82` skips dev builds (those containing `-` in the version string).
- **MCP packages are dependencies of `lingtai`.** The `lingtai` PyPI package bundles `lingtai-kernel` + all addon MCPs. `config.CheckUpgrade` on every launch upgrades everything. Users never install MCP packages individually.
