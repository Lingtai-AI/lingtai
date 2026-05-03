# Firstrun Step 2 — Include Built-in Default Preset

**Date:** 2026-05-03
**Repo:** `lingtai` (TUI). All paths relative to repo root.

## Problem

Firstrun wizard's Step 2/4 (`stepAgentPresets`, "配本我之预" — pick default + toggle allowed) hides built-in templates by design. When the user picks `codex` (or any other built-in, e.g. `mimo-pro`, `deepseek_flash`) as the default in Step 1/4, Step 2 shows an empty or codex-less list.

Concretely:

- Step 1 lists built-in templates **and** saved presets — codex appears under `已存预设` (and again under `新建预设`).
- User selects `codex` row → presses Next → Step 2 builds its list at firstrun.go:3050-3053 with `if !preset.IsBuiltin(p.Name)` — `codex` is filtered out.
- Result: codex is the chosen default but doesn't appear among allowed candidates. The init.json that gets written has `manifest.preset.default = "...codex.json"` but `default ∉ allowed` — invalid by `init_schema.validate_init` (kernel asserts `default` must be in `allowed`).
- User experience: blank or short list with their chosen preset missing entirely.

The "saved-only" filter is intentional — it encourages the "fork before allow" lifecycle. But Step 1 doesn't enforce that; it lets users pick a built-in as default without forcing a clone first. The two screens disagree about whether built-ins are first-class.

## Root cause

`enterAgentPresets()` in `firstrun.go:3044-3122` builds `m.savedPresetIdx`:

```go
m.savedPresetIdx = m.savedPresetIdx[:0]
for i, p := range m.presets {
    if !preset.IsBuiltin(p.Name) {
        m.savedPresetIdx = append(m.savedPresetIdx, i)
    }
}
```

If the user's default (`m.cursor`) points to a built-in, that index never enters `savedPresetIdx`, so `presetDefaultIdx` lookup at line 3063-3070 finds no match and stays at 0 (or, if the saved list is empty, becomes invalid).

## Fix — Option (a): Auto-include the default's preset

Smallest, lowest-risk fix. Whenever `m.cursor` points to a built-in, append that one built-in to `savedPresetIdx` so it appears as a row in Step 2. Mark it allowed and default. Built-ins that aren't the chosen default stay hidden (preserves the "saved-only" curation).

The schema invariant (`default ∈ allowed`) is preserved automatically because the built-in default row gets `presetAllowed[r] = true` like any other default.

### Patch 1: `firstrun.go` — auto-include built-in default

In `enterAgentPresets()` (lines 3048-3054), change:

```go
// Build the row list: indices of saved presets within m.presets.
m.savedPresetIdx = m.savedPresetIdx[:0]
for i, p := range m.presets {
    if !preset.IsBuiltin(p.Name) {
        m.savedPresetIdx = append(m.savedPresetIdx, i)
    }
}
```

to:

```go
// Build the row list: indices of saved presets within m.presets.
// Built-in templates are normally hidden — Step 2 is for the
// "saved-only" curation surface. The exception: if the user chose a
// built-in as their default in Step 1, we MUST include it, otherwise
// Step 2 would render with the chosen default missing and the kernel
// would reject the resulting init.json (default must be in allowed).
m.savedPresetIdx = m.savedPresetIdx[:0]
for i, p := range m.presets {
    if !preset.IsBuiltin(p.Name) {
        m.savedPresetIdx = append(m.savedPresetIdx, i)
    } else if i == m.cursor {
        // The built-in chosen as default in Step 1 — surface it so
        // the user can see it and the schema invariant holds.
        m.savedPresetIdx = append(m.savedPresetIdx, i)
    }
}
```

(One added `else if`. Order is preserved because we append in the same `i` loop.)

### Patch 2: schema invariant — built-in default cannot be unallowed

The existing block at lines 1074-1078 already prevents un-allowing the row that is currently the default. That gate works regardless of whether the row is a saved preset or built-in, so no change is needed there.

But: there is a different risk worth covering. If the user *changes* the default from a built-in row to another row in Step 2, then un-allows the original built-in, that row should disappear from `savedPresetIdx` on the next `enterAgentPresets` call. Since `savedPresetIdx` is rebuilt fresh each entry (line 3049 `[:0]`), this is already correct — the built-in only appears when it *is* the active default.

