---
related_files:
  - tui/ANATOMY.md
  - tui/internal/preset/ANATOMY.md
  - tui/internal/config/ANATOMY.md
  - tui/internal/fs/ANATOMY.md
  - tui/internal/headless/headless.go
  - tui/internal/headless/headless_test.go
  - tui/internal/headless/presets.go
  - tui/internal/headless/presets_test.go
  - tui/internal/headless/spawn.go
  - tui/internal/headless/spawn_test.go
  - tui/main.go
maintenance: |
  Keep related_files as repo-relative paths to real files. Include neighboring
  ANATOMY.md files so the anatomy graph stays connected rather than isolated;
  anatomy links must be bidirectional. If you create a new ANATOMY.md, copy this
  maintenance field. If you notice drift between this anatomy and the code,
  report it. See lingtai-dev-guide for details.
---

# headless

> **Maintenance:** see the `lingtai-tui-anatomy` skill at `tui/internal/preset/skills/lingtai-tui-anatomy/SKILL.md`. Coding agents update this file in the same commit as code changes.

`headless` is the TUI's non-interactive, JSON-emitting CLI surface: the machine-readable half of `lingtai-tui`. It backs the `bootstrap`, `presets`, and `spawn` subcommands wired from `tui/main.go` so another agent (or a script) can inventory presets and create/launch an agent without a terminal UI. Everything it prints is one JSON document on stdout, and every failure is one JSON error object plus a nonzero exit code — never a Bubble Tea screen.

## Components

| Symbol | Citation | Purpose |
|---|---|---|
| `WriteJSON` | `tui/internal/headless/headless.go:10` | encodes one indented JSON document to the supplied writer — the single output primitive every headless command uses |
| `WriteError` / `WriteErrorDetail` | `tui/internal/headless/headless.go:17,25` | the structured failure shape (`{"error": ..., "code": ...}`, plus optional `details`) so callers can branch on `code` rather than parse prose |
| `ExitError` | `tui/internal/headless/headless.go:37` | writes the error document to stderr and exits nonzero; used directly by `tui/main.go` argument parsing before a subcommand is entered |
| `PresetEntry` / `PresetsOutput` | `tui/internal/headless/presets.go:10,19` | the `presets` output schema — name, source (template/saved), description, and the resolved ref an `init.json` can cite |
| `RunPresets` | `tui/internal/headless/presets.go:25` | lists presets through `internal/preset`, honoring the mutually exclusive `--saved-only` / `--templates-only` filters |
| `SpawnOpts` / `SpawnOutput` | `tui/internal/headless/spawn.go:30,40` | the `spawn` input options (target dir, preset, agent name, language) and its output document (agent dir, name, preset, PID, readiness) |
| `RunSpawn` | `tui/internal/headless/spawn.go:65` | the whole non-interactive create-and-launch path: global-dir resolution, `globalmigrate.Run`, project init, manifest write, launch, then a bounded readiness wait (`defaultReadyTimeout`, `tui/internal/headless/spawn.go:17`) before emitting JSON |

## Connections

- **Called by `tui/main.go`** — `bootstrapMain`, `presetsMain`, and `spawnMain` are thin argv parsers over this package. The adjacent `doctorMain` and `selfUpdateMain` subcommands deliberately do NOT route through here: they repair the local install via `internal/config` rather than emitting headless JSON.
- **Calls `tui/internal/preset/`** for preset enumeration and for seeding a new agent's `init.json`.
- **Calls `tui/internal/globalmigrate/`** (`tui/internal/headless/spawn.go:95`) so a headless spawn advances per-machine state exactly like an interactive start.
- **Calls `tui/internal/process/`** to init the project and launch the agent subprocess, and `tui/internal/fs/` to observe the new agent's manifest/heartbeat while waiting for readiness.

## Composition

- **Parent:** `tui/` (`tui/ANATOMY.md`)
- **Subfolders:** none — flat package
- **Siblings:** `tui/internal/tui/` is the interactive counterpart; both drive the same `preset`/`process`/`fs` layers.

## Notes

- **stdout is a protocol.** Anything printed on stdout by a headless command must be part of the documented JSON document. Diagnostics belong on stderr. Adding a stray `fmt.Println` breaks every scripted caller.
- **Readiness is bounded, not guaranteed.** `RunSpawn` waits up to `defaultReadyTimeout` for the agent to publish a heartbeat and reports what it observed; it does not block forever, and a not-yet-ready agent is reported, not treated as a failure to launch.
- **Error codes are the stable surface.** `invalid_args`, `init_failed`, `bootstrap_failed`, and the spawn-specific codes are consumed by callers. Rename one only with the callers in the same change.
