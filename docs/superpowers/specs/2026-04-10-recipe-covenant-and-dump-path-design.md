# Recipe Covenant Override + Dump Path Rename

**Date:** 2026-04-10
**Status:** Draft

## Problem

1. The hourly markdown dump path uses `~/.lingtai-tui/brief/<hash>/history/` but the correct layout is `~/.lingtai-tui/brief/projects/<hash>/history/` to accommodate the universal `profile.md` at `~/.lingtai-tui/brief/profile.md`.

2. Recipes currently provide `greet.md` and `comment.md` per language, but cannot override the system-wide covenant. The secretary agent (and potentially other utility agents) needs a simplified covenant with no avatar spawning or network participation.

## Solution

### 1. Dump Path Rename

Change `briefHistoryDir()` in `tui/internal/fs/session_dump.go` to return `<base>/brief/projects/<hash>/history/` instead of `<base>/brief/<hash>/history/`.

This is safe — the feature was just introduced and has not been released.

### 2. Recipe Covenant Override

Add `ResolveCovenantPath(recipeDir, lang string) string` to `tui/internal/preset/recipes.go`, following the same pattern as `ResolveGreetPath` and `ResolveCommentPath`:

1. Check `<recipeDir>/<lang>/covenant.md`
2. Fallback to `<recipeDir>/covenant.md`
3. Return empty string if not found

In the init.json generation flow, after resolving the recipe, check if the recipe provides a covenant file. If so, use it as `opts.CovenantFile` instead of the system-wide one.

### Recipe Asset Structure (with optional covenant)

```
recipe_assets/<recipe>/
  recipe.json
  en/
    greet.md
    comment.md
    covenant.md    ← optional, overrides system covenant
  zh/
    greet.md
    comment.md
    covenant.md    ← optional
  wen/
    greet.md
    comment.md
    covenant.md    ← optional
```

When `covenant.md` is absent from the recipe (the common case), the system-wide covenant at `~/.lingtai-tui/covenant/<lang>/covenant.md` is used as before. No existing behavior changes.

## Files to Modify

| File | Change |
|------|--------|
| `tui/internal/fs/session_dump.go` | `briefHistoryDir()` — add `projects/` segment to path |
| `tui/internal/fs/session_dump_test.go` | Update `TestBriefHistoryDir` expected path |
| `tui/internal/preset/recipes.go` | Add `ResolveCovenantPath()` — same pattern as `ResolveGreetPath` |
| `tui/internal/preset/recipes_test.go` | Add test for `ResolveCovenantPath` |
| `tui/internal/preset/preset.go` | In the init.json generation flow, check recipe covenant and set `opts.CovenantFile` if found |

## Where the Covenant Override Hooks In

Currently in `preset.go:449-452`:

```go
covenantFile := opts.CovenantFile
if covenantFile == "" {
    covenantFile = CovenantPath(globalDir, lang)
}
```

The override needs to happen **before** `GenerateInitJSONWithOpts` is called — at the call sites where the recipe is known. The callers (in `app.go` or recipe setup flows) should resolve the recipe covenant path and set `opts.CovenantFile` if found. The existing fallback chain in `GenerateInitJSONWithOpts` then works unchanged:

1. `opts.CovenantFile` set by caller (recipe covenant) → used
2. `opts.CovenantFile` empty → falls back to system-wide covenant

## What is NOT in This PR

- Secretary recipe content (greet.md, covenant.md, skills)
- Secretary agent lifecycle
- `/secretary` TUI command
- `lingtai run --recipe` kernel command
- Profile.md / journal.md consumption

## Verification

1. `cd tui && make build`
2. Run TUI — verify hourly dumps go to `~/.lingtai-tui/brief/projects/<hash>/history/`
3. Create a test recipe with `en/covenant.md` — verify init.json uses the recipe covenant
4. Create a test recipe without covenant.md — verify init.json falls back to system covenant
5. All existing tests pass
