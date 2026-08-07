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
  - tui/internal/migrate/check_addon_comment.go
  - tui/internal/migrate/check_addon_comment_test.go
  - tui/internal/migrate/error_propagation_test.go
  - tui/internal/migrate/m003_character_to_lingtai.go
  - tui/internal/migrate/m004_soul_inquiry_source.go
  - tui/internal/migrate/m005_relative_addressing.go
  - tui/internal/migrate/m007_normalize_ledger.go
  - tui/internal/migrate/m008_recipe_state.go
  - tui/internal/migrate/m008_recipe_state_test.go
  - tui/internal/migrate/m009_procedures.go
  - tui/internal/migrate/m010_legacy_addons_warn.go
  - tui/internal/migrate/m011_session_backfill.go
  - tui/internal/migrate/m012_session_resort.go
  - tui/internal/migrate/m013_agora_rename.go
  - tui/internal/migrate/m014_skills_groups.go
  - tui/internal/migrate/m016_rename_pad_codex_library.go
  - tui/internal/migrate/m017_rename_preset_caps.go
  - tui/internal/migrate/m017_rename_preset_caps_test.go
  - tui/internal/migrate/m018_library_split.go
  - tui/internal/migrate/m018_library_split_test.go
  - tui/internal/migrate/m019_procedures_english_only.go
  - tui/internal/migrate/m020_pseudo_agent_subscriptions.go
  - tui/internal/migrate/m020_pseudo_agent_subscriptions_test.go
  - tui/internal/migrate/m021_library_paths.go
  - tui/internal/migrate/m021_library_paths_test.go
  - tui/internal/migrate/m022_recipe_lang_suffix.go
  - tui/internal/migrate/m022_recipe_lang_suffix_test.go
  - tui/internal/migrate/m023_recipe_state_rename.go
  - tui/internal/migrate/m023_recipe_state_rename_test.go
  - tui/internal/migrate/m024_add_active_preset.go
  - tui/internal/migrate/m024_add_active_preset_test.go
  - tui/internal/migrate/m025_preset_description_object.go
  - tui/internal/migrate/m025_preset_description_object_test.go
  - tui/internal/migrate/m026_preset_path_form_test.go
  - tui/internal/migrate/m027_strip_media_capabilities.go
  - tui/internal/migrate/m028_addons_to_mcp.go
  - tui/internal/migrate/m028_addons_to_mcp_test.go
  - tui/internal/migrate/m028_e2e_test.go
  - tui/internal/migrate/m029_preset_allowed_list_test.go
  - tui/internal/migrate/m031_drop_legacy_intrinsic_capabilities.go
  - tui/internal/migrate/m032_cleanup_codex_oauth.go
  - tui/internal/migrate/m034_library_skills_caps_test.go
  - tui/internal/migrate/m036_sqlite_log_backfill_test.go
  - tui/internal/migrate/m037_preset_skills_paths_test.go
  - tui/internal/migrate/m038_agent_init_skills_paths_test.go
  - tui/internal/migrate/m039_agent_init_context_preset_repair_test.go
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
- **`related_files` is this package's full inventory.** The repo-wide no-orphan rule (root `ANATOMY.md`, `## Anatomy convention`) requires every tracked file here to appear in the frontmatter above, and `TestArchitectureDocumentsCoverEveryTrackedFile` (`tui/architecture_documents_test.go`) fails when one is missing. The body stays the curated architectural map: adding a file does not oblige a new row above, but adding its `related_files` entry in the same commit is mandatory — and deleting a file means deleting its entry.
