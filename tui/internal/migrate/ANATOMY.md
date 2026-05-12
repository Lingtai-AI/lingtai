# migrate (TUI)

> **Maintenance:** see the `lingtai-tui-anatomy` skill at `tui/internal/preset/skills/lingtai-tui-anatomy/SKILL.md`. Coding agents update this file in same-commit as code changes. **Cross-binary contract:** when bumping `CurrentVersion`, also bump `portal/internal/migrate/migrate.go`.

## What this is

Versioned, append-only, forward-only migration system for per-project `.lingtai/` state. The TUI and portal share the `meta.json` version space; both registries must bump in lockstep. Each `m<NNN>_<name>.go` file registers one migration; `Run()` runs pending migrations sequentially, then stamps `CurrentVersion` into `meta.json`.

## Components

| Symbol | Citation | Purpose |
|--------|----------|---------|
| `CurrentVersion` | `tui/internal/migrate/migrate.go:12` | latest version compiled into this binary (currently 34) |
| `Migration` struct | `tui/internal/migrate/migrate.go:20` | `{Version int, Name string, Fn func(string) error}` |
| `migrations` slice | `tui/internal/migrate/migrate.go:27` | ordered list of all m001..m034, append-only |
| `Run(lingtaiDir)` | `tui/internal/migrate/migrate.go:68` | reads `meta.json` → runs pending migrations → persists atomically |
| `StampCurrent(lingtaiDir)` | `tui/internal/migrate/migrate.go:114` | stamps `CurrentVersion` without running migrations (fresh projects) |
| `metaFile` struct | `tui/internal/migrate/migrate.go:14` | `{Version int, AddonCommentCleanupNotified bool}` |
| `persistMeta` | `tui/internal/migrate/migrate.go:139` | atomic temp+rename write of `meta.json` |
| m001 | `tui/internal/migrate/m001_topology.go:9` | move `topology.jsonl` from `.tui-asset/` to `.portal/` |
| m015 | `tui/internal/migrate/m015_timemachine_gitignore.go` | add `.gitignore` to timemachine dir |
| m026 | `tui/internal/migrate/m026_preset_path_form.go` | rewrite preset paths to `~/...` form |
| m029 | `tui/internal/migrate/m029_preset_allowed_list.go:32` | legacy `path` → declarative `allowed` list |
| m030 | `tui/internal/migrate/m030_preset_dir_split.go` | split flat `presets/` → `templates/` + `saved/` |
| m033 | `tui/internal/migrate/m033_strip_codex_api_key_env.go:21` | strip bogus `api_key_env` from saved codex presets |
| m034 | `tui/internal/migrate/m034_library_skills_caps.go:17` | rename capability keys `codex`→`library` and `library`→`skills` |

Each migration file exports one `func migrateXxx(lingtaiDir string) error`. m002 is a no-op `func(_ string) error { return nil }` — it preserves the version slot.

## Connections

- **Called by** `tui/main.go` — Bootstrap runs `Run(lingtaiDir)` on startup; `InitProject` calls `StampCurrent` for fresh projects.
- **Reads/writes** `<project>/.lingtai/meta.json` — version stamp shared with portal.
- **Migrations touch** `init.json`, `presets/saved/`, `.portal/`, `.tui-asset/`, and agent subdirectories.
- **Cross-binary contract with portal** — see `portal/internal/migrate/ANATOMY.md`. Each `CurrentVersion` bump must be mirrored. Migrations touching shared state (e.g. m029, m030) live in both packages. TUI-only migrations get a no-op stub in the portal registry.

## Composition

- **Parent:** `tui/internal/` (no own anatomy)
- **Subfolders:** none — flat package, one file per migration
- **Siblings:** `tui/internal/preset/ANATOMY.md` — m029/m030 reference preset directory layout; `tui/internal/globalmigrate/` — per-machine analogue under `~/.lingtai-tui/`

## State

- **`<project>/.lingtai/meta.json`** — `{"version": <N>, "addon_comment_cleanup_notified": <bool>}`. Written atomically (temp+rename) on every migration run.
- **Per-agent `init.json`** — mutated by several migrations (m017 rename caps, m024 add active preset, m026 path form, m027 strip media, m028 addons→MCP, m029 allowed list, m034 library/skills capability keys).

## Notes

- **Append-only, forward-only.** Never delete or reorder `migrations` entries. Never implement reverse migrations. A version bump is permanent — downstream binaries refuse to open projects stamped with a newer version.
- **Cross-binary contract.** This is the single most important rule in the package. When bumping `CurrentVersion`, you MUST also bump `portal/internal/migrate/migrate.go`. Shared migrations need identical logic in both packages. TUI-only migrations get a no-op stub in the portal. Otherwise the portal refuses to open any project the TUI has touched.
- **No-op stubs preserve slots.** m002 is `func(_ string) error { return nil }` — its slot in the register must be held so later migration version numbers don't shift.
- **Fresh-project shortcut.** `StampCurrent` writes `CurrentVersion` without running migrations — a freshly-generated project conforms to the current schema by construction. Running historical migrations against it would corrupt valid data (e.g. m016 renames `library→codex`, but a fresh project never had the old key).
- **Version-check error.** `Run` returns `"data version N is newer than this binary supports (M); upgrade lingtai-tui"` when meta.json version exceeds `CurrentVersion`.
- **Idempotent.** Migrations are designed to be re-runnable — they check preconditions and skip if already applied (e.g. m029 checks for existing `allowed` list).
