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
  - portal/internal/migrate/m015_timemachine_gitignore.go
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

# portal/internal/migrate — Migration Registry (Portal)

> **Maintenance:** see `tui/internal/preset/skills/lingtai-tui-anatomy/SKILL.md`.

## What this is

Versioned, append-only, forward-only migration system for per-project `.lingtai/` state — the portal-side mirror of `tui/internal/migrate/`. Both binaries share the `meta.json` version space; every migration that touches shared on-disk state (init.json schema, preset paths, addon wiring) lives in both packages with identical logic. TUI-only migrations get a no-op stub here to preserve the version slot; portal-only migrations (m001 topology move) get a no-op stub in the TUI.

## Components

### Registry (`migrate.go`)
- `CurrentVersion` (`portal/internal/migrate/migrate.go:17`) — `39`. Must match the TUI's `CurrentVersion` exactly; the cross-binary contract requires lockstep bumps.
- `metaFile` struct (`portal/internal/migrate/migrate.go:19-21`) — `{"version": N}` shape of `.lingtai/meta.json`.
- `Migration` type (`portal/internal/migrate/migrate.go:24-28`) — version + name + function.
- `migrations` slice (`portal/internal/migrate/migrate.go:31-71`) — the append-only ordered list of all 39 migrations. **Real** entries have a named migration function; **no-op stubs** use `func(_ string) error { return nil }`.

