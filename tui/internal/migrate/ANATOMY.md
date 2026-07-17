---
related_files:
  - tui/ANATOMY.md
  - tui/internal/fs/ANATOMY.md
  - tui/internal/preset/ANATOMY.md
  - tui/internal/processscan/ANATOMY.md
  - portal/internal/migrate/ANATOMY.md
  - tui/internal/migrate/migrate.go
  - tui/internal/migrate/migrate_test.go
  - tui/internal/migrate/collision_repair_test.go
  - tui/internal/migrate/m001_topology.go
  - tui/internal/migrate/m026_preset_path_form.go
  - tui/internal/migrate/m029_preset_allowed_list.go
  - tui/internal/migrate/m030_preset_dir_split.go
  - tui/internal/migrate/m033_strip_codex_api_key_env.go
  - tui/internal/migrate/m034_library_skills_caps.go
  - tui/internal/migrate/m035_remove_brief.go
  - tui/internal/migrate/m036_sqlite_log_backfill.go
  - tui/internal/migrate/m037_preset_skills_paths.go
  - tui/internal/migrate/m038_agent_init_skills_paths.go
  - tui/internal/migrate/m039_agent_init_context_preset_repair.go
maintenance: |
  Keep related_files as repo-relative paths to real files. Include neighboring
  ANATOMY.md files so the anatomy graph stays connected rather than isolated;
  anatomy links must be bidirectional. If you create a new ANATOMY.md, copy this
  maintenance field. If you notice drift between this anatomy and the code,
  report it. See lingtai-dev-guide for details.
---

# migrate (TUI)

> **Maintenance:** see the `lingtai-tui-anatomy` skill at `tui/internal/preset/skills/lingtai-tui-anatomy/SKILL.md`. Coding agents update this file in same-commit as code changes.

## What this is

A retained historical package containing the append-only per-project migration
registry and its m001–m039 source/tests. The package remains navigable and
usable by its historical unit tests, but production TUI startup, project
creation, launcher, and diagnostics no longer import or call it. Runtime
compatibility diagnosis/repair is kernel- and Agent-owned; this package is not a
live startup component.

## Components

| Symbol | Citation | Purpose |
|--------|----------|---------|
| `CurrentVersion` | `tui/internal/migrate/migrate.go:18` | retained historical registry version, 39; not consulted or stamped by production |
| `Migration` struct | `tui/internal/migrate/migrate.go:26-30` | historical `{Version, Name, Fn}` registry entry shape |
| `migrations` slice | `tui/internal/migrate/migrate.go:33-81` | retained ordered m001–m039 history |
| `Run(lingtaiDir)` | `tui/internal/migrate/migrate.go:88-121` | historical test/API runner that reads and advances `meta.json`; no production caller |
| `StampCurrent(lingtaiDir)` | `tui/internal/migrate/migrate.go:128-134` | historical fresh-project stamp helper; no production caller |
| `metaFile` | `tui/internal/migrate/migrate.go:21-24` | historical `meta.json` shape, including the old notification field |
| `persistMeta` | `tui/internal/migrate/migrate.go:152-167` | historical atomic metadata writer |
| m001–m039 | `tui/internal/migrate/migrate.go:34-80` | preserved migration entries and source navigation for Git history/tests |
| m038/m039 | `tui/internal/migrate/m038_agent_init_skills_paths.go:1-31` / `tui/internal/migrate/m039_agent_init_context_preset_repair.go:1-68` | retained collision-resolution history; not automatic repairs |

The six authorized m040/preflight paths are intentionally absent. No m001–m039
source, test, registry directory, or `migration/migration.md` was deleted.

## Connections

- **Historical callers:** package tests invoke the retained helpers directly;
  production TUI code has zero `Run`/`StampCurrent` callers.
- **Historical state:** the old helpers read/write `.lingtai/meta.json` and old
  migrations can touch agent files, presets, or portal assets. Production no
  longer reads, writes, or advances project migration progress.
- **Cross-binary history:** `portal/internal/migrate/` retains the matching
  historical registry for parity tests; neither registry is a live startup
  dependency.

## Composition

- **Parent:** `tui/internal/` (no own anatomy)
- **Subfolders:** none — flat package, one file per historical migration
- **Siblings:** `tui/internal/preset/ANATOMY.md` (explicit writer ownership),
  `tui/internal/globalmigrate/` (separate per-machine housekeeping), and
  `portal/internal/migrate/ANATOMY.md` (historical mirror)

## State

- **Historical only:** `<project>/.lingtai/meta.json` with version/notification
  fields is an API/test fixture, not a TUI- or Portal-maintained runtime ledger.
- **Historical migration targets:** old entries document prior mutations of
  `init.json`, preset directories, `.portal/`, `.tui-asset/`, and agent files.
  Current explicit TUI writers are documented by `tui/internal/preset/ANATOMY.md`.

## Notes

- m001–m039 are preserved as non-executing production history. Their tests may
  call functions directly to keep historical behavior covered.
- `CurrentVersion = 39` is retained for package tests and registry parity only;
  do not add a production caller, version check, stamp, preflight, or registry
  gate. Future config changes are explicit Agent edits against kernel canonical.
- The old append-only/collision rules explain Git history, not a new runtime
  contract. The six m040/preflight deletions are the complete authorized
  deletion set for this recut.
