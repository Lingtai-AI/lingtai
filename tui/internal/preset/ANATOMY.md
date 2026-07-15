---
related_files:
  - tui/ANATOMY.md
  - tui/internal/config/ANATOMY.md
  - tui/internal/fs/ANATOMY.md
  - tui/internal/migrate/ANATOMY.md
  - tui/internal/tui/ANATOMY.md
  - tui/internal/preset/preset.go
  - tui/internal/preset/preset_test.go
  - tui/internal/preset/recipe_apply.go
  - tui/internal/preset/recipe_apply_test.go
  - tui/internal/preset/recipes.go
  - tui/internal/preset/recipes_test.go
  - tui/internal/preset/rehydrate.go
  - tui/internal/preset/state.go
  - tui/internal/preset/state_test.go
  - tui/internal/preset/preset_allowed_revoke_test.go
  - tui/internal/preset/preset_propagate_test.go
  - tui/internal/preset/preset_agent_json_merge_test.go
  - tui/internal/preset/skills/lingtai-dev-guide/SKILL.md
  - tui/internal/preset/skills/lingtai-dev-guide/reference/skill-stewardship/SKILL.md
  - tui/internal/preset/skills/lingtai-update/SKILL.md
  - tui/internal/preset/skills/lingtai-preset-skill/SKILL.md
  - tui/internal/preset/skills/lingtai-preset-skill/reference/minimax/SKILL.md
  - tui/internal/preset/skills/lingtai-preset-skill/reference/zhipu/SKILL.md
  - tui/internal/preset/skills/lingtai-preset-skill/reference/mimo/SKILL.md
  - tui/internal/preset/skills/lingtai-preset-skill/reference/deepseek/SKILL.md
  - tui/internal/preset/skills/lingtai-preset-skill/reference/gemini/SKILL.md
  - tui/internal/preset/skills/lingtai-preset-skill/reference/kimi/SKILL.md
  - tui/internal/preset/skills/lingtai-preset-skill/reference/nvidia/SKILL.md
  - tui/internal/preset/skills/lingtai-preset-skill/reference/openrouter/SKILL.md
  - tui/internal/preset/skills/lingtai-preset-skill/reference/codex/SKILL.md
  - tui/internal/preset/skills/lingtai-preset-skill/reference/codex-pool/SKILL.md
  - tui/internal/preset/skills/lingtai-preset-skill/reference/claude-agent-sdk/SKILL.md
  - tui/internal/preset/skills/lingtai-preset-skill/reference/custom/SKILL.md
  - tui/internal/preset/preset_skill_router_test.go
maintenance: |
  Keep related_files as repo-relative paths to real files. Include neighboring
  ANATOMY.md files so the anatomy graph stays connected rather than isolated;
  anatomy links must be bidirectional. If you create a new ANATOMY.md, copy this
  maintenance field. If you notice drift between this anatomy and the code,
  report it. See lingtai-dev-guide for details.
---

# preset

> **Maintenance:** see the `lingtai-tui-anatomy` skill at `tui/internal/preset/skills/lingtai-tui-anatomy/SKILL.md`. Coding agents update this file in same-commit as code changes.

## What this is

The preset package owns the atomic `{llm, capabilities}` bundle layer — loading, saving, listing, validating, and applying presets to agent `init.json` files. Presets live under `~/.lingtai-tui/presets/`; the directory (`templates/` vs `saved/`) IS the marker distinguishing built-in from user-owned — no in-band field.

## Components

