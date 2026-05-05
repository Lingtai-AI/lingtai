# lingtai

> **Maintenance:** see the `lingtai-tui-anatomy` skill (at `tui/internal/preset/skills/lingtai-tui-anatomy/SKILL.md`, ships to `~/.lingtai-tui/utilities/lingtai-tui-anatomy/`). **Coding agents** update this file in the same commit as code changes. **LingTai agents** report drift as issues (mail or `discussions/<name>-patch.md`); do not silently fix.

This repo is the Go-side of the LingTai project: two binary targets (`lingtai-tui` and `lingtai-portal`) plus the canonical install pipeline. All Python code (kernel runtime + `lingtai` PyPI package) lives in the sibling repo `lingtai-kernel`. The TUI launches Python agents as subprocesses and observes them via the filesystem; neither binary has a runtime Python dependency.

> **What is an `ANATOMY.md`?** See the canonical convention at `tui/internal/preset/skills/lingtai-tui-anatomy/SKILL.md` for this Go monorepo, or `lingtai-kernel/src/lingtai/intrinsic_skills/lingtai-kernel-anatomy/SKILL.md` for the Python kernel. Both follow the same 6-section template; the TUI skill covers the two-binary-symmetry contract that the kernel skill doesn't have.

## Components

The repo root holds two binary trees plus shared infrastructure. Each binary is a self-contained Go module; they communicate with running agents purely through the agent's working directory (`.lingtai/<agent>/`).

- **`tui/`** — Terminal UI binary (`lingtai-tui`). Bubble Tea v2 + lipgloss v2. Single-binary launcher, agent monitor, first-run wizard, mail viewer, preset editor. Builds to `tui/bin/lingtai-tui`. The flat `tui/main.go` (~889 lines) wires subcommands (`purge`, `list`, `clean`, `suspend`, `timemachine`, `postman`) and the interactive entry; everything substantive is under `tui/internal/`. See the per-package summary below.
- **`portal/`** — Web portal binary (`lingtai-portal`). Go HTTP server with an embedded React frontend served from a single binary via `embed.FS`. Reads the same `.lingtai/` filesystem the TUI does, surfaces a network visualisation, mail/replay UI, and topology recorder. Builds to `portal/bin/lingtai-portal`. Per-package layout under `portal/internal/`.
- **`install.sh`** — One-command installer (`curl … | bash`). Builds both binaries from source and installs them into Homebrew's `bin` directory. Auto-detects CN-restricted networks and falls back to mirrors for Go modules / `npm` / Go checksum DB.
- **`scripts/`** — Auxiliary Python utilities (image-to-blocks, Telegram chat dumper, tool description dumper, file-rename helper). NOT the runtime — these are dev-only tools.
- **`examples/`** — Reference config files (`init.jsonc`, `bash_policy.json`, `imap.jsonc`, `telegram.jsonc`) for users wiring up their own agents.
- **`docs/`** — User and developer docs (specs, plans, daily change log, screenshots, known limitations).
- **`prompt/`** — Localised prompt fragments shared across the TUI/portal.
- **`assets/`** — Static images (logos, screenshots) used by README and docs.
- **`README.md` / `README.zh.md` / `README.wen.md`** — Tri-lingual project README.
- **`RELEASING.md`** — Release process: tag, GitHub release, Homebrew tap bump.
- **`CLAUDE.md`** — Repo-specific Claude Code instructions (build commands, gotchas, sibling repos).

### `tui/` packages

