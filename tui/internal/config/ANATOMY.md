# config — bootstrap, venv, global config

> **Maintenance:** see `lingtai-tui-anatomy` (at `tui/internal/preset/skills/lingtai-tui-anatomy/SKILL.md`).

This package manages the TUI's bootstrap sequence — the steps that run before any agent launches. It owns the Python runtime venv, the lingtai CLI upgrade path, addon verification, and the global config files under `~/.lingtai-tui/`.

## Components

- **`venv.go:59-166`** — `EnsureVenv`: creates the runtime venv at `~/.lingtai-tui/runtime/venv/`. Uses `uv` if available (can auto-download Python 3.13), falls back to system Python. Verifies Python 3.11+, installs `lingtai`, symlinks CLI into PATH.
- **`venv.go:300-309`** — `CheckUpgrade`: thin wrapper over `UpgradePythonRuntime(globalDir, false, ...)` that returns `true` only when a real upgrade was verified post-install. Used by `EnsureRuntime`, where any failure is silent so the TUI can still launch. It no longer reports success when the underlying pip command fails.
- **`venv.go:311-350`** — `EnsureRuntime` / `EnsureRuntimeQuiet`: startup helpers that ensure the managed Python runtime exists, then always run the non-blocking `CheckUpgrade` path after a successful ensure. This avoids the old intermittent skip where a newly-created/repaired venv would not check PyPI until the next launch.
- **`venv.go:423-453`** — `RunDoctorUpdate`: the forced check-and-update routine called by `/doctor` and `lingtai-tui doctor`. Verifies the TUI binary against the latest GitHub release (brew-upgrades when needed), repairs a missing/broken runtime venv, then runs `UpgradePythonRuntime(force=true)`. All command stdout/stderr is captured and surfaced; nothing is silently swallowed.
- **`venv.go:556-620`** — `UpgradePythonRuntime`: compares installed `lingtai` to PyPI, runs `uv pip install --upgrade lingtai -p <venv>` (or `pip install --upgrade lingtai`), then re-imports to verify the new version. Reports `DoctorFail` on any command error or post-install version mismatch.
- **`venv.go:340-407`** — `DoctorOptions` / `CommandRunner`: dependency-injection seams so tests stub brew/pip/python without dialing the network. Production callers leave them nil and get real `exec.Command` + `http.Client`.
- **`venv.go:283-313`** — `EnsureAddons`: reads `init.json`'s `addons` map, verifies each addon is importable as `lingtai.addons.<name>`. Error surfaces which addon is missing and suggests `pip install --upgrade lingtai`.
- **`venv.go:241-254`** — `CheckTUIUpgrade`: compares running TUI binary version against latest GitHub release. Returns tag if upgrade available. Used by the startup-time prompt in `main.go`.
- **`venv.go:47-57`** — `NeedsVenv`: returns true if no venv exists or `lingtai` is not importable.
- **`venv.go:171-193`** — `linkLingtaiCLI`: symlinks `lingtai` CLI from venv into brew prefix or `~/.local/bin`.
- **`global.go:30-42`** — `Config`: global config at `~/.lingtai-tui/config.json`. Keys map env-var names to API key values.
- **`global.go:78-83`** — `TUIConfig`: TUI preferences at `~/.lingtai-tui/tui_config.json` (language, mail page size, theme, insights).
- **`global.go:178-188`** — `WriteEnvFile`: writes `~/.lingtai-tui/.env` from Config.Keys. Loaded by agents via `env_file` in `init.json`.
- **`registry.go`** — preset registry management (see `preset/ANATOMY.md`).

## Connections

- **Called from:** `tui/main.go:277-289`, `tui/internal/tui/firstrun.go:597-604`, and `tui/internal/headless/spawn.go:77-83` — startup/first-run/headless bootstrap paths.
- **Calls out:** PyPI API (`pypi.org/pypi/lingtai/json`), GitHub API (`api.github.com/repos/Lingtai-AI/lingtai/releases/latest`), `uv` / `pip` CLI.
- **Bootstrap sequence** (in `main.go:277-295`):
  1. `config.MigrateLegacyLanguage(globalDir)` — one-shot language migration
  2. `config.NeedsVenv(globalDir)` — check if a setup banner should be printed
  3. `config.EnsureRuntime(globalDir)` — create/repair venv if needed, then always auto-check/upgrade the `lingtai` PyPI package
  4. `config.EnsureAddons(python, agentDir)` — verify addon importability
  5. `preset.Bootstrap(globalDir)` — copy preset resources
  6. `tui.ExportCommandsJSON(globalDir)` — export slash commands

## Composition

- **Parent:** `tui/internal/`
- **Sibling packages:** `tui/internal/preset/`, `tui/internal/migrate/`, `tui/internal/process/`

## State

- **Writes:** `~/.lingtai-tui/runtime/venv/` (Python venv), `~/.lingtai-tui/config.json` (API keys), `~/.lingtai-tui/tui_config.json` (TUI prefs), `~/.lingtai-tui/.env` (env file for agents).
- **Reads:** `init.json` (addon declarations), PyPI/GitHub APIs (version checks).

## Notes

- **MCP packages are dependencies of `lingtai`.** `lingtai` on PyPI is a meta-package that bundles `lingtai-kernel` + all addon MCPs. `pip install --upgrade lingtai` upgrades everything. Users never install MCP packages individually.
- **`EnsureRuntime` runs on every TUI launch** (for returning users), first-run bootstrap, and headless spawn. It always runs `CheckUpgrade` after any successful venv creation/repair; the PyPI check is non-blocking (3s HTTP timeout) and silently no-ops on network errors. The same logic — minus the silence — is reachable via `lingtai-tui doctor` or `/doctor`, which call `RunDoctorUpdate` with `ForceTUI=true` / `ForcePython=true` and surface every command's stdout/stderr.
- **Dev mode detection:** `EnsureVenv` checks for local `~/Documents/GitHub/lingtai-kernel` + `~/Documents/GitHub/lingtai` and uses editable installs (`pip install -e`) if both exist.
- **`uv` preferred over `pip`:** all pip operations prefer `uv` if available (faster, can auto-download Python).
