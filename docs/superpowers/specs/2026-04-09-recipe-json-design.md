# recipe.json Manifest and Imported Recipe Picker Design

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a mandatory `recipe.json` manifest to every recipe, and surface auto-detected imported recipes (`.lingtai-recipe/` with valid `recipe.json`) as a first-class option in the recipe picker — separate from the manual "Custom" entry.

**Architecture:** `recipe.json` is resolved via the standard `<lang> → root` fallback. The picker gains an "Imported" slot at index 0 (above the separator, above Adaptive) when a valid imported recipe is detected. Internally it behaves like custom but is auto-detected.

**Tech Stack:** Go, existing `preset` and `tui` packages.

---

## 1. `recipe.json` Contract

Every recipe directory must contain a `recipe.json` resolvable via the i18n fallback chain (`<lang>/recipe.json → recipe.json`):

```json
{
  "name": "OpenClaw Explainer",
  "description": "Legal dataset walkthrough with 10 specialized agents"
}
```

- `name` — **required**, displayed in the picker
- `description` — **required**, shown as hint text
- Extra fields — ignored, forward-compatible

Root-level `recipe.json` is mandatory (per the i18n rule: root must always exist). Language-specific overrides are optional.

## 2. `RecipeInfo` and `LoadRecipeInfo`

New struct and loader in `preset/recipes.go`:

```go
type RecipeInfo struct {
    Name        string `json:"name"`
    Description string `json:"description"`
}
```

`LoadRecipeInfo(recipeDir, lang string) (RecipeInfo, error)` resolves `recipe.json` via `resolveRecipeFile`, parses JSON, validates that `name` is non-empty. Returns error if file not found or `name` is empty.

## 3. Imported Recipe Detection

`ProjectLocalRecipeDir(projectRoot string) string` currently returns the path if `.lingtai-recipe/` exists and is a directory. The detection is tightened:

- After confirming the directory exists, call `LoadRecipeInfo(dir, lang)`
- If `LoadRecipeInfo` fails (no `recipe.json` or invalid), return `""` — the directory is not a valid recipe
- The caller in `firstrun.go` uses the returned `RecipeInfo` for display

Note: `ProjectLocalRecipeDir` doesn't have `lang` today. Rather than changing its signature, the `firstrun.go` constructor calls `LoadRecipeInfo` separately after getting the path.

## 4. Picker Layout

When an imported recipe is detected:

```
  Recipe

  ● OpenClaw Explainer    Imported
  ─────────────────────────────────
  ○ Adaptive               Recommended
  ○ Greeter
  ○ Plain
  ○ Tutorial
  ○ Custom                 Enter folder path
```

When no imported recipe is detected (same as today):

```
  Recipe

  ● Adaptive               Recommended
  ─────────────────────────────────────
  ○ Greeter
  ○ Plain
  ○ Tutorial
  ○ Custom                 Enter folder path
```

### Index mapping

The picker indices are dynamic based on whether an imported recipe exists:

**With imported recipe:**

| Index | Recipe |
|---|---|
| 0 | Imported (from `.lingtai-recipe/`) |
| 1 | Adaptive |
| 2 | Greeter |
| 3 | Plain |
| 4 | Tutorial |
| 5 | Custom |

**Without imported recipe:** Same as today (0=Adaptive, 1=Greeter, 2=Plain, 3=Tutorial, 4=Custom).

The `recipeNameToIdx` and `recipeIdxToName` functions gain an `hasImported bool` parameter (or access the model's `importedRecipe` field) to shift indices accordingly.

## 5. New Constant

```go
const RecipeImported = "imported"
```

Stored in `.tui-asset/.recipe` as `{"recipe": "imported", "custom_dir": "/path/to/.lingtai-recipe"}`. Reuses the `CustomDir` field — imported recipes resolve greet/comment/skills from this directory, identical to custom.

## 6. Bundled Recipe Manifests

Each bundled recipe gets a root-level `recipe.json`:

**`adaptive/recipe.json`:**
```json
{"name": "Adaptive", "description": "Progressive feature discovery — introduces commands and capabilities as you need them"}
```

**`greeter/recipe.json`:**
```json
{"name": "Greeter", "description": "Comprehensive guided greeting with full feature overview"}
```

**`plain/recipe.json`:**
```json
{"name": "Plain", "description": "Minimal — no greeting, no behavioral constraints"}
```

**`tutorial/recipe.json`:**
```json
{"name": "Tutorial", "description": "Step-by-step walkthrough of lingtai features"}
```

## 7. Documentation Updates

- **`lingtai-recipe` skill (en + zh):** Add `recipe.json` as mandatory file in the directory structure section. Document the two required fields.
- **`lingtai-agora` skill:** Update Step 5 (launch recipe) to include `recipe.json` creation. The orchestrator asks the user for a name and description during the publish flow.

## 8. i18n

New keys (en.json, zh.json, wen.json):

- `recipe.imported` — "Imported" (hint label shown next to the recipe name)

## 9. Files Changed

| File | Change |
|---|---|
| `tui/internal/preset/recipes.go` | Add `RecipeInfo`, `LoadRecipeInfo`, `RecipeImported` constant |
| `tui/internal/preset/recipes_test.go` | Tests for `LoadRecipeInfo` |
| `tui/internal/preset/recipe_assets/adaptive/recipe.json` | New |
| `tui/internal/preset/recipe_assets/greeter/recipe.json` | New |
| `tui/internal/preset/recipe_assets/plain/recipe.json` | New |
| `tui/internal/preset/recipe_assets/tutorial/recipe.json` | New |
| `tui/internal/tui/firstrun.go` | Imported recipe detection, dynamic picker indices, `viewRecipe()` rendering |
| `tui/internal/preset/skills/lingtai-recipe/en/SKILL.md` | Document `recipe.json` |
| `tui/internal/preset/skills/lingtai-recipe/zh/SKILL.md` | Document `recipe.json` |
| `tui/internal/preset/skills/lingtai-agora/SKILL.md` | Update Step 5 |
| `tui/i18n/en.json` | Add `recipe.imported` |
| `tui/i18n/zh.json` | Add `recipe.imported` |
| `tui/i18n/wen.json` | Add `recipe.imported` |

## 10. What This Design Does NOT Include

- Validation that bundled recipes have `recipe.json` at startup (they're embedded, we control them)
- Displaying `description` from bundled recipe.json in the picker (bundled recipes use hardcoded i18n strings as today — `recipe.json` is for the contract, not for TUI display of bundled recipes)
- Recipe versioning or author fields (forward-compatible, not read)