| Package | LOC | Role |
|---------|-----|------|
| `tui/internal/tui/` | ~22k | Bubble Tea models for every screen — first-run wizard, network home (`app.go`), agent detail, mail composer, preset editor, codex/library/agora, doctor, addon installer. The biggest module by far; the `tui/` package is itself decomposable but the boundaries match Bubble Tea's screen-per-file convention. |
| `tui/internal/preset/` | — | Atomic `{llm, capabilities}` bundle layer. `preset.go` (~1900 lines) handles load/save/list, `recipe_apply.go` handles recipe import, `state.go` tracks user preset state. Embeds the canonical preset templates, covenant text, principles, soul fragments, procedures, skills, and recipe assets via `//go:embed`. |
| `tui/internal/migrate/` | — | Versioned, append-only, forward-only migration system for per-project `.lingtai/` state. Each `m<NNN>_<name>.go` registers in `migrate.go`; version stamped in `.lingtai/meta.json`. Currently at m033 (`m033_strip_codex_api_key_env.go`). The TUI and portal share the meta.json version space; both must bump in lockstep — see "Migration cross-package contract" in Notes. |
| `tui/internal/globalmigrate/` | — | Per-machine analogue under `~/.lingtai-tui/`. Same conventions, separate version space (`~/.lingtai-tui/meta.json`). For things like Homebrew tap renames, runtime venv relocations, preset directory split. |
| `tui/internal/fs/` | — | Filesystem accessors: agent manifest, heartbeat, mail (read/list/write outbox), token ledger, location, network discovery, signal files, session JSONL load. The TUI's read-only window into a running agent's working directory. |
| `tui/internal/config/` | — | Global TUI config under `~/.lingtai-tui/`: `tui_config.json`, runtime venv resolution, addon registry. |
| `tui/internal/process/` | — | Subprocess launcher (`launcher.go`). Spawns `python -m lingtai run <dir>` with the right venv, log redirection, and PID tracking. |
| `tui/internal/postman/` | — | UDP/IPv6 cross-internet agent mesh (邮差). Standalone subcommand `lingtai-tui postman`. See `docs/plans/` for design. |
| `tui/internal/timemachine/` | — | Git-backed history daemon for the `human/` directory. Runs as `lingtai-tui timemachine <dir>` subcommand; commits topology snapshots so `/viz` can replay history. |
| `tui/internal/secretary/` | — | Assistant subsystem; embeds asset bundles for the secretary screen. |
| `tui/i18n/` | — | en/zh/wen JSON tables. **Three locales always** — adding a key requires updating all three. Missing keys render as the raw key string. |
| `tui/scripts/` | — | Build helper scripts (cross-compile, asset bundling). |
| `tui/packages/` | — | Vendored or generated dependency artefacts. |
| Per-OS `*_unix.go` / `*_windows.go` | — | Platform-specific shims for `agent_count`, `exec`, `list`, `purge`, `suspend` subcommands. |

### `portal/` packages

| Package | LOC | Role |
|---------|-----|------|
| `portal/internal/api/` | ~1.5k | HTTP server (`server.go`), handlers (`handlers.go`), and replay endpoint (`replay.go` — 655 lines, the largest single API surface). Listens on a randomly-chosen port (or `--port`), writes the bound port to `.portal/port` so the TUI can find it. |
| `portal/internal/fs/` | ~2.2k | Same shape as `tui/internal/fs/` but tailored to portal's needs: agent reading, heartbeat, mail, network/topology reconstruction (`reconstruct.go`, 326 lines), location resolution. |
| `portal/internal/migrate/` | — | Versioned migrations for portal-readable state. Shares `meta.json` version space with the TUI; portal-only migrations get a TUI no-op stub and vice versa. Currently mirrors m001 / m002 / m003 / m004 / m015 / m026–m031 (the entries that touch shared on-disk state). |
| `portal/web/` | — | React 19 + TypeScript + Vite frontend. Source under `portal/web/src/` (`App.tsx`, `Graph.tsx`, `BottomBar.tsx`, `FilterPanel.tsx`, etc.). Builds to `portal/web/dist/` then `embed.go` (`//go:embed all:web/dist`) compiles it into the Go binary. |
| `portal/i18n/` | — | en/zh/wen JSON tables. Independent of the TUI's i18n — same three-locale rule. |
| `portal/docs/` | — | Portal-specific docs and screenshots. |

## Connections

