---
name: preset-skill-op-saved-presets
description: How saved presets differ from templates, and the Load/Save/Delete/Bootstrap contract — order, atomicity, and naming.
version: 2.0.0
last_changed_at: "2026-09-30T00:00:00Z"
related_files:
  - tui/internal/preset/preset.go
  - tui/internal/preset/preset_skill_router_test.go
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# Saved presets

Evidence: `tui/internal/preset/preset.go` — `List`, `Load`, `Validate`,
`Save`, `Delete`, `RefreshTemplates`, `BuiltinPresets`, `AutoSavedName`.

## Templates vs saved — the directory is the marker

`~/.lingtai-tui/presets/templates/` holds the 4 TUI-owned built-ins (one per
provider family: codex, claude, openai, anthropic);
`~/.lingtai-tui/presets/saved/` holds user-owned clones. There is no in-band
`"is_template"` field — `Source` (`SourceTemplate` / `SourceSaved`) is set
purely from which directory a file was loaded from, and is never
serialized. Two presets with byte-identical JSON but different directories
are treated differently.

## Load order — saved wins

`Load(name)` looks in `saved/` first, then `templates/`. A saved preset with
the same name as a template wins — the user's variant overrides. `List()`
returns saved presets first (alphabetical), then the templates. Treat the
templates' exact relative order as an implementation detail for anything
beyond the first-run default (codex is listed first).

**A saved preset falls back to the template only when the saved file is
genuinely absent; a malformed saved override blocks fallback.** `Load`
distinguishes "file doesn't exist" (`errors.Is(err, fs.ErrNotExist)`, falls
through to the next directory) from a real failure — invalid JSON,
permission error, or a path that is a directory — which is surfaced to the
caller with the underlying cause preserved rather than collapsed into a
generic not-found. Only when **both** the saved and template files are
absent does `Load` return the not-found error.

## Structural `Validate()` is not auto-invoked by Save or Load

`Validate()` mirrors the kernel's own load-time
validation gauntlet (`description.summary` non-empty; a **non-empty** `tier`
must be one of 1..5, while an empty tier is allowed;
`llm.provider` non-empty; `llm.model` non-empty except for `claude-code`,
which carries no model; `context_limit` shape; `base_url` is optional for
every family). It exists so
callers — notably the preset editor's commit path — can refuse to save
something the kernel would refuse to load. **Neither `Save` nor `Load` calls
it automatically**; a caller that writes a preset without checking
`Validate()` first can write a structurally invalid file. The editor calls
it explicitly (see `reference/operations/availability-save-gate/SKILL.md`).

## Save — always to `saved/`, same-name overwrites, no atomicity claim

`Save(p)` writes indented JSON **only** to the
`saved/` directory — never to `templates/`; that directory is owned
exclusively by `RefreshTemplates`/Bootstrap. A save under a name that
already exists in `saved/` **overwrites it** (`os.WriteFile`, not
create-exclusive). There is no atomicity claim for this write — it is a
direct, non-atomic `os.WriteFile`, not a temp-file-plus-rename.

## `<template>-N` naming for edited templates

`AutoSavedName(template, existing)` picks
`<template>-<N>` for the lowest unused positive `N`, so editing and saving a
built-in template never overwrites the template itself — it always
branches off a fresh saved copy the user owns.

## Delete — saved-only, missing is success, but requires exact authority

`Delete(name)` removes a file from `saved/` only —
templates are immutable from the user's perspective, and Bootstrap
re-extracts them on the next launch regardless. Deleting a name that
doesn't exist in `saved/` returns `nil` (success), not an error. This
success-on-missing behavior does not lower the bar for *when* a delete is
proposed: deleting a user's saved preset is still a destructive action that
requires exact authority for that specific preset before it is attempted.

## Bootstrap / `RefreshTemplates` — rewrites and prunes templates only

`RefreshTemplates()` rewrites every file in `templates/` from
`BuiltinPresets()` on every TUI launch, and prunes any `templates/*.json`
file no longer present in `BuiltinPresets()` — so a TUI upgrade that retires
a template (for example the former per-vendor templates) propagates
cleanly. It **never touches
`saved/`**. Like `Save`, there is no atomicity claim for these per-file
writes.

## Operations

For why the preset editor's Save is structural-only and never makes a
live provider/model call, see
`reference/operations/availability-save-gate/SKILL.md`.
For how a saved preset becomes the running default and what `/refresh`
switches, see `reference/operations/activation-session-refresh/SKILL.md`.
