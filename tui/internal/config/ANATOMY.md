# config — bootstrap, venv, global config

> **Maintenance:** see `lingtai-tui-anatomy` (at `tui/internal/preset/skills/lingtai-tui-anatomy/SKILL.md`).

This package manages the TUI's bootstrap sequence — the steps that run before any agent launches. It owns the Python runtime venv, the lingtai CLI upgrade path, addon verification, and the global config files under `~/.lingtai-tui/`.

## Components

- **`venv.go:59-166`** — `EnsureVenv`: creates the runtime venv at `~/.lingtai-tui/runtime/venv/`. Uses `uv` if available (can auto-download Python 3.13), falls back to system Python. Verifies Python 3.11+, installs `lingtai`, symlinks CLI into PATH.
- **`venv.go:319-377`** — `CheckUpgrade`: compares installed `lingtai` version against PyPI latest. Runs `pip install --upgrade lingtai` if newer exists. Non-blocking (3s timeout). Upgrades the `lingtai` meta-package, which bundles `lingtai-kernel` + all addon MCPs (`lingtai-telegram`, `lingtai-feishu`, `lingtai-imap`, `lingtai-wechat`).
- **`venv.go:283-313`** — `EnsureAddons`: reads `init.json`'s `addons` map, verifies each addon is importable as `lingtai.addons.<name>`. Error surfaces which addon is missing and suggests `pip install --upgrade lingtai`.
- **`venv.go:243-272`** — `CheckTUIUpgrade`: compares running TUI binary version against latest GitHub release. Returns tag if upgrade available.
- **`venv.go:47-57`** — `NeedsVenv`: returns true if no venv exists or `lingtai` is not importable.
- **`venv.go:171-193`** — `linkLingtaiCLI`: symlinks `lingtai` CLI from venv into brew prefix or `~/.local/bin`.
- **`global.go:30-42`** — `Config`: global config at `~/.lingtai-tui/config.json`. Keys map env-var names to API key values.
- **`global.go:78-83`** — `TUIConfig`: TUI preferences at `~/.lingtai-tui/tui_config.json` (language, mail page size, theme, insights).
- **`global.go:178-188`** — `WriteEnvFile`: writes `~/.lingtai-tui/.env` from Config.Keys. Loaded by agents via `env_file` in `init.json`.
- **`registry.go`** — preset registry management (see `preset/ANATOMY.md`).

## Connections

- **Called from:** `tui/main.go:275-286` — the bootstrap sequence in `main()`.
- **Calls out:** PyPI API (`pypi.org/pypi/lingtai/json`), GitHub API (`api.github.com/repos/Lingtai-AI/lingtai/releases/latest`), `uv` / `pip` CLI.
- **Bootstrap sequence** (in `main.go:273-291`):
  1. `config.MigrateLegacyLanguage(globalDir)` — one-shot language migration
  2. `config.NeedsVenv(globalDir)` — check if venv exists
  3. `config.EnsureVenv(globalDir)` — create venv + install lingtai (if needed)
  4. `config.CheckUpgrade(globalDir)` — auto-upgrade lingtai (if venv exists)
  5. `config.EnsureAddons(python, agentDir)` — verify addon importability
  6. `preset.Bootstrap(globalDir)` — copy preset resources
  7. `tui.ExportCommandsJSON(globalDir)` — export slash commands

## Composition

- **Parent:** `tui/internal/`
- **Sibling packages:** `tui/internal/preset/`, `tui/internal/migrate/`, `tui/internal/process/`

## State

- **Writes:** `~/.lingtai-tui/runtime/venv/` (Python venv), `~/.lingtai-tui/config.json` (API keys), `~/.lingtai-tui/tui_config.json` (TUI prefs), `~/.lingtai-tui/.env` (env file for agents).
- **Reads:** `init.json` (addon declarations), PyPI/GitHub APIs (version checks).

## Notes

- **MCP packages are dependencies of `lingtai`.** `lingtai` on PyPI is a meta-package that bundles `lingtai-kernel` + all addon MCPs. `pip install --upgrade lingtai` upgrades everything. Users never install MCP packages individually.
- **`CheckUpgrade` runs on every TUI launch** (for returning users). It is non-blocking (3s HTTP timeout) and silently no-ops on network errors.
- **Dev mode detection:** `EnsureVenv` checks for local `~/Documents/GitHub/lingtai-kernel` + `~/Documents/GitHub/lingtai` and uses editable installs (`pip install -e`) if both exist.
- **`uv` preferred over `pip`:** all pip operations prefer `uv` if available (faster, can auto-download Python).
