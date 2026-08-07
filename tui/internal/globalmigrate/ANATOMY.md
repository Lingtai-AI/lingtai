---
related_files:
  - tui/ANATOMY.md
  - tui/internal/migrate/ANATOMY.md
  - tui/internal/config/ANATOMY.md
  - tui/internal/preset/ANATOMY.md
  - tui/internal/globalmigrate/globalmigrate.go
  - tui/internal/globalmigrate/globalmigrate_test.go
  - tui/internal/globalmigrate/m001_tap_huangzesen_to_lingtai_ai.go
  - tui/main.go
  - tui/internal/headless/spawn.go
maintenance: |
  Keep related_files as repo-relative paths to real files. Include neighboring
  ANATOMY.md files so the anatomy graph stays connected rather than isolated;
  anatomy links must be bidirectional. If you create a new ANATOMY.md, copy this
  maintenance field. If you notice drift between this anatomy and the code,
  report it. See lingtai-dev-guide for details.
---

# globalmigrate

> **Maintenance:** see the `lingtai-tui-anatomy` skill at `tui/internal/preset/skills/lingtai-tui-anatomy/SKILL.md`. Coding agents update this file in the same commit as code changes.

`globalmigrate` is the per-*machine* migration registry: the analogue of `tui/internal/migrate/` whose scope is global state under `~/.lingtai-tui/`, not per-project state under each `.lingtai/`. It has its own version space (`~/.lingtai-tui/meta.json`) and runs forward-only, once per TUI launch, before the UI renders. Unlike the retired per-project registry, this one still executes in production — it is how machine-wide facts like a Homebrew tap rename or a runtime venv relocation get repaired.

## Components

| Symbol | Citation | Purpose |
|---|---|---|
| `CurrentVersion` | `tui/internal/globalmigrate/globalmigrate.go:18` | the latest global-migration version compiled into this binary (currently 2) |
| `metaFile` | `tui/internal/globalmigrate/globalmigrate.go:20` | the `~/.lingtai-tui/meta.json` shape — one `version` integer |
| `Migration` | `tui/internal/globalmigrate/globalmigrate.go:25` | one versioned step: `Version`, `Name`, and a `func(globalDir string) error` body |
| `migrations` | `tui/internal/globalmigrate/globalmigrate.go:47-50` | the append-only ordered registry. v1 `tap-huangzesen-to-lingtai-ai`; v2 `split-presets-dir` is a **neutralized no-op tombstone** |
| `Run` | `tui/internal/globalmigrate/globalmigrate.go:60` | reads the current version (absent ⇒ 0), runs pending steps in order, then atomically stamps the new version |
| `migrateTapHuangzesenToLingtaiAI` | `tui/internal/globalmigrate/m001_tap_huangzesen_to_lingtai_ai.go:9-` | adds the canonical `lingtai-ai/lingtai` tap for users who installed from `huangzesen/lingtai`; deliberately does **not** untap the old one, because brew refuses to untap the source of an installed formula and doing so would mean reinstalling the running binary mid-startup |

## Connections

- **Called by `tui/main.go`** (`tui/main.go:1001`, `tui/main.go:1457`) on the interactive startup and doctor paths, and by `tui/internal/headless/spawn.go:95` so a headless spawn advances machine state identically.
- **Runs before `preset.Bootstrap`** in the startup order — which is exactly why a destructive step here can eat user data (see Notes).
- **Reads/writes `~/.lingtai-tui/meta.json` only.** Directory resolution comes from `tui/internal/config/`.
- **Independent of `tui/internal/migrate/`.** Same conventions and shape, disjoint version space and target directory. Do not renumber across the two.

## Composition

- **Parent:** `tui/` (`tui/ANATOMY.md`)
- **Subfolders:** none — one registry file plus one `mNNN_*.go` per step
- **Siblings:** `tui/internal/migrate/` (per-project, retired from production execution), `tui/internal/config/` (owns the global directory)

## State

- **Reads/writes:** `~/.lingtai-tui/meta.json` (`{"version": N}`).
- **v1 additionally runs `brew tap`** as an external side effect on machines that have Homebrew.

## Notes

- **Best-effort, not a data fix.** `Run` reports individual step failures to stderr and continues; it never aborts startup. Anything that must not fail silently does not belong here.
- **Tombstones are kept, not deleted.** v2 (`split-presets-dir`) once moved flat `~/.lingtai-tui/presets/*.json` into `templates/`/`saved/` and, on a destination collision, silently *deleted* the source — the preset-loss incident. The destructive body was removed but the version entry is retained so machines at v1 still advance to v2 and machines at v2 see no change. `tui/internal/globalmigrate/globalmigrate_test.go` is the regression guard. Removing a version entry renumbers history and is never the right fix.
- **No i18n.** Steps print with `fmt.Println` because they run before the TUI renders a locale.
- **Append-only, forward-only.** New work is a new highest version with a new `mNNN_*.go` file; never edit a shipped step's behavior in place.
