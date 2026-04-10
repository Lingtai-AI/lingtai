# Recipe Covenant Override + Dump Path Rename — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow recipes to override the system-wide covenant, and fix the hourly dump path to use `brief/projects/<hash>/` instead of `brief/<hash>/`.

**Architecture:** Add `ResolveCovenantPath()` to the recipe system following the existing pattern for greet/comment resolution. Add `resolveRecipeCovenant()` helper in recipe_save.go and wire it into the init.json generation call sites. Rename the dump path by inserting `projects/` segment into `briefHistoryDir()`.

**Tech Stack:** Go

---

## File Structure

| File | Role |
|------|------|
| `tui/internal/fs/session_dump.go` | **Modify.** Update `briefHistoryDir()` path |
| `tui/internal/fs/session_dump_test.go` | **Modify.** Update test expected paths |
| `tui/internal/preset/recipes.go` | **Modify.** Add `ResolveCovenantPath()` |
| `tui/internal/preset/recipes_test.go` | **Modify.** Add tests for `ResolveCovenantPath()` |
| `tui/internal/tui/recipe_save.go` | **Modify.** Add `resolveRecipeCovenant()` |
| `tui/internal/tui/firstrun.go:3073-3076` | **Modify.** Wire covenant override at init.json generation |

---

### Task 1: Dump path rename

**Files:**
- Modify: `tui/internal/fs/session_dump.go:20-23`
- Modify: `tui/internal/fs/session_dump_test.go:25-33`

- [ ] **Step 1: Update `briefHistoryDir()` to include `projects/` segment**

In `tui/internal/fs/session_dump.go`, change:

```go
// briefHistoryDir returns <base>/brief/<hash>/history/.
func briefHistoryDir(base, hash string) string {
	return filepath.Join(base, "brief", hash, "history")
}
```

To:

```go
// briefHistoryDir returns <base>/brief/projects/<hash>/history/.
func briefHistoryDir(base, hash string) string {
	return filepath.Join(base, "brief", "projects", hash, "history")
}
```

- [ ] **Step 2: Update `TestBriefHistoryDir` expected path**

In `tui/internal/fs/session_dump_test.go`, change:

```go
func TestBriefHistoryDir(t *testing.T) {
	hash := "abcdef012345"
	base := "/tmp/test-tui"
	dir := briefHistoryDir(base, hash)
	want := filepath.Join(base, "brief", hash, "history")
```

To:

```go
func TestBriefHistoryDir(t *testing.T) {
	hash := "abcdef012345"
	base := "/tmp/test-tui"
	dir := briefHistoryDir(base, hash)
	want := filepath.Join(base, "brief", "projects", hash, "history")
```

- [ ] **Step 3: Update integration test expected paths**

In `TestSessionCacheHourBoundaryDump`, `TestSessionCacheMultiHourDump`, and `TestSessionCacheIdempotentDump`, the `histDir` construction uses `filepath.Join(dir, "brief", hash, "history")`. Update all three to:

```go
histDir := filepath.Join(dir, "brief", "projects", hash, "history")
```

These tests are at lines 216, 261, and 308 approximately. Search for `"brief", hash, "history"` in the test file and replace all occurrences with `"brief", "projects", hash, "history"`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd tui && go test ./internal/fs/ -run "TestBrief|TestSessionCache" -v`
Expected: All PASS

- [ ] **Step 5: Build to verify compilation**

Run: `cd tui && make build`
Expected: Builds successfully

- [ ] **Step 6: Commit**

```bash
git add tui/internal/fs/session_dump.go tui/internal/fs/session_dump_test.go
git commit -m "refactor(fs): rename dump path to brief/projects/<hash>/history/"
```

---

### Task 2: Add `ResolveCovenantPath` to recipe system

**Files:**
- Modify: `tui/internal/preset/recipes.go:78-82`
- Modify: `tui/internal/preset/recipes_test.go`

- [ ] **Step 1: Write the failing tests for `ResolveCovenantPath`**

Add to `tui/internal/preset/recipes_test.go`:

```go
func TestResolveCovenantPath_LangSpecific(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "en"), 0o755)
	want := filepath.Join(dir, "en", "covenant.md")
	os.WriteFile(want, []byte("test covenant"), 0o644)
	got := ResolveCovenantPath(dir, "en")
	if got != want {
		t.Errorf("ResolveCovenantPath prefers lang-specific, got %q, want %q", got, want)
	}
}

func TestResolveCovenantPath_FallbackToRoot(t *testing.T) {
	dir := t.TempDir()
	want := filepath.Join(dir, "covenant.md")
	os.WriteFile(want, []byte("root covenant"), 0o644)
	got := ResolveCovenantPath(dir, "en")
	if got != want {
		t.Errorf("ResolveCovenantPath fallback to root, got %q, want %q", got, want)
	}
}

func TestResolveCovenantPath_Empty(t *testing.T) {
	dir := t.TempDir()
	got := ResolveCovenantPath(dir, "en")
	if got != "" {
		t.Errorf("ResolveCovenantPath empty dir = %q, want empty string", got)
	}
}

