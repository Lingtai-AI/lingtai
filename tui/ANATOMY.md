---
related_files:
  - ANATOMY.md
  - portal/ANATOMY.md
  - tui/internal/tui/ANATOMY.md
  - tui/internal/config/ANATOMY.md
  - tui/internal/fs/ANATOMY.md
  - tui/internal/inventory/ANATOMY.md
  - tui/internal/migrate/ANATOMY.md
  - tui/internal/preset/ANATOMY.md
  - tui/internal/processscan/ANATOMY.md
  - tui/main.go
  - tui/main_no_project_gate_test.go
  - tui/internal/tui/launcher.go
  - tui/Makefile
  - tui/go.mod
  - tui/i18n/i18n.go
  - tui/i18n/en.json
  - tui/i18n/zh.json
  - tui/i18n/wen.json
  - tui/internal/inventory/inventory.go
  - tui/internal/inventory/inventory_test.go
  - tui/list_common.go
  - tui/list_common_test.go
  - tui/list_unix.go
  - tui/list_unix_test.go
  - tui/list_windows.go
  - tui/list_windows_test.go
  - tui/purge_common.go
  - tui/purge_common_test.go
  - tui/purge_unix.go
  - tui/purge_unix_test.go
  - tui/purge_windows.go
  - tui/suspend_unix.go
  - tui/suspend_windows.go
  - tui/upgrade.go
maintenance: |
  Keep related_files as repo-relative paths to real files. Include neighboring
  ANATOMY.md files so the anatomy graph stays connected rather than isolated;
  anatomy links must be bidirectional. If you create a new ANATOMY.md, copy this
  maintenance field. If you notice drift between this anatomy and the code,
  report it. See lingtai-dev-guide for details.
---

# tui — the `lingtai-tui` binary

> **Maintenance:** see `lingtai-tui-anatomy` (at `tui/internal/preset/skills/lingtai-tui-anatomy/SKILL.md`).

This folder is the self-contained Go module for the `lingtai-tui` terminal UI binary. It ships as a single executable built from `main.go` with platform-specific companions, embedding the i18n tables. All substantive logic lives under `internal/`. The binary has two faces: a subcommand surface (`purge`, `list`, `clean`, `suspend`, `postman`, `bootstrap`, `presets`, `spawn`, `self-update`, `doctor`) and an interactive Bubble Tea v2 UI that launches Python agents as subprocesses and observes them via the filesystem.

## Components

