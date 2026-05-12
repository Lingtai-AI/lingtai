# preset

> **Maintenance:** see the `lingtai-tui-anatomy` skill at `tui/internal/preset/skills/lingtai-tui-anatomy/SKILL.md`. Coding agents update this file in same-commit as code changes.

## What this is

The preset package owns the atomic `{llm, capabilities}` bundle layer — loading, saving, listing, validating, and applying presets to agent `init.json` files. Presets live under `~/.lingtai-tui/presets/`; the directory (`templates/` vs `saved/`) IS the marker distinguishing built-in from user-owned — no in-band field.

## Components

| Symbol | Citation | Purpose |
|--------|----------|---------|
| `Preset` struct | `tui/internal/preset/preset.go:60` | `Name` + `Description` (structured object) + `Manifest` (raw JSON) + `Source` (runtime-only) |
| `PresetSource` | `tui/internal/preset/preset.go:74` | `SourceUnknown` / `SourceTemplate` / `SourceSaved` — directory-of-origin |
| `PresetDescription` | `tui/internal/preset/preset.go:98` | Structured `{summary, tier, Extra}` with custom marshal/unmarshal |
| `Load(name)` | `tui/internal/preset/preset.go:246` | saved/ first, then templates/; sets `Source` |
| `List()` | `tui/internal/preset/preset.go:209` | saved (alphabetical) + templates (canonical order); each carries `Source` |
| `Save(p)` | `tui/internal/preset/preset.go:330` | ALWAYS to `saved/`; never templates |
| `RefreshTemplates()` | `tui/internal/preset/preset.go:394` | rewrites `templates/` from `BuiltinPresets()`, prunes retired |
| `BuiltinPresets()` | `tui/internal/preset/preset.go:425` | minimax, zhipu, mimo, deepseek, openrouter, codex, custom |
| `IsTemplate(p)` | `tui/internal/preset/preset.go:464` | canonical "is this read-only?" — prefer over `IsBuiltin(p.Name)` |
| `RefFor(p)` | `tui/internal/preset/preset.go:473` | `~/.lingtai-tui/presets/{templates\|saved}/<name>.json` |
| `ResolveRefs(refs, keys)` | `tui/internal/preset/preset.go:528` | health-check: Source, Exists, HasKey for each preset path |
| `Validate()` | `tui/internal/preset/preset.go:282` | mirrors kernel-side validation; `summary` non-empty, `tier` 1..5, `llm.provider`/`model` non-empty |
| `//go:embed` directives | `tui/internal/preset/preset.go:16-47` | covenant, principle, procedures, templates, soul, recipe_assets, skills |
| `CopyBundle` | `tui/internal/preset/recipe_apply.go:59` | copies `.recipe/` (replace) + library (merge) + `.lingtai/` (merge) into project |
| `RecipeNeedsApply` | `tui/internal/preset/recipe_apply.go:133` | diffs `.recipe/` vs last-applied snapshot under `.tui-asset/.recipe/` |
| `ApplyRecipe` | `tui/internal/preset/recipe_apply.go:179` | writes `.prompt` + patches `skills.paths` per agent; snapshots `.recipe/` |
| `AppendSkillsPath` | `tui/internal/preset/recipe_apply.go:268` | idempotent append to `manifest.capabilities.skills.paths` |
| `AgentsMissingInit` | `tui/internal/preset/recipe_apply.go:331` | imported-network agents with `.agent.json` but no `init.json` |
| `RecipeState` | `tui/internal/preset/state.go:19` | `{Recipe, CustomDir}` — TUI-only, in `recipe-state.json` |
| `LoadRecipeState` / `SaveRecipeState` | `tui/internal/preset/state.go:35,52` | atomic read/write of `.lingtai/.tui-asset/recipe-state.json` |
| `RehydrateNetwork` | `tui/internal/preset/rehydrate.go:35` | propagates orchestrator `init.json` to worker agents; strips addons, admin |

## Connections

- **Called by `tui/internal/tui/`** — all Bubble Tea screens (network home, preset editor, first-run wizard, recipe selector).
- **Calls `tui/internal/config/`** — for `GlobalDirName` constant.
- **Reads/writes `~/.lingtai-tui/presets/`** — `templates/` (TUI-owned, rewritten on Bootstrap) and `saved/` (user-owned). Also reads/writes per-project `.lingtai/<agent>/init.json` and `.lingtai/.tui-asset/`.
- **Embeds prompt fragments** — covenant, principle, procedures, soul, templates, recipe_assets, skills — via `//go:embed`. These are the canonical TUI-shipped prompt text; the kernel reads them from disk after the TUI extracts them.

## Composition

- **Parent:** `tui/internal/` (no own anatomy)
- **Subfolders:** `covenant/`, `principle/`, `procedures/`, `templates/`, `soul/`, `recipe_assets/`, `skills/` — all `//go:embed` targets
- **Siblings:** `tui/internal/migrate/ANATOMY.md` — migrations m029 (preset allowed list), m030 (preset dir split) live there

## State

- **`~/.lingtai-tui/presets/templates/*.json`** — TUI-owned; rewritten every Bootstrap from `BuiltinPresets()`. Retired templates deleted.
- **`~/.lingtai-tui/presets/saved/*.json`** — user-owned; `Save()` lands here; never touched by Bootstrap.
- **`~/.lingtai-tui/presets/_kernel_meta.json`** — skipped by `listFromDir`.
- **`<project>/.lingtai/<agent>/init.json`** — `manifest.preset.{default, active, allowed}` written/patched by recipe apply and rehydration.
- **`<project>/.lingtai/.tui-asset/recipe-state.json`** — TUI-only project-level recipe selection state.

## Notes

- **Templates vs saved.** The directory IS the marker. `IsTemplate(p)` checks `p.Source == SourceTemplate`. Callers should prefer it over `IsBuiltin(p.Name)`. When writing `manifest.preset.*` paths, use `RefFor(p)` — it picks the right subdirectory from `Source`.
- **Authorization gate.** `manifest.preset.allowed` is the explicit list; the kernel refuses any swap not in it. `default` and `active` must both appear. m029 introduced this declarative form.
- **Saved name convention.** When a user edits a template, `AutoSavedName` picks `<template>-<N>` with the lowest unused N, so templates are never overwritten.
- **No in-band marker.** There is no `"is_template": true` field. Two presets with identical JSON but different directories are treated differently — `Source` is set at load time, never serialized.