func TestResolveCovenantPath_EmptyRecipeDir(t *testing.T) {
	got := ResolveCovenantPath("", "en")
	if got != "" {
		t.Errorf("ResolveCovenantPath empty recipeDir = %q, want empty", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd tui && go test ./internal/preset/ -run TestResolveCovenantPath -v`
Expected: FAIL — `ResolveCovenantPath` undefined

- [ ] **Step 3: Implement `ResolveCovenantPath`**

Add to `tui/internal/preset/recipes.go`, after `ResolveCommentPath` (around line 82):

```go
// ResolveCovenantPath returns the absolute path to the covenant file for a
// recipe directory and language, using the same fallback rule as ResolveGreetPath.
// Returns empty string if the recipe does not provide a covenant override.
func ResolveCovenantPath(recipeDir, lang string) string {
	return resolveRecipeFile(recipeDir, lang, "covenant.md")
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd tui && go test ./internal/preset/ -run TestResolveCovenantPath -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add tui/internal/preset/recipes.go tui/internal/preset/recipes_test.go
git commit -m "feat(preset): add ResolveCovenantPath for recipe covenant override"
```

---

### Task 3: Wire covenant override into recipe application

**Files:**
- Modify: `tui/internal/tui/recipe_save.go:43-53`
- Modify: `tui/internal/tui/firstrun.go:3073-3076`

- [ ] **Step 1: Add `resolveRecipeCovenant` helper**

Add to `tui/internal/tui/recipe_save.go`, after `resolveRecipeComment` (around line 53):

```go
// resolveRecipeCovenant returns the covenant.md path for a recipe, if the
// recipe provides one. Returns empty string if the recipe does not override
// the system-wide covenant.
func resolveRecipeCovenant(globalDir, recipeName, customDir, lang string) string {
	var recipeDir string
	if recipeName == preset.RecipeCustom || recipeName == preset.RecipeImported {
		recipeDir = customDir
	} else {
		recipeDir = preset.RecipeDir(globalDir, recipeName)
	}
	return preset.ResolveCovenantPath(recipeDir, lang)
}
```

- [ ] **Step 2: Wire covenant override in `firstrun.go`**

In `tui/internal/tui/firstrun.go`, at the block around line 3073-3076, change:

```go
	// Resolve comment path from recipe
	commentPath := resolveRecipeComment(m.globalDir, recipeName, customDir, lang)
	opts := m.pendingAgentOpts
	opts.CommentFile = commentPath
```

To:

```go
	// Resolve comment and covenant paths from recipe
	commentPath := resolveRecipeComment(m.globalDir, recipeName, customDir, lang)
	covenantPath := resolveRecipeCovenant(m.globalDir, recipeName, customDir, lang)
	opts := m.pendingAgentOpts
	opts.CommentFile = commentPath
	if covenantPath != "" {
		opts.CovenantFile = covenantPath
	}
```

The `if covenantPath != ""` guard is important: only override the covenant if the recipe actually provides one. If empty, `GenerateInitJSONWithOpts` falls back to the system-wide covenant as before.

- [ ] **Step 3: Check for other `GenerateInitJSON` call sites that need the same treatment**

There is one more call site in `tui/internal/tui/app.go:295`:

```go
if err := preset.GenerateInitJSON(p, agentName, agentName, a.projectDir, a.globalDir); err != nil {
```

This uses `GenerateInitJSON` (no opts), which creates default opts with empty `CovenantFile`. This is the molt/restart path — it does NOT go through recipe resolution. Check what recipe state is available at this call site. Read the surrounding code to determine if the covenant override should also apply here.

In `tui/internal/tui/app.go`, around line 295, read lines 280-310 to understand the context. If this path has access to the recipe name and can resolve the covenant, add the override. If not, this is a known gap to document.

Read the code at that location. If the recipe state is available (via `LoadRecipeState`), resolve the covenant and set `opts.CovenantFile` before calling. Change `GenerateInitJSON` to `GenerateInitJSONWithOpts` with the resolved opts.

The code should look like:

```go
	opts := preset.DefaultAgentOpts()
	// Resolve recipe covenant override if applicable
	recipeState, _ := preset.LoadRecipeState(a.projectDir)
	if recipeState.Recipe != "" {
		lang := a.tuiConfig.Language
		if lang == "" {
			lang = "en"
		}
		covenantPath := resolveRecipeCovenant(a.globalDir, recipeState.Recipe, recipeState.CustomDir, lang)
		if covenantPath != "" {
			opts.CovenantFile = covenantPath
		}
		commentPath := resolveRecipeComment(a.globalDir, recipeState.Recipe, recipeState.CustomDir, lang)
		if commentPath != "" {
			opts.CommentFile = commentPath
		}
	}
	if err := preset.GenerateInitJSONWithOpts(p, agentName, agentName, a.projectDir, a.globalDir, opts); err != nil {
```

- [ ] **Step 4: Build to verify compilation**

Run: `cd tui && make build`
Expected: Builds successfully

- [ ] **Step 5: Run all tests**

Run: `cd tui && go test ./... 2>&1 | grep -E "FAIL|ok"`
Expected: All `ok` (the pre-existing preset test failure is unrelated)

- [ ] **Step 6: Commit**

```bash
git add tui/internal/tui/recipe_save.go tui/internal/tui/firstrun.go tui/internal/tui/app.go
git commit -m "feat(tui): wire recipe covenant override into init.json generation"
```