- **`tui/main.go:33-1332`** — single-file `package main`. The version stamp (`tui/main.go:31`, set via `-ldflags`), welcome/help text, Rust toolchain startup guidance, and interactive entry (`tui/main.go:33-397`). After parsing subcommands, `main()` runs the no-project decision gate (below), then global housekeeping, checks invariants (init.json all-or-nothing, exactly-one-orchestrator), handles upgrade prompts and first-run wizard routing, then launches Bubble Tea. Existing-project handoff keeps the launcher, canonical Bodhi-leaf loading view, and prepared App in one program.
- **No-project decision gate** — the pure probe runs before eager-write startup work. When no project exists, one root program owns launcher navigation, the canonical loading view, and Open Existing preparation; a typed ready result installs the real App. Create remains the existing committed-project path and cancellation remains zero-write. Gate ordering and subcommand isolation are covered by `tui/main_no_project_gate_test.go`.
- **No-project launcher Open Existing** — the pre-App launcher embeds the established registry-mode `ProjectsModel` instead of maintaining a second project catalog. Its validated selection becomes `DecisionOpenExisting`; the same Bubble Tea root then renders the canonical Bodhi-leaf loading view while preparation runs and installs the real App only after a typed ready outcome. Create remains draft-only until the existing staging/rename commit path.
- **`tui/main.go:35-96`** — subcommand dispatch. Each subcommand returns early; the fallthrough path starts the interactive TUI.
- **`list_common.go` / `list_unix.go` / `list_windows.go`** — `lingtai-tui list` process discovery and decentralized running-agent inventory rendering. Platform files call shared `processscan` discovery and fail loud with a nonzero exit when the scan command itself fails (`tui/list_unix.go:19-24`, `tui/list_windows.go:19-24`); `internal/inventory` converts process rows into typed, enriched, grouped records (`tui/internal/inventory/inventory.go:105-167`), and `list_common.go` keeps CLI parsing plus table/JSON rendering (`tui/list_common.go:47-181`). `--admin` is a detail mode that adds admin, IM, and state columns; it is not an admin-only filter.
- **`upgrade.go`** — startup TUI binary upgrade flow: after install-method routing (`tui/main.go:161-170`), Homebrew installs keep the existing prompt that detects other running TUI windows, puts affected agents to sleep, stops old TUI processes, and runs `brew upgrade` before asking the user to relaunch; source/user-local installs get a separate explicit `[y/N]` prompt that routes through the source updater backend (`install.sh --update`). Unknown installs are not mutated at startup (version-only).
- **`tui/main.go:808-891`** — `maybePromptRustToolchain` / `markRustPromptSeen`: one-time optional Rust/Cargo startup prompt. Only prompts on an interactive TTY when the managed runtime is on the Python file-search fallback and no `cargo` is on PATH. Writes the `~/.lingtai-tui/runtime/rust-toolchain-prompted` marker on decline/install/skip — including when the probe errors or reports an unsupported runtime — so the Python probe never re-spawns on every launch.
- **`tui/main.go:964-1112`** — `cleanMain`/`cleanProject`: suspend agents in `.lingtai/` (10s timeout), then `os.RemoveAll`. Refuses to delete while any agent heartbeat is still fresh after the timeout, when agents cannot be listed, or when an agent appeared during the wait (re-discovered before deleting) — unless `--force` is given (issue #488).
- **`tui/main.go:1114-1162`** — `postmanMain`: parse `--port`, collect watch directories, call `postman.Run`.
- **`tui/purge_common.go:9-39` / `tui/purge_unix.go:17-80`** — `purgeMain` (unix): shared processscan-to-purge-target filtering, then SIGTERM → SIGKILL survivors. Build tag `!windows`.
- **`purge_windows.go`** — `purgeMain` (windows): equivalent via Windows process enumeration.
- **`tui/list_unix.go:13-57`** — `listMain` (unix): enumerate running agents with human-readable uptime derived from ps etime, phantom detection (`.lingtai/` deleted but process still running). Build tag `!windows`.
- **`list_windows.go`** — `listMain` (windows): equivalent.
- **`tui/suspend_unix.go:14-86`** — `suspendMain` (unix): discover agents via `.agent.json`, write `.suspend` files, wait 5s. Build tag `!windows`.
- **`suspend_windows.go`** — `suspendMain` (windows): equivalent.
- **`tui/agent_count_unix.go:16-38`** — `countRunningAgents` (unix): lightweight `ps aux` scan used by `maybeShowAgentCount`. Build tag `!windows`.
- **`agent_count_windows.go`** — `countRunningAgents` (windows): equivalent.
- **`tui_process_unix.go` / `tui_process_windows.go`** — platform helpers for detecting and stopping other running `lingtai-tui` binaries before an in-app Homebrew upgrade.
- **`Makefile:1-23`** — build, dev (fast local), cross-compile (darwin/linux × arm64/amd64), clean. Version stamp via `-ldflags "-X main.version=$(VERSION)"` where `VERSION` is `git describe --tags --always`.
- **`tui/i18n/i18n.go:10`** — `//go:embed en.json zh.json wen.json`. The only embed target in the root `tui/` package; all other embeds are in `internal/preset/`.
- **`tui/internal/`** — all substantive packages (tui screens, preset engine, historical migration package, filesystem readers, process launcher, headless JSON CLI surface, postman, lock shims).
- **`tui/internal/headless/`** — JSON-emitting non-interactive surface. `RunPresets` (lists templates/saved presets as JSON), `RunSpawn` (creates a project + launches an agent), and `ExitError` (structured error codes). Wired from `main.go` via `bootstrapMain` (`tui/main.go:1164`), `presetsMain` (`tui/main.go:1252`), and `spawnMain` (`tui/main.go:1276`). For agents and scripts that drive `lingtai-tui` without the Bubble Tea UI.

## Connections

- **Called from:** the shell (`lingtai-tui`), Homebrew tap (`lingtai-ai/lingtai/lingtai-tui`), `install.sh`.
- **Calls out:** `tui/internal/tui` (Bubble Tea app), `tui/internal/sqlitelog` (sqlite3-backed `logs/log.sqlite` query helpers for notification events, session boundaries, diagnostics, doctor errors, and clear completion checks), `tui/internal/globalmigrate` (per-machine migrations), `tui/internal/preset` (bootstrap + utility skill population), `tui/internal/process` (agent launch), `tui/internal/processscan` (shared ps-based agent-process detection), `tui/internal/inventory` (typed running-agent inventory shared by CLI list and `/projects`), `tui/internal/config` (global config, venv, upgrade checks, install-method-routed TUI updater), `tui/internal/postman` (mail relay daemon).
- **Locale resolution** (`tui/main.go:184-190`): immediately after `globalDir` is known, the TUI runs `config.MigrateLegacyLanguage` → `config.LoadTUIConfig` → `i18n.SetLang(tuiCfg.Language)` so every user-visible startup string (codex banner, welcome, agent-count reminder) renders in the configured locale rather than the i18n default. `tuiCfg` is reused for the rest of bootstrap.
- **Bootstrap sequence** (`tui/main.go:265-336`): on every launch, the TUI initializes project-local `.lingtai/` state with `process.InitProject`, registers the project, and explicitly refreshes user-level utility skills via `preset.PopulateBundledLibrary(globalDir)` before returning-user runtime/bootstrap work. Returning users then run `config.NeedsVenv` (for setup banner) → `config.EnsureRuntime` (create/repair venv if needed, then always run the non-blocking `CheckUpgrade`) → `preset.Bootstrap` → `tui.ExportCommandsJSON` → `maybePromptRustToolchain` (one-time optional Rust/Cargo prompt only when file search is on Python fallback and no cargo is on PATH). `CheckUpgrade` auto-upgrades the `lingtai` meta-package from PyPI, which bundles `lingtai-kernel` + all addon MCPs. See `tui/internal/config/ANATOMY.md`.
- **`lingtai-tui doctor` subcommand** (`tui/main.go:1174-1214`): runs `config.RunDoctorUpdate` (`tui/main.go:1186`) with both `ForceTUI=true` and `ForcePython=true`, then refreshes presets, utility skills, and `commands.json`. The report includes native file-search sidecar / Rust toolchain diagnostics (`config.checkFileSearchNative`). Designed to be usable when the TUI cannot start (for example, a broken venv) — it never touches `.lingtai/`. Exit nonzero only on unrecoverable failures.
- **`lingtai-tui self-update` subcommand** (`tui/main.go:1216-1249`): runs `config.RunManualTUIUpdate` (`tui/main.go:1223`) through the detected install-method backend. Homebrew installs run the brew updater; source/user-local installs run the source updater backend (`install.sh --update`); unknown installs produce unsupported guidance without trying brew.
- **Version flow:** `Makefile:4` injects `git describe` into `tui/main.go:31`. On startup, `tui/main.go:94` calls `tui.SetTUIVersion(version)`, which stores it for `/doctor` drift detection.
- **TUI binary upgrade:** `tui/main.go:151-175` checks for a newer release, detects the current install method (`config.DetectCurrentTUIInstall`), then routes on `install.Method` (`tui/main.go:161-170`) and delegates newer-release handling to `upgrade.go` (`handleTUIUpgrade`) for Homebrew and source/user-local installs. Homebrew keeps the existing confirmation flow, can put agents in their projects to sleep, stops other TUI processes, runs the Homebrew backend, and asks the user to relaunch. Source/user-local installs require an explicit `[y/N]` yes before running the source updater backend through `install.sh --update`; unknown installs keep the version-only startup path and do not mutate.
- **i18n loading:** `i18n/i18n.go` embeds the three locale JSONs; `tui/main.go:184-190` sets the active locale from `tuiCfg.Language` early in startup (see Locale resolution above) so startup banners localize correctly. Each catalog holds only its own language — there are no bilingual entries (a `mail.initial_loading` that mixed `loading...` and `加载中...` was the documented anti-pattern, since split per-locale).

## Composition

- **Parent:** repo root (`../ANATOMY.md`)
- **Subfolders:**
  - `tui/internal/tui/` — Bubble Tea screens (~19k LOC; `tui/internal/tui/ANATOMY.md`)
  - `tui/internal/sqlitelog/` — sqlite3 CLI-backed helpers for reading kernel `logs/log.sqlite` notification events, session-boundary and diagnostic rows, doctor errors, and clear completion rows
  - `tui/internal/preset/` — preset load/save/apply, embeds templates/recipes
  - `tui/internal/doctorreport/` — redacted report writer used by interactive `/doctor` to save a GitHub-ready diagnostic bundle (report.md + metadata.json + redaction.json). Owns redaction; collects no raw event logs.
  - `tui/internal/migrate/` — retained m001–m039 migration history/tests; not imported by production startup or project creation
  - `tui/internal/globalmigrate/` — separate per-machine housekeeping (`~/.lingtai-tui/`)
  - `tui/internal/fs/` — filesystem readers for agent state
  - `tui/internal/config/` — bootstrap, venv, global config (`tui/internal/config/ANATOMY.md`)
  - `tui/internal/process/` — subprocess launcher
  - `tui/internal/processscan/` — shared ps-based `lingtai run <agentDir>` process detection used by launch
  - `tui/internal/inventory/` — typed running-agent inventory built from processscan rows plus `.agent.json`/heartbeat/status enrichment (`tui/internal/inventory/ANATOMY.md`)
  - `tui/internal/headless/` — JSON-emitting non-interactive CLI surface (`bootstrap`, `presets`, `spawn` subcommands)
  - `tui/internal/postman/` — UDP cross-internet mail relay
  - `tui/i18n/` — en/zh/wen locale tables
  - `tui/scripts/` — build helpers
- **Build output:** `tui/bin/lingtai-tui` (single binary)
- **Sibling binaries:** `portal/` — `lingtai-portal` web server

## State

- **Writes:** `tui/bin/lingtai-tui` (build artifact). Subcommands write signal files (`.suspend`) and can remove `.lingtai/` (`cleanMain`).
- **Reads:** `~/.lingtai-tui/` (global config, venv, presets), `<project>/.lingtai/` (agent state), `~/.lingtai-tui/config.json` (TUI preferences).
- **Version stamp:** `tui/main.go:31` — set at build time, never changes at runtime.
- **Upgrade sentinels:** `~/.lingtai-tui/.firstrun` (one-time welcome), `~/.lingtai-tui/.last_agent_check` (periodic agent count reminder), `~/.lingtai-tui/runtime/rust-toolchain-prompted` (one-time startup Rust prompt dismissal/install marker).

## Notes

- **Finding companions / local agent inventory:** use `lingtai-tui list --detailed [dir]` or `/projects` as the first stop before hand-walking `.lingtai/` trees. `--admin` is a detail/rendering mode, not a filter. The shared inventory conversion/enrichment is owned by `tui/internal/inventory/inventory.go:105-167` and `tui/internal/inventory/inventory.go:214-263`; platform process discovery is in `tui/list_unix.go:19-27` and `tui/list_windows.go:19-24`; CLI parsing/table/JSON rendering is in `tui/list_common.go:52-177`; `/projects` renders the same typed snapshot as selectable project-grouped rows plus curated details in `tui/internal/tui/projects.go:586-631` and `tui/internal/tui/projects.go:709-1147`.
- **Binary naming is `lingtai-tui`, never `lingtai`.** `lingtai` is the Python agent CLI inside the runtime venv.
- **`main.go` is intentionally flat** — every subcommand's `*Main()` function is defined inline in `main.go` or platform-specific `*_unix.go`/`*_windows.go` files. Don't refactor subcommands into `internal/` packages; the flat `main.go` is the contract.
- **Platform shims follow the `//go:build !windows` pattern.** Unix is the primary target; Windows files mirror the same function signatures. Every subcommand (`purge`, `list`, `suspend`) plus `countRunningAgents` and TUI-process upgrade helpers have paired platform files.
- **The platform split** covers: `purge`, `list`, `suspend`, `agent_count`, and `tui_process`. The `postman` subcommand lives in `internal/` and shares no platform-specific `main.go` surface.
- **Version stamping:** `Makefile:4` uses `git describe --tags --always`. Dev builds get `-X main.version=dev`. The upgrade check in `tui/main.go:158-162` skips dev builds (those containing `-` in the version string). This check — and the whole GitHub-release-lookup/Homebrew-upgrade-prompt block around it — runs AFTER the no-project decision gate, so entering the launcher for an empty directory never blocks on a network version check either.
- **MCP packages are dependencies of `lingtai`.** The `lingtai` PyPI package bundles `lingtai-kernel` + all addon MCPs. `config.CheckUpgrade` on every launch upgrades everything. Users never install MCP packages individually.
- **No-project launcher is a distinct model, not a fake `App`.** `App` assumes a resolved project/orchestrator context, so the root begins as `LauncherRootModel`, renders the canonical Bodhi loading page after Open Existing, and installs the real App only after a concrete root is prepared. The launcher and App are model phases of the same Open Existing Bubble Tea program; Create remains the launcher’s draft flow and is committed before its normal App program starts.
