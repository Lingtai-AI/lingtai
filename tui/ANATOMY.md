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
  - tui/internal/process/ANATOMY.md
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
  - tui/kernel_upgrade_consent_test.go
  - tui/startup_preflight_test.go
maintenance: |
  Keep related_files as repo-relative paths to real files. Include neighboring
  ANATOMY.md files so the anatomy graph stays connected rather than isolated;
  anatomy links must be bidirectional. If you create a new ANATOMY.md, copy this
  maintenance field. If you notice drift between this anatomy and the code,
  report it. See lingtai-dev-guide for details.
---

# tui — the `lingtai-tui` binary

> **Maintenance:** see `lingtai-tui-anatomy` (at `tui/internal/preset/skills/lingtai-tui-anatomy/SKILL.md`).

This folder is the self-contained Go module for the `lingtai-tui` terminal UI binary. It ships as a single executable built from `main.go` with platform-specific companions, embedding the i18n tables. All substantive logic lives under `internal/`. The binary has two faces: a subcommand surface (`purge`, `list`, `clean`, `suspend`, `bootstrap`, `presets`, `spawn`, `self-update`, `doctor`) and an interactive Bubble Tea v2 UI that launches Python agents as subprocesses and observes them via the filesystem.

## Components

- **`tui/main.go:33-1332`** — single-file `package main`. The version stamp (`tui/main.go:31`, set via `-ldflags`), welcome/help text, Rust toolchain startup guidance, and interactive entry (`tui/main.go:33-397`). After parsing subcommands, `main()` runs the no-project decision gate (below), then global housekeeping, checks invariants (init.json all-or-nothing, exactly-one-orchestrator), handles upgrade prompts and first-run wizard routing, then launches Bubble Tea. Existing-project handoff keeps the launcher, canonical Bodhi-leaf loading view, and prepared App in one program.
- **No-project decision gate** — the pure probe runs before eager-write startup work. When no project exists, one root program owns launcher navigation, the canonical loading view, and Open Existing preparation; a typed ready result installs the real App. Create remains the existing committed-project path and cancellation remains zero-write. Gate ordering and subcommand isolation are covered by `tui/main_no_project_gate_test.go`.
- **No-project launcher Open Existing** — the pre-App launcher embeds the established registry-mode `ProjectsModel` instead of maintaining a second project catalog. Its validated selection becomes `DecisionOpenExisting`; the same Bubble Tea root then renders the canonical Bodhi-leaf loading view while preparation runs and installs the real App only after a typed ready outcome. Create remains draft-only until the existing staging/rename commit path.
- **`tui/main.go:35-96`** — subcommand dispatch. Each subcommand returns early; the fallthrough path starts the interactive TUI.
- **`list_common.go` / `list_unix.go` / `list_windows.go`** — `lingtai-tui list` process discovery and decentralized running-agent inventory rendering. Platform files call shared `processscan` discovery and fail loud with a nonzero exit when the scan command itself fails (`tui/list_unix.go:19-24`, `tui/list_windows.go:19-24`); `internal/inventory` converts process rows into typed, enriched, grouped records (`tui/internal/inventory/inventory.go:105-167`), and `list_common.go` keeps CLI parsing plus table/JSON rendering (`tui/list_common.go:47-181`). `--admin` is a detail mode that adds admin, IM, and state columns; it is not an admin-only filter.
- **`upgrade.go`** — startup TUI binary upgrade flow: after install-method routing (`tui/main.go:161-170`), Homebrew installs keep the existing prompt that detects other running TUI windows, puts affected agents to sleep, stops old TUI processes, and runs `brew upgrade` before asking the user to relaunch; source/user-local installs get a separate explicit `[y/N]` prompt that routes through the source updater backend (`install.sh --update`). Unknown installs are not mutated at startup (version-only).
- **`tui/main.go:808-891`** — `maybePromptRustToolchain` / `markRustPromptSeen`: one-time optional Rust/Cargo startup prompt. Only prompts on an interactive TTY when the managed runtime is on the Python file-search fallback and no `cargo` is on PATH. Writes the `~/.lingtai-tui/runtime/rust-toolchain-prompted` marker on decline/install/skip — including when the probe errors or reports an unsupported runtime — so the Python probe never re-spawns on every launch.
- **`tui/main.go:809-958`** — `cleanMain`/`cleanProject`: suspend agents in `.lingtai/` (10s timeout), then `os.RemoveAll`. Refuses to delete while any agent heartbeat is still fresh after the timeout, when agents cannot be listed, or when an agent appeared during the wait (re-discovered before deleting) — unless `--force` is given (issue #488).
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
- **`tui/internal/`** — all substantive packages (tui screens, preset engine, historical migration package, filesystem readers, process launcher, headless JSON CLI surface, lock shims).
- **`tui/internal/headless/`** — JSON-emitting non-interactive surface. `RunPresets` (lists templates/saved presets as JSON), `RunSpawn` (creates a project + launches an agent), and `ExitError` (structured error codes). Wired from `main.go` via `bootstrapMain` (`tui/main.go:959`), `presetsMain` (`tui/main.go:1047`), and `spawnMain` (`tui/main.go:1071`). For agents and scripts that drive `lingtai-tui` without the Bubble Tea UI. `RunSpawn`'s runtime check calls `config.RuntimeReady` — read-only, never installs/repairs/upgrades. Headless spawn cannot prompt, so a not-ready runtime (missing, broken, or a declined preflight) surfaces as an actionable `bootstrap_failed` error instead of a silent install.

## Connections

- **Called from:** the shell (`lingtai-tui`), Homebrew tap (`lingtai-ai/lingtai/lingtai-tui`), `install.sh`.
- **Calls out:** `tui/internal/tui` (Bubble Tea app), `tui/internal/sqlitelog` (sqlite3-backed `logs/log.sqlite` query helpers for notification events, session boundaries, diagnostics, doctor errors, and clear completion checks), `tui/internal/globalmigrate` (per-machine migrations), `tui/internal/preset` (bootstrap + utility skill population), `tui/internal/process` (agent launch), `tui/internal/processscan` (shared ps-based agent-process detection), `tui/internal/inventory` (typed running-agent inventory shared by CLI list and `/projects`), and `tui/internal/config` (global config, venv, upgrade checks, install-method-routed TUI updater).
- **Locale resolution** (`tui/main.go:184-190`): immediately after `globalDir` is known, the TUI runs `config.MigrateLegacyLanguage` → `config.LoadTUIConfig` → `i18n.SetLang(tuiCfg.Language)` so every user-visible startup string (codex banner, welcome, agent-count reminder) renders in the configured locale rather than the i18n default. `tuiCfg` is reused for the rest of bootstrap.
- **Shared startup-update preflight** (`runVersionPreflight`/`runVersionPreflightWithOptions`, `tui/main.go:1331-1414`) — the FIRST thing `main()` does after resolving `projectDir`, strictly before the no-project decision gate (`tui/main.go:120-122`, `if runVersionPreflight() { return }`). Every default interactive launch shape — existing-project returning user, first-run (reached through the no-project launcher or directly), and the empty-directory no-project launcher's own picker screen — passes through this single call site before any of them can diverge, so none can silently skip the check. install.sh installs the kernel by default, so first-run is not a reason to skip it. It performs, unconditionally: (1) the read-only TUI version check (`config.CheckTUIUpgrade`, skipped for dev builds) — a Homebrew/source install with an available update routes through the pre-existing `upgrade.go` `handleTUIUpgrade` `[y/N]` flow (unchanged), and a completed self-upgrade exits `main()` immediately, before the gate or any Bubble Tea program starts; (2) the read-only kernel check (`maybeCheckAndPromptKernelUpgrade` → `config.InspectKernel`, no pip/uv/brew command ever) and, only when `NeedsUpdate=true` on a real stdin TTY, a `[y/N]` prompt whose wording distinguishes an absent/unimportable/broken kernel (`status.Installed==""`: explicit install/repair prompt, never the generic update phrasing) from a present-but-stale one (installed → latest update prompt) — either way only "yes" calls the mutating `config.RunKernelUpdate(globalDir, force=false)` exactly once, and this IS the sole automatic install/repair/update opportunity for the whole launch. Because this runs before any Bubble Tea program exists, the kernel prompt can block on stdin directly with no `inProgram`/fallback dance. Consent is never persisted or reused across launches. Uses `config.GlobalDirPath()` (pure, no mkdir) for the check phase; a consented mutation creates `~/.lingtai-tui` itself as an intentional side effect of that mutation, not of merely checking — preserving the no-project gate's zero-write invariant up to the point of explicit consent. See `tui/internal/config/ANATOMY.md`, `docs/superpowers/specs/2026-06-23-launch-kernel-upgrade-confirm-design.md`, and `.../2026-06-23-update-command-design.md` (defines `InspectKernel`/`RunKernelUpdate` and the `/update` command that also consumes them, `tui/internal/tui/update.go`).
- **Bootstrap sequence** (`tui/main.go:1415-1652`, `prepareApp`): runs strictly AFTER `runVersionPreflight` has already completed (see above) — `prepareApp` itself performs no TUI or kernel version check, and no install/repair, of its own. On every launch, the TUI initializes project-local `.lingtai/` state with `process.InitProject`, registers the project, and explicitly refreshes user-level utility skills via `preset.PopulateBundledLibrary(globalDir)` before returning-user runtime/bootstrap work (`tui/main.go:1533-1637`). Returning users run `config.RuntimeReady` — a **read-only** readiness check; if the runtime is still not usable (declined preflight, from-scratch machine, etc.) `prepareApp` surfaces the actionable error and refuses to continue rather than silently repairing — then `preset.Bootstrap` → `tui.ExportCommandsJSON` → `maybePromptRustToolchain` (one-time optional Rust/Cargo prompt only when file search is on Python fallback and no cargo is on PATH). First-run setup (`firstrun.go`) and headless spawn (`internal/headless/spawn.go`) likewise call `config.RuntimeReady` only — there is no `EnsureRuntime`/`EnsureRuntimeQuiet` any more anywhere in this codebase (removed as part of this design): the shared preflight is the sole automatic install/repair opportunity, and every other caller only checks and surfaces, never silently mutates.
- **`lingtai-tui doctor` subcommand** (`tui/main.go:969-1009`): runs `config.RunDoctorUpdate` (`tui/main.go:981`) with both `ForceTUI=true` and `ForcePython=true`, then refreshes presets, utility skills, and `commands.json`. The report includes native file-search sidecar / Rust toolchain diagnostics (`config.checkFileSearchNative`). `ForcePython=true` is the explicit-invocation consent for a kernel upgrade — running `doctor` IS the human's opt-in, so this path (unlike ordinary returning-user startup) mutates without a further prompt. Designed to be usable when the TUI cannot start (for example, a broken venv) — it never touches `.lingtai/`. Exit nonzero only on unrecoverable failures.
- **`lingtai-tui self-update` subcommand** (`tui/main.go:1011-1032`): runs `config.RunManualTUIUpdate` (`tui/main.go:1018`) through the detected install-method backend. Homebrew installs run the brew updater; source/user-local installs run the source updater backend (`install.sh --update`); unknown installs produce unsupported guidance without trying brew. Like `doctor`, explicit invocation of this subcommand is itself the consent — it updates the TUI binary, not the Python kernel (that's `doctor`'s `ForcePython`).
- **Version flow:** `Makefile:4` injects `git describe` into `tui/main.go:31`. On startup, `tui/main.go:94` calls `tui.SetTUIVersion(version)`, which stores it for `/doctor` drift detection.
- **TUI binary upgrade:** now lives inside the shared preflight (`runVersionPreflightWithOptions`, `tui/main.go:1352-1410` — see "Shared startup-update preflight" above), not inside `prepareApp`. It checks for a newer release, detects the current install method (`config.DetectCurrentTUIInstall`), then routes on `install.Method` and delegates newer-release handling to `upgrade.go` (`handleTUIUpgrade`) for Homebrew and source/user-local installs. Homebrew keeps the existing confirmation flow, can put agents in their projects to sleep, stops other TUI processes, runs the Homebrew backend, and asks the user to relaunch. Source/user-local installs require an explicit `[y/N]` yes before running the source updater backend through `install.sh --update`; unknown installs keep the version-only startup path and do not mutate. Because the preflight runs before the no-project gate, a completed self-upgrade exits `main()` before any project-specific state (or the launcher's own Bubble Tea program) is ever touched.
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
  - `tui/internal/process/` — agent-process lifecycle boundary: exact Python CLI launch plus platform termination (`tui/internal/process/ANATOMY.md`)
  - `tui/internal/processscan/` — advisory cross-platform `lingtai run <agentDir>` process observation (`tui/internal/processscan/ANATOMY.md`)
  - `tui/internal/inventory/` — typed running-agent inventory built from processscan rows plus `.agent.json`/heartbeat/status enrichment (`tui/internal/inventory/ANATOMY.md`)
  - `tui/internal/headless/` — JSON-emitting non-interactive CLI surface (`bootstrap`, `presets`, `spawn` subcommands)
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
- **Platform-specific helpers** distinguish the CLI subcommands `purge`, `list`, and `suspend` from the interactive helpers `agent_count` and `tui_process`; the latter participate in interactive startup and upgrade-preflight paths and are not subcommands.
- **Version stamping:** `Makefile:4` uses `git describe --tags --always`. Dev builds get `-X main.version=dev`. The upgrade check in `tui/main.go:158-162` skips dev builds (those containing `-` in the version string). This check — and the whole GitHub-release-lookup/Homebrew-upgrade-prompt block around it — runs AFTER the no-project decision gate, so entering the launcher for an empty directory never blocks on a network version check either.
- **MCP packages are dependencies of `lingtai`.** The `lingtai` PyPI package bundles `lingtai-kernel` + all addon MCPs. `config.RunKernelUpdate`/`CheckUpgrade` upgrade everything at once, but only mutate when the human consents — on an interactive returning-user launch's `[y/N]` prompt (itself shown only when the read-only `config.InspectKernel` reports an actionable update), or via explicit `doctor`/`self-update`/`/update` invocation — never silently on every launch. Users never install MCP packages individually.
- **No-project launcher is a distinct model, not a fake `App`.** `App` assumes a resolved project/orchestrator context, so the root begins as `LauncherRootModel`, renders the canonical Bodhi loading page after Open Existing, and installs the real App only after a concrete root is prepared. The launcher and App are model phases of the same Open Existing Bubble Tea program; Create remains the launcher’s draft flow and is committed before its normal App program starts.