Actually wait: after Step 2 entry, the user may re-pick a different default during Step 2 itself (without going back to Step 1). Look at firstrun.go:1100-1108 to confirm whether that's possible.

(Read: line 1100 toggles allowed, line 1106 sets default to current cursor. So yes, user can re-pick default during Step 2 — but the row list is already drawn for this entry, the built-in stays visible until the next entry. Acceptable: the built-in just sits there unallowed and not-default. Schema invariant only requires `default ∈ allowed`, not "default is the only built-in.")

This is fine as-is; no extra patch needed for that case.

### Patch 3: visual hint that this row is a built-in (optional)

The row would benefit from a small marker so the user knows this preset will be used "as-is" without a saved copy. The existing rendering at lines 2122-2153 doesn't distinguish built-in rows from saved rows.

Add (around line 2147, in the existing `nameStyle` block):

```go
displayName := i18n.T("preset.name_" + p.Name)
if displayName == "preset.name_"+p.Name {
    displayName = p.Name
}
// Mark built-in rows so the user knows this is a template, not a
// user-saved preset. Templates render styled the same as saved rows
// for the active default, but get a small "（模板）" suffix so it's
// clear it's the built-in path.
if preset.IsBuiltin(p.Name) {
    displayName += " " + StyleFaint.Render(i18n.T("firstrun.preset_cfg.builtin_marker"))
}
```

i18n entries needed (all three locales):

- `en.json`: `"firstrun.preset_cfg.builtin_marker": "(template)"`
- `zh.json`: `"firstrun.preset_cfg.builtin_marker": "（模板）"`
- `wen.json`: `"firstrun.preset_cfg.builtin_marker": "（模板）"`

Mark this section optional; do it if it falls naturally.

## Why this design

- **Doesn't disturb the saved-only curation principle.** Other built-ins remain filtered out — the user has to fork them before they can swap to them at runtime. Only the *currently-active default* gets the implicit pass, and only because the schema *requires* the default to be allowed.
- **No file-system writes triggered.** Option (b) — auto-fork the chosen built-in into a saved copy — would write to `~/.lingtai-tui/presets/saved/` without the user asking, polluting the saved directory with a copy they didn't intend to keep. Option (a) keeps Step 2 purely as a UI fix.
- **Idempotent across firstrun re-entries.** Because `savedPresetIdx` is rebuilt fresh each `enterAgentPresets` call (line 3049 `[:0]`), the built-in shows up only when it's currently the chosen default. If the user changes default to a saved preset on a subsequent run, the built-in vanishes again.
- **Stable cursor / focus.** The auto-include is appended in the same loop iteration order, so `m.presetDefaultIdx` lookup at lines 3063-3070 finds the row at the correct position.

## Verification

1. **Build + vet:**
   ```bash
   cd ~/Documents/GitHub/lingtai/tui && go vet ./... && make build
   ```
2. **Tests:**
   ```bash
   cd ~/Documents/GitHub/lingtai/tui && go test ./...
   ```
3. **Manual:** launch the rebuilt TUI, fresh project (or `/setup`):
   - Step 1: cursor on `codex` (built-in) → press Next.
   - Step 2: confirm `codex` row appears with `[*]` marker (default + allowed).
   - Press Space on it: confirm it stays allowed (line 1074-1078 gate).
   - Save through to init.json: confirm `manifest.preset.{default, allowed}` both contain the codex path.
   - Repeat with another built-in (e.g. `mimo-pro`) to confirm the fix is generic.

## Lines of change estimate

- **Adds:** ~4 (one `else if` clause + comment)
- **Edits:** 0
- **Touched files:** 1 (`firstrun.go`); +3 if i18n marker is included
- **No deletes.**

## Out of scope

- **Auto-fork on Step 1 default selection.** That's option (b) from the design discussion. Cleaner long-term but writes to the user's saved/ directory without explicit consent. Defer.
- **Step 1 mismatch with Step 2.** Step 1 lists everything (templates + saved) but Step 2 only saved. The mismatch is intentional ("Step 2 is curation"). This patch shrinks the practical impact of the mismatch but doesn't eliminate it; that would require a Step 1 redesign.
- **Recipe-imported presets.** Recipe imports already land as saved presets (Source == SourceSaved), so `IsBuiltin` returns false and they appear in Step 2 normally. No change needed.
