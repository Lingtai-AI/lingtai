---
related_files:
  - portal/ANATOMY.md
  - tui/internal/migrate/ANATOMY.md
  - portal/internal/migrate/migrate.go
  - portal/internal/migrate/migrate_test.go
  - portal/internal/migrate/collision_repair_test.go
  - portal/internal/migrate/m001_topology.go
  - portal/internal/migrate/m002_tape_normalize.go
  - portal/internal/migrate/m003_character_to_lingtai.go
  - portal/internal/migrate/m004_relative_addressing.go
  - portal/internal/migrate/m026_preset_path_form.go
  - portal/internal/migrate/m027_strip_media_capabilities.go
  - portal/internal/migrate/m028_addons_to_mcp.go
  - portal/internal/migrate/m029_preset_allowed_list.go
  - portal/internal/migrate/m030_preset_dir_split.go
  - portal/internal/migrate/m031_drop_legacy_intrinsic_capabilities.go
  - portal/internal/migrate/m035_remove_brief.go
  - portal/internal/migrate/m038_agent_init_skills_paths.go
  - portal/internal/migrate/m039_agent_init_context_preset_repair.go
maintenance: |
  Keep related_files as repo-relative paths to real files. Include neighboring
  ANATOMY.md files so the anatomy graph stays connected rather than isolated;
  anatomy links must be bidirectional. If you create a new ANATOMY.md, copy this
  maintenance field. If you notice drift between this anatomy and the code,
  report it. See lingtai-dev-guide for details.
---

# portal/internal/migrate — retained migration history

> **Maintenance:** see `tui/internal/preset/skills/lingtai-tui-anatomy/SKILL.md`.

## What this is

The Portal-side historical mirror of `tui/internal/migrate/`. Its m001–m039
registry, source, and tests remain for Git history and direct historical unit
coverage. Portal production no longer imports this package from `portal/main.go`
and does not read, write, or advance project migration progress.

## Components

- `CurrentVersion` (`portal/internal/migrate/migrate.go:17`) — retained value
  `39`, for parity/history only.
- `metaFile` and `Migration` (`portal/internal/migrate/migrate.go:19-28`) — old
  metadata and registry entry shapes.
- `migrations` (`portal/internal/migrate/migrate.go:31-80`) — ordered m001–m039
  history, including no-op slots.
- `StampCurrent` (`portal/internal/migrate/migrate.go:86-100`) — historical fresh
  project stamp helper; no Portal production caller.
- `Run` (`portal/internal/migrate/migrate.go:105-167`) — historical runner that
  reads pending entries and atomically advances `meta.json`; no Portal
  production caller.
- Historical shared-state entries include topology/path/preset repairs and
  m035/m038/m039, cited at `portal/internal/migrate/m001_topology.go:9-31`,
  `portal/internal/migrate/m026_preset_path_form.go:21`,
  `portal/internal/migrate/m035_remove_brief.go:13`,
  `portal/internal/migrate/m038_agent_init_skills_paths.go:1-36`, and
  `portal/internal/migrate/m039_agent_init_context_preset_repair.go:1-60`.

The six authorized m040/preflight paths are intentionally absent. No m001–m039
source, test, registry directory, or `migration/migration.md` was deleted.

## Connections

- **Historical callers:** migration tests and parity tests invoke the retained
  helpers directly. Production Portal startup proceeds from `.lingtai/` to its
  `.portal/` server setup without this package.
- **Historical state:** old helpers targeted shared `.lingtai/meta.json`, agent
  `init.json`, presets, and Portal assets. Current Portal startup leaves those
  files untouched; format reconstruction in `portal/internal/api/` remains its
  own data inspection path, not a project migration registry.
- **Cross-binary history:** the TUI mirror keeps matching entries for historical
  parity; future config repair belongs to kernel canonical plus explicit Agent
  edits, not a new registry/version gate.

## Composition

- **Parent:** `portal/internal/`.
- **Siblings:** `portal/internal/api/` and `portal/internal/fs/`; they no longer
  receive a migrated filesystem from this package.
- **Mirror:** `tui/internal/migrate/ANATOMY.md` documents the TUI-side history.

## State

- **Historical only:** `.lingtai/meta.json` with its old version field is a test
  fixture/API surface, not Portal-maintained runtime state.
- **Historical targets:** retained entries document prior mutations of `init.json`,
  preset directories, `.portal/`, and agent files.

## Notes

- `CurrentVersion = 39` and the contiguous registry remain only so m001–m039
  history/tests continue to compile and pass. Do not add production callers,
  stamps, version checks, preflights, or automatic rewrites.
- The old TUI/Portal lockstep and collision rules explain historical source and
  parity tests; they are not a live startup contract after runtime retirement.
- Exactly six authorized m040/preflight files remain deleted. Retained m001–m039
  code/tests and `migration/migration.md` are protected historical evidence.