- **TUI → kernel.** The TUI launches the kernel as a subprocess: `python -m lingtai run <agent-dir>` via `process/launcher.go`. The kernel is installed into `~/.lingtai-tui/runtime/venv/` (an isolated venv set up on first run via `pip install lingtai`). After spawn, the TUI never talks to the agent process directly — only via the agent's working directory.
- **TUI → filesystem (read).** `internal/fs/` reads `.lingtai/<agent>/.agent.json`, `.agent.heartbeat`, `mailbox/`, `logs/token_ledger.jsonl`, `history/chat_history.jsonl`, `system/*.md`. The kernel writes these; the TUI never writes them.
- **TUI → filesystem (write).** Signal files only: `.lingtai/<agent>/{.sleep, .suspend, .interrupt, .clear, .prompt, .refresh, .inquiry, .forget}`. The kernel polls these on each heartbeat tick. `init.json` is also writeable but only via explicit user actions in the wizard / preset editor.
- **TUI → human pseudo-mailbox.** The TUI is the user's MUA: it writes outbound messages into `.lingtai/human/mailbox/outbox/<uuid>/message.json`; agents poll this folder and claim deliveries.
- **Portal → filesystem.** Same read pattern as the TUI; additionally writes `.lingtai/.portal/port`, recordings under `.lingtai/.portal/recordings/`, and topology snapshots that feed the replay timeline.
- **Portal ↔ TUI integration.** `lingtai-tui` calls `gh release` and discovers an installed `lingtai-portal` to launch on `/viz`; otherwise the binaries are independent.
- **TUI ↔ Homebrew tap.** Releases push a new formula version to `huangzesen/homebrew-lingtai/lingtai-tui.rb`. Users running `brew upgrade lingtai-ai/lingtai/lingtai-tui` pull from there. See `RELEASING.md`.
- **Portal embeds web frontend.** `embed.go` at the portal root compiles `portal/web/dist/` into the Go binary so `lingtai-portal` ships with no runtime dependency on Node.

### Cross-repo dependencies

This repo depends on `lingtai-kernel` only at runtime (the Python agent it launches), not at build time. Other sibling repos:

- **`lingtai-kernel`** — Python kernel + `lingtai` PyPI package. Owns the canonical agent runtime.
- **`lingtai-skill`** — Single-source-of-truth for the mailbox-protocol `SKILL.md`. Vendored into plugin repos via `lingtai-claude-code/scripts/sync-from-canonical.sh`.
- **`lingtai-claude-code`** — Claude Code plugin (SessionStart hook, marketplace manifest).
- **`codex-plugin`** — OpenAI Codex CLI plugin.
- **`lingtai-imap` / `lingtai-telegram` / `lingtai-feishu` / `lingtai-wechat`** — MCP server addons. Each ships as a separate PyPI package.
- **`huangzesen/homebrew-lingtai`** — Homebrew tap for `lingtai-tui`.

## Composition

- **Parent:** none — this is a top-level repo.
- **Subfolders:** `tui/`, `portal/`, `docs/`, `examples/`, `prompt/`, `scripts/`, `assets/`. The TUI and portal each have full per-package internal trees with their own `internal/` boundaries.
- **Build outputs:** `tui/bin/lingtai-tui`, `portal/bin/lingtai-portal`. Cross-compile via `make cross-compile` in either directory (darwin/linux/windows × amd64/arm64).
- **Module names:** `github.com/anthropics/lingtai-tui` and `github.com/anthropics/lingtai-portal`. Note the historical naming — these are NOT moving to a `Lingtai-AI/` import path even though the GitHub org renamed.

## State

- **Per-project state** under `<project>/.lingtai/`:
  - `meta.json` — migration version stamp (shared between TUI and portal).
  - `<agent>/init.json` — the agent's preset manifest (LLM + capabilities + allowed presets list).
  - `<agent>/.agent.json` / `.agent.heartbeat` / `.status.json` — written by the agent, read by the TUI/portal.
  - `<agent>/mailbox/{inbox,outbox,sent,archive}/<uuid>/message.json` — filesystem mailbox.
  - `<agent>/.notification/<channel>.json` — `.notification/` filesystem-as-protocol payloads (email, soul, system events). Read-only from the TUI's perspective; the agent owns them.
  - `human/` — the user's pseudo-agent (no admin, no heartbeat). Mailbox layout identical to a real agent.
  - `.tui-asset/` — TUI-owned per-project caches (remotes list, etc.).
  - `.portal/port` / `.portal/recordings/` — portal-owned files when running.
- **Per-machine state** under `~/.lingtai-tui/`:
  - `meta.json` — global migration version stamp.
  - `tui_config.json` — global TUI preferences (default language, model selection, etc.).
  - `runtime/venv/` — Python venv with `lingtai` installed; agents launch from here.
  - `presets/templates/` — TUI-owned, rewritten on every Bootstrap from embedded data. Don't hand-edit.
  - `presets/saved/` — User-owned preset clones; the wizard's auto-clone-on-edit lands new presets here as `<template>-<N>.json`.
  - `utilities/` — Optional library paths surfaced to agents.