| Symbol | Citation | Purpose |
|--------|----------|---------|
| `Preset` struct | `tui/internal/preset/preset.go:61` | `Name` + `Description` (structured object) + `Manifest` (raw JSON) + `Source` (runtime-only) |
| `PresetSource` | `tui/internal/preset/preset.go:75` | `SourceUnknown` / `SourceTemplate` / `SourceSaved` — directory-of-origin |
| `PresetDescription` | `tui/internal/preset/preset.go:99` | Structured `{summary, tier, Extra}` with custom marshal/unmarshal |
| `Load(name)` | `tui/internal/preset/preset.go:257` | saved/ first, then templates/; sets `Source` |
| `List()` | `tui/internal/preset/preset.go:210` | saved (alphabetical) + templates (canonical order); each carries `Source` |
| `Save(p)` | `tui/internal/preset/preset.go:373` | ALWAYS to `saved/`; never templates |
| `RefreshTemplates()` | `tui/internal/preset/preset.go:437` | rewrites `templates/` from `BuiltinPresets()`, prunes retired |
| `PopulateBundledLibrary(globalDir)` | `tui/internal/preset/preset.go:1288` | rewrites `~/.lingtai-tui/utilities/` from embedded `skills/` |
| `BuiltinPresets()` | `tui/internal/preset/preset.go:489` | minimax, zhipu, mimo, deepseek, gemini, kimi, nvidia, openrouter, codex, codex-pool, claude-agent-sdk, custom |
| `skills/lingtai-preset-skill/` | `tui/internal/preset/skills/lingtai-preset-skill/SKILL.md:1` | 12-child router; one nested manual per `BuiltinPresets()` name under `reference/<preset>/SKILL.md`. |
| `IsTemplate(p)` | `tui/internal/preset/preset.go:540` | canonical "is this read-only?" — prefer over `IsBuiltin(p.Name)` |
| `RefFor(p)` | `tui/internal/preset/preset.go:549` | `~/.lingtai-tui/presets/{templates\|saved}/<name>.json` |
| `ResolveRefsWithAuth(refs, keys, auth)` / `ResolveRefs(refs, keys)` | `tui/internal/preset/preset.go` | health-check: Source, Exists, HasKey (+ `CodexAuthRef`) for each preset path; credential validity requires configured `api_key_env`, Codex OAuth, or Claude Code CLI auth for `claude-agent-sdk`. For codex, when `AuthState.CodexAuthDir` is set, validity is judged per-preset against the preset's own `manifest.llm.codex_auth_path` token file (empty → legacy `codex-auth.json` fallback) so multiple Codex accounts are independent; without the dir it falls back to the global `CodexOAuthConfigured` bool |
| `Validate()` | `tui/internal/preset/preset.go:324` | mirrors kernel-side validation; `summary` non-empty, `tier` 1..5, `llm.provider`/`model` non-empty |
| `//go:embed` directives | `tui/internal/preset/preset.go:16-47` | covenant, principle, procedures, templates, soul, recipe_assets, skills |
| `skills/lingtai-dev-guide/` | `tui/internal/preset/skills/lingtai-dev-guide/SKILL.md:1`, `tui/internal/preset/skills/lingtai-dev-guide/reference/skill-stewardship/SKILL.md:1` | Bundled developer guide utility skill and its skill-stewardship nested reference, including the rule that skill authors keep routers lean and link dense content through progressive disclosure rather than encoding stale hard caps. |
| `CopyBundle` | `tui/internal/preset/recipe_apply.go:59` | copies `.recipe/` (replace) + recipe skill library sibling (merge) + `.lingtai/` (merge) into project |
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
- **Embeds prompt fragments** — covenant, principle, procedures, soul, templates, recipe_assets, skills — via `//go:embed`. These are the canonical TUI-shipped prompt text; the kernel reads them from disk after the TUI extracts them. Nested utility skills (for example `skills/swiss-knife/reference/<name>/SKILL.md`) are embedded and extracted as ordinary files under their parent router.

## Composition

- **Parent:** `tui/internal/` (no own anatomy)
- **Subfolders:** `covenant/`, `principle/`, `procedures/`, `templates/`, `soul/`, `recipe_assets/`, `skills/` — all `//go:embed` targets. `skills/swiss-knife/` is a top-level router whose nested utility references live under `skills/swiss-knife/reference/*/SKILL.md`. `skills/lingtai-preset-skill/` is another top-level router whose 12 nested references mirror `BuiltinPresets()` under `skills/lingtai-preset-skill/reference/*/SKILL.md`.
- **Siblings:** `tui/internal/migrate/ANATOMY.md` — migrations m029 (preset allowed list), m030 (preset dir split) live there

## State

- **`~/.lingtai-tui/presets/templates/*.json`** — TUI-owned; rewritten every Bootstrap from `BuiltinPresets()`. Retired templates deleted.
- **`~/.lingtai-tui/presets/saved/*.json`** — user-owned; `Save()` lands here; never touched by Bootstrap.
- **`~/.lingtai-tui/presets/_kernel_meta.json`** — skipped by `listFromDir`.
- **`~/.lingtai-tui/utilities/`** — TUI-owned utility skills; `PopulateBundledLibrary(globalDir)` rewrites this directory explicitly from embedded `skills/`.
- **`<project>/.lingtai/<agent>/init.json`** — `manifest.preset.{default, active, allowed}` written/patched by recipe apply and rehydration.
- **`<project>/.lingtai/.tui-asset/recipe-state.json`** — TUI-only project-level recipe selection state.

## Notes

- **Templates vs saved.** The directory IS the marker. `IsTemplate(p)` checks `p.Source == SourceTemplate`. Callers should prefer it over `IsBuiltin(p.Name)`. When writing `manifest.preset.*` paths, use `RefFor(p)` — it picks the right subdirectory from `Source`.
- **Authorization gate.** `manifest.preset.allowed` is the explicit list; the kernel refuses any swap not in it. `default` and `active` must both appear. m029 introduced this declarative form.
- **Saved name convention.** When a user edits a template, `AutoSavedName` picks `<template>-<N>` with the lowest unused N, so templates are never overwritten.
- **No in-band marker.** There is no `"is_template": true` field. Two presets with identical JSON but different directories are treated differently — `Source` is set at load time, never serialized.