### Real migrations (touch shared on-disk state)
- **m001** — `topology-to-portal` (`portal/internal/migrate/m001_topology.go:9-31`). Portal-only: moves `topology.jsonl` from `.tui-asset/` to `.portal/`.
- **m002** — `tape-normalize` (`portal/internal/migrate/m002_tape_normalize.go:7-9`). **Now a no-op.** Tape normalization is handled by `ReconstructTape` at portal startup.
- **m003** — `character-to-lingtai` — rewrites `.lingtai/character/` → `.lingtai/` (shared).
- **m004** — `relative-addressing` — rewrites absolute paths to relative in `init.json` (shared).
- **m006** — `relative-addressing-fix` — second pass: calls `migrateRelativeAddressing` again (shared).
- **m015** — `timemachine-gitignore` (`m015_timemachine_gitignore.go`). Writes a `.gitignore` to the `.lingtai/` root for Time Machine support (shared).
- **m026** — `preset-path-form` (`portal/internal/migrate/m026_preset_path_form.go:21`). Rewrites stem-form preset refs to path-form in `init.json` (shared).
- **m027** — `strip-media-capabilities` — drops `compose`/`video`/`draw`/`talk`/`listen` from `init.json` (shared).
- **m028** — `addons-to-mcp` — rewrites legacy `addons:{name:cfg}` → `addons:[name]` + `mcp.{name}` entries (shared).
- **m029** — `preset-allowed-list` — rewrites `manifest.preset` to `{default, active, allowed:[paths]}` schema (shared).
- **m030** — `preset-dir-split` — rewrites flat `presets/` paths to `templates/` or `saved/` subdirs (shared).
- **m031** — `drop-legacy-intrinsic-capabilities` — drops `psyche`/`email` from `init.json` (shared).
- **m035** — `remove-brief` (`portal/internal/migrate/m035_remove_brief.go:13`). Portal mirror of the TUI's brief-cleanup migration: deletes `system/brief.md` for each agent, drops `brief`/`brief_file` from `init.json`, and drops `brief` from `human/settings.json`. Touches shared on-disk state — either binary may be the first to open a post-secretary-removal project, so identical logic lives in both packages.
- **m038** — `agent-init-skills-paths` (`m038_agent_init_skills_paths.go`). Restores missing `skills.paths` in per-agent `init.json` files hit by the preset editor model-switch bug (PR #312). Shared on-disk state — whichever binary migrates first must repair.
- **m039** — `agent-init-context-preset-repair` (`m039_agent_init_context_preset_repair.go`). Combined catch-up: (1) calls `migrateAgentInitSkillsPaths` idempotently, then (2) copies legacy root `manifest.context_limit` into `manifest.llm.context_limit`, and (3) rewrites stale codex.json preset refs. The dual call ensures projects stamped at v38 by either old branch binary receive both repairs. See "Versions 38 and 39 — collision history" below.

**Versions 38 and 39 — collision history.** PR #340 (`docs/guide-custom-preset-tutorial`) and PR #357 (`fix/agent-init-context-preset-migration-20260615`) independently claimed migration version 38. The collision was discovered when a project migrated by one branch binary got `data version 38 is newer than this binary supports (37)` after returning to origin/main. Resolution in `fix/migration-version-collision-20260620`: PR #340's repair (skills-paths) takes v38; PR #357's repair (context/preset) takes v39. m039 calls m038's function idempotently first, so any project previously stamped at v38 by either old binary still receives both repairs. See `tui/internal/migrate/ANATOMY.md` for the canonical version and the collision-recovery pattern note.

### No-op stubs (preserve version slots)
m005 (`soul-inquiry-source`), m007 (`normalize-ledger`), m008 (`recipe-state`), m009 (`procedures`), m010 (`legacy-addons-warn`), m011 (`session-backfill`), m012 (`session-resort`), m013 (`agora-rename`), m014 (`skills-groups`), m016 (`rename-pad-codex-library`), m017 (`rename-preset-caps`), m018 (`library-split`), m019 (`procedures-english-only`), m020 (`pseudo-agent-subscriptions`), m021 (`library-paths`), m022 (`recipe-lang-suffix`), m023 (`recipe-state-rename`), m024 (`add-active-preset`), m025 (`preset-description-object`), m032 (`cleanup-codex-oauth`), m033 (`strip-codex-api-key-env`), m034 (`library-skills-caps`), m036 (`sqlite-log-backfill`), m037 (`preset-skills-paths`). (m035, m038, m039 are real — listed above.)

All stubs are `TUI-only` — they touch `.tui-asset/`, global preset files, the TUI's saved-preset directory, or TUI-side capability aliases. The portal doesn't care about these but must hold the version slot so `meta.json` version numbers match.

### Core functions
- `StampCurrent(lingtaiDir)` (`portal/internal/migrate/migrate.go:76-90`) — writes `meta.json` at `CurrentVersion` without running migrations. Used for fresh projects.
- `Run(lingtaiDir)` (`portal/internal/migrate/migrate.go:95-137`) — reads `meta.json`, runs pending migrations in order, writes new version atomically (temp + rename). Returns `data version N is newer...` error if the project has been touched by a newer binary.

## Connections

- **Called by** `portal/main.go` at startup — both `StampCurrent` (if `meta.json` missing) and `Run` (if present and stale).
- **Reads/writes** `.lingtai/meta.json` (shared with the TUI). Real migrations read/write `init.json`, `.agent.json`, preset files, and `.lingtai/.portal/`.
- **Cross-reference** `tui/internal/migrate/` is the authoritative registry — every migration touching shared state is duplicated here. The TUI's registry has an equivalent set of no-op stubs for portal-only migrations. When adding a migration, bump `CurrentVersion` in **both** files in lockstep.

## Composition

- **Parent:** `portal/internal/` (portal binary packages).
- **Siblings:** `api/`, `fs/` — migrate runs before either, so both see a migrated filesystem.
- **Repo-root path:** `portal/internal/migrate/`. Mirror of `tui/internal/migrate/` under the same monorepo.

## State

`.lingtai/meta.json` — a single `{"version": N}` stamp. Written atomically (temp + rename) to survive partial writes. Both binary trees share this file; the portal's `Run()` checks that the version is `< CurrentVersion` and writes `CurrentVersion` on completion.

## Notes

- **The no-op stub contract.** TUI and portal share the `meta.json` version space. Adding a TUI-only migration requires a corresponding no-op stub in the portal registry — `Fn: func(_ string) error { return nil }` — to preserve the version slot. Otherwise the portal refuses to open any project the TUI has already migrated, producing: `data version N is newer than this binary supports (M); upgrade lingtai-portal`.
- **Lockstep bumps.** After any migration bump, rebuild both binaries: `(cd tui && make build) && (cd portal && make build)`. A stale portal binary against a freshly-migrated project fails with the same "newer than this binary supports" error.
- **m002 (`tape-normalize`) is a historical no-op.** It was real in an earlier portal version but its work is now done by `ReconstructTape` in `portal/internal/fs/reconstruct.go` at startup. The version slot is preserved; the function body is `return nil`.
- **No-op stubs are documented inline** in the registry (`portal/internal/migrate/migrate.go:31-71`) with TUI-only comments. Adding a new stub should follow the same pattern: version, name, explicit `Fn: func(_ string) error { return nil }`, and a `// TUI-only:` comment.
- **Collision-recovery pattern.** When two branches claim the same version and one has already migrated a real project, assign the earlier repair to the lower claimed version and add a combined catch-up at the next free slot. The catch-up calls the earlier function idempotently first, then applies its own logic. See m039 and `tui/internal/migrate/ANATOMY.md` for the canonical example and rule.