## Notes

- **Binary naming.** The TUI binary is `lingtai-tui`, never `lingtai`. `lingtai` is the Python agent CLI inside the runtime venv (`~/.lingtai-tui/runtime/venv/bin/lingtai`). Build to `tui/bin/lingtai-tui`; never `tui/bin/lingtai`.
- **Bubble Tea v2 paste delivery.** Bubble Tea v2 splits keys (`tea.KeyPressMsg`) from clipboard pastes (`tea.PasteMsg`). Any `Update` dispatcher gating on `case tea.KeyPressMsg:` must also forward `tea.PasteMsg` to whichever text widget is focused — otherwise paste silently drops. For embedded sub-models hosted inside another model (e.g. `PresetEditorModel` inside `FirstRunModel`), the host's outer `default:` branch must forward paste msgs into the sub-model. Trace top-down to find missing layers; the symptom is "typing works, paste does nothing."
- **`textarea` vs `textinput`.** For any paste-friendly field (API keys, base URLs), use `textarea` even when the content is conceptually one line. `textinput` drops characters on multi-byte / clipboard pastes. Always apply `themedTextareaStyles()` from the `tui` package — bare `textarea.New()` ships dark default cursor/focus colors that render as a black smear against the warm theme.
- **Migration cross-package contract.** TUI and portal share `meta.json` but have separate migration registries. Adding a TUI migration means bumping `CurrentVersion` in BOTH `tui/internal/migrate/migrate.go` and `portal/internal/migrate/migrate.go`. Migrations that touch shared state (preset paths, init.json schema) live in both packages with identical logic — copy the file across. TUI-only migrations get a no-op stub `Fn: func(_ string) error { return nil }` in the portal registry to preserve the version slot. Otherwise the portal refuses to open any project the TUI has already touched.
- **Dev-mode rebuild gotcha.** When running symlinked dev binaries (`/opt/homebrew/bin/lingtai-{tui,portal}` → `~/Documents/GitHub/lingtai/{tui,portal}/bin/...`), a stale portal binary against a freshly-migrated project fails with `data version N is newer than this binary supports (M); upgrade lingtai-portal`. After ANY migration bump, rebuild BOTH:
  ```bash
  cd ~/Documents/GitHub/lingtai/tui && make build
  cd ~/Documents/GitHub/lingtai/portal && make build
  ```
  The brew-installed pair never hits this because they ship together at the same version; dev mode hits it whenever you rebuild one and forget the other.
- **Preset architecture.** Presets are atomic `{llm, capabilities}` bundles. `templates/` is TUI-owned (rewritten every Bootstrap from embedded data, prunes retired entries — never hand-edit). `saved/` is user-owned (Bootstrap never touches it). The directory IS the answer to "is this a template?" — there's no in-band marker. Each loaded `Preset` carries a `Source` field (`SourceTemplate` / `SourceSaved`); prefer `IsTemplate(p)` over the legacy `IsBuiltin(p.Name)`. When writing `manifest.preset.*` paths from Go, always use `preset.RefFor(p)` to pick the right subdirectory based on `Source`.
- **Authorization gate.** `manifest.preset.allowed` is the explicit list of preset paths the agent may swap to at runtime. The kernel refuses any swap not in `allowed`. `default` and `active` MUST both appear in `allowed`; `init_schema.validate_init` enforces this. m029 was the migration that introduced this declarative form.
- **Three-locale rule.** Adding an i18n key means updating en.json, zh.json, AND wen.json in BOTH `tui/i18n/` and (where applicable) `portal/i18n/`. Missing translations show as the raw key on screen — they don't fall back. Procedural / dev-only strings can stay English-only with a comment noting why.
- **Filesystem-only IPC.** The TUI and portal never open a socket or RPC channel to a running agent. All communication is via files: agent manifests, heartbeats, signal files, mailbox folders, `.notification/`. This is the same boundary the kernel-side documents in `lingtai-kernel/src/lingtai_kernel/ANATOMY.md` "Notifications". Anything you'd want to add here that needs cross-process communication should follow the same pattern: write a file, let the other side poll.
