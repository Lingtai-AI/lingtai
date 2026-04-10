# Reusable Markdown Viewer Design

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Extract a reusable two-panel markdown viewer component from the `/skills` viewer. Use it for both `/skills` and recipe Ctrl+O preview, with a richer entry list (grouped, lang variants, imported vs bundled separation).

**Architecture:** A standalone `MarkdownViewerModel` that takes a pre-built list of `MarkdownEntry` items (with Label, Group, Path, and optional Content). The caller builds the entries — the viewer is pure display. Two consumers: `/skills` (builds from `scanSkills`) and recipe preview (builds by scanning the recipe directory).

**Tech Stack:** Go, Bubble Tea v2, glamour, lipgloss, viewport.

---

## 1. `MarkdownEntry` and `MarkdownViewerModel`

```go
type MarkdownEntry struct {
    Label   string // display name: "lingtai-agora v1.0.0", "greet.md (zh)"
    Group   string // section header: "Skills", "Imported", "Problems", "greet.md"
    Path    string // absolute path to file (read + render on selection)
    Content string // pre-built content (rendered instead of Path if non-empty)
}

type MarkdownViewerCloseMsg struct{}

func NewMarkdownViewer(entries []MarkdownEntry, title string) MarkdownViewerModel
```

### Viewer behavior

- **Left panel:** Entries rendered in groups. Groups appear in the order they first occur in the entries slice. Group headers are styled (accent + bold). Cursor navigates entries (not group headers). Selected entry is highlighted.
- **Right panel:** Glamour-rendered markdown of the selected entry. If `Content` is non-empty, render it directly. Otherwise read `Path` and render. YAML frontmatter (`---\n...\n---\n`) is stripped before rendering. Falls back to plain text wrapping if glamour fails.
- **Keyboard:** Up/Down move cursor. PgUp/PgDn/arrows scroll right panel. Esc returns `MarkdownViewerCloseMsg`. `r` reloads the selected file from disk.
- **Layout:** Same two-panel layout as current `/skills` — left column 1/3 width (25-40 chars), right column remainder, vertical separator.

## 2. What moves from `skills.go` to `mdviewer.go`

**Moves entirely:**
- Two-panel layout: `renderBody()`, column width calculation, line padding, panel joining with `│` separator
- Right panel rendering: glamour markdown rendering, frontmatter stripping, plain text fallback
- Viewport management: `syncViewportContent()`, resize handling
- View structure: header (title + separator), viewport, footer (hints + scroll indicator)

**Stays in `skills.go`:**
- `skillEntry`, `skillProblem` structs
- `scanSkills()` function (directory scanning, frontmatter parsing)
- `parseFrontmatter()` helper

**Deleted from `skills.go`:**
- `SkillsModel` struct (replaced by `MarkdownViewerModel`)
- All rendering code (`renderBody`, `renderLeft`, `renderRight`, `View`)
- All Update/Init code
- `skillsLoadMsg` message type
- `skillsHeaderLines`, `skillsFooterLines` constants

## 3. What moves from `firstrun.go`

**Deleted entirely:**
- `recipePreview`, `recipePreviewFile`, `recipePreviewVP`, `recipePreviewReady` fields
- `enterRecipePreview()` function
- `viewRecipePreview()` function
- `syncRecipePreviewContent()` function
- `renderRecipeFileContent()` function
- Recipe preview keyboard handling block in Update
- `renderRecipeSidePane()` function (the inline side pane in the picker)
- `recipeFilePreview()` helper

The recipe picker's Ctrl+O now builds an entry list and switches to the markdown viewer. The inline side pane (narrow preview alongside the picker) is also removed — the full two-panel viewer replaces it.

## 4. Entry building: `/skills`

`scanSkills()` returns `([]skillEntry, []skillProblem)` as today. A new function builds viewer entries:

```go
func buildSkillEntries(skillsDir string, skills []skillEntry, problems []skillProblem) []MarkdownEntry
```

Logic:
- For each `skillEntry`: check if it's a symlink via `os.Lstat(filepath.Join(skillsDir, dirName))`. Symlinks get group `"Imported"`, real dirs get group `"Skills"`.
- Label: `name` + version (if present), e.g. `"lingtai-agora v1.0.0"`
- Path: the skill's `Path` field (absolute path to SKILL.md)
- For each `skillProblem`: group `"Problems"`, label is folder name, `Content` is the reason string.

Order: Skills first, then Imported, then Problems.

## 5. Entry building: Recipe preview

A new function scans a recipe directory and builds viewer entries:

```go
func buildRecipeEntries(recipeDir string) []MarkdownEntry
```

Logic:
1. Scan for `greet.md` — check root and each lang subdir. For each found:
   - Group: `"greet.md"`
   - Label: `"greet.md"` (root) or `"greet.md (zh)"` (lang-specific)
   - Path: absolute path
2. Same for `comment.md` — group `"comment.md"`
3. Same for `recipe.json` — group `"recipe.json"`
4. Scan `skills/` subdirectory if present. For each skill, for each lang variant:
   - Group: `"Skills"`
   - Label: `"<skill-name>/SKILL.md"` or `"<skill-name>/SKILL.md (zh)"`
   - Path: absolute path

Order: greet.md variants, comment.md variants, recipe.json variants, skill variants.

## 6. Integration in `app.go`

### `/skills` command

Currently creates `SkillsModel`. Replace with:
1. Call `scanSkills(skillsDir)` to get entries + problems
2. Call `buildSkillEntries(skillsDir, skills, problems)` to get `[]MarkdownEntry`
3. Create `NewMarkdownViewer(entries, i18n.T("skills.title"))`
4. Switch to viewer view

### Recipe Ctrl+O

Currently sets `m.recipePreview = true`. Replace with:
1. Resolve current recipe directory (imported/custom/bundled)
2. Call `buildRecipeEntries(recipeDir)` to get `[]MarkdownEntry`
3. Return a message that tells `app.go` to switch to the markdown viewer (or handle within `FirstRunModel` by storing a `MarkdownViewerModel` and delegating)

**Design choice for recipe Ctrl+O:** Since `FirstRunModel` is a sub-model of `App`, and the recipe preview needs to return to the recipe picker (not to mail), the simplest approach is to store a `*MarkdownViewerModel` in `FirstRunModel` and delegate Update/View when it's active. When the viewer sends `MarkdownViewerCloseMsg`, nil it out and return to the picker.

```go
// In FirstRunModel:
recipeViewer *MarkdownViewerModel // non-nil when recipe preview is active
```

This is still approach (a) — standalone model — just embedded as a field rather than at the app level.

## 7. Files changed

| File | Change |
|---|---|
| `tui/internal/tui/mdviewer.go` | **New.** `MarkdownEntry`, `MarkdownViewerModel`, all rendering/viewport/layout code |
| `tui/internal/tui/skills.go` | **Slim down.** Keep `scanSkills`, `parseFrontmatter`, data structs. Add `buildSkillEntries`. Remove all rendering. |
| `tui/internal/tui/firstrun.go` | **Slim down.** Remove all recipe preview code. Add `recipeViewer *MarkdownViewerModel` field, `buildRecipeEntries`, delegation in Update/View. Remove `renderRecipeSidePane` and `recipeFilePreview`. |
| `tui/internal/tui/app.go` | Update `/skills` to build entries + create viewer |

## 8. What this design does NOT include

- Syntax highlighting for code blocks (glamour handles this already)
- Search/filter within the viewer
- Editing files from the viewer
- Caching rendered markdown (re-rendered on every cursor move, same as today)
