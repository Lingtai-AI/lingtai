# portal/internal/migrate — Migration Registry (Portal)

> **Maintenance:** see `tui/internal/preset/skills/lingtai-tui-anatomy/SKILL.md`.

## What this is

Versioned, append-only, forward-only migration system for per-project `.lingtai/` state — the portal-side mirror of `tui/internal/migrate/`. Both binaries share the `meta.json` version space; every migration that touches shared on-disk state (init.json schema, preset paths, addon wiring) lives in both packages with identical logic. TUI-only migrations get a no-op stub here to preserve the version slot; portal-only migrations (m001 topology move) get a no-op stub in the TUI.

## Components

### Registry (`migrate.go`)
- `CurrentVersion` (`migrate.go:11`) — `34`. Must match the TUI's `CurrentVersion` exactly; the cross-binary contract requires lockstep bumps.
- `metaFile` struct (`migrate.go:13-15`) — `{"version": N}` shape of `.lingtai/meta.json`.
- `Migration` type (`migrate.go:18-22`) — version + name + function.
- `migrations` slice (`migrate.go:25-59`) — the append-only ordered list of all 34 migrations. **Real** entries have a named migration function; **no-op stubs** use `func(_ string) error { return nil }`.

### Real migrations (touch shared on-disk state)
- **m001** — `topology-to-portal` (`m001_topology.go:9-31`). Portal-only: moves `topology.jsonl` from `.tui-asset/` to `.portal/`.
- **m002** — `tape-normalize` (`m002_tape_normalize.go:7-9`). **Now a no-op.** Tape normalization is handled by `ReconstructTape` at portal startup.
- **m003** — `character-to-lingtai` — rewrites `.lingtai/character/` → `.lingtai/` (shared).
- **m004** — `relative-addressing` — rewrites absolute paths to relative in `init.json` (shared).
- **m006** — `relative-addressing-fix` — second pass: calls `migrateRelativeAddressing` again (shared).
- **m015** — `timemachine-gitignore` (`m015_timemachine_gitignore.go`). Writes a `.gitignore` to the `.lingtai/` root for Time Machine support (shared).
- **m026** — `preset-path-form` (`m026_preset_path_form.go:21`). Rewrites stem-form preset refs to path-form in `init.json` (shared).
- **m027** — `strip-media-capabilities` — drops `compose`/`video`/`draw`/`talk`/`listen` from `init.json` (shared).
- **m028** — `addons-to-mcp` — rewrites legacy `addons:{name:cfg}` → `addons:[name]` + `mcp.{name}` entries (shared).
- **m029** — `preset-allowed-list` — rewrites `manifest.preset` to `{default, active, allowed:[paths]}` schema (shared).
- **m030** — `preset-dir-split` — rewrites flat `presets/` paths to `templates/` or `saved/` subdirs (shared).
- **m031** — `drop-legacy-intrinsic-capabilities` — drops `psyche`/`email` from `init.json` (shared).

### No-op stubs (preserve version slots)
m005 (`soul-inquiry-source`), m007 (`normalize-ledger`), m008 (`recipe-state`), m009 (`procedures`), m010 (`legacy-addons-warn`), m011 (`session-backfill`), m012 (`session-resort`), m013 (`agora-rename`), m014 (`skills-groups`), m016 (`rename-pad-codex-library`), m017 (`rename-preset-caps`), m018 (`library-split`), m019 (`procedures-english-only`), m020 (`pseudo-agent-subscriptions`), m021 (`library-paths`), m022 (`recipe-lang-suffix`), m023 (`recipe-state-rename`), m024 (`add-active-preset`), m025 (`preset-description-object`), m032 (`cleanup-codex-oauth`), m033 (`strip-codex-api-key-env`), m034 (`library-skills-caps`).

All are `TUI-only` — they touch `.tui-asset/`, global preset files, the TUI's saved-preset directory, or TUI-side capability aliases. The portal doesn't care about these but must hold the version slot so `meta.json` version numbers match.

### Core functions
- `StampCurrent(lingtaiDir)` (`migrate.go:60-78`) — writes `meta.json` at `CurrentVersion` without running migrations. Used for fresh projects.
- `Run(lingtaiDir)` (`migrate.go:80-126`) — reads `meta.json`, runs pending migrations in order, writes new version atomically (temp + rename). Returns `data version N is newer...` error if the project has been touched by a newer binary.

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
- **No-op stubs are documented inline** in the registry (`migrate.go:25-59`) with TUI-only comments. Adding a new stub should follow the same pattern: version, name, explicit `Fn: func(_ string) error { return nil }`, and a `// TUI-only:` comment.
