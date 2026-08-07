---
related_files:
  - tui/ANATOMY.md
  - tui/internal/tui/ANATOMY.md
  - tui/internal/preset/ANATOMY.md
  - tui/i18n/i18n.go
  - tui/i18n/i18n_test.go
  - tui/i18n/en.json
  - tui/i18n/zh.json
  - tui/i18n/wen.json
  - portal/ANATOMY.md
  - docs/i18n-vocab.md
maintenance: |
  Keep related_files as repo-relative paths to real files. Include neighboring
  ANATOMY.md files so the anatomy graph stays connected rather than isolated;
  anatomy links must be bidirectional. If you create a new ANATOMY.md, copy this
  maintenance field. If you notice drift between this anatomy and the code,
  report it. See lingtai-dev-guide for details.
---

# i18n (TUI)

> **Maintenance:** see the `lingtai-tui-anatomy` skill at `tui/internal/preset/skills/lingtai-tui-anatomy/SKILL.md`. Coding agents update this file in the same commit as code changes.

`tui/i18n` is the TUI binary's localization layer: three embedded flat key→string JSON tables (`en`, `zh`, `wen` — Classical Chinese) plus a tiny lookup API every Bubble Tea screen calls. It is deliberately dependency-free and embedded (`//go:embed`), so a single `lingtai-tui` binary ships every locale with no runtime file lookup. `portal/i18n/` is its independent counterpart in the portal binary: same three-locale rule, separate tables, no shared code.

## Components

| Symbol | Citation | Purpose |
|---|---|---|
| `localeFS` | `tui/i18n/i18n.go:10-11` | `//go:embed en.json zh.json wen.json` — the compiled-in locale tables |
| `supportedLocales` | `tui/i18n/i18n.go:23` | the closed set of accepted locale codes; anything else is rejected by `SetLang` |
| `init` | `tui/i18n/i18n.go:29` | establishes the default locale so `T` is safe before any explicit `SetLang` |
| `SetLang(l)` | `tui/i18n/i18n.go:39` | switches the process locale, returning an error for an unsupported code |
| `cachedLocale` / `load` | `tui/i18n/i18n.go:51,75` | parse-once-per-locale table loading from the embedded FS |
| `T(key)` | `tui/i18n/i18n.go:90` | the main lookup — active locale first, then the English table, then the raw key itself |
| `TF(key, args...)` | `tui/i18n/i18n.go:102` | `T` plus `fmt.Sprintf` formatting for parameterized strings |
| `TIn(locale, key)` | `tui/i18n/i18n.go:108` | explicit-locale lookup for rendering text in a locale other than the current one (e.g. seeding an agent's own language) |
| `Lang()` | `tui/i18n/i18n.go:122` | the currently selected locale code |
| `en.json` / `zh.json` / `wen.json` | `tui/i18n/en.json`, `tui/i18n/zh.json`, `tui/i18n/wen.json` | the three flat key→string tables; keys must be identical across all three |

## Connections

- **Called by every screen in `tui/internal/tui/`** and by user-facing strings in `tui/main.go`; `tui/internal/preset/` uses `TIn` when writing locale-specific preset/recipe text.
- **Not shared with the portal.** `portal/i18n/` is a separate package with its own tables — an added key that appears on both surfaces must be added in both places.
- **Vocabulary consistency** across locales is tracked in `docs/i18n-vocab.md`.
- **Runs before the TUI renders in some paths.** `tui/internal/globalmigrate/` deliberately prints with `fmt` rather than `T` because it executes before a locale is selected.

## Composition

- **Parent:** `tui/` (`tui/ANATOMY.md`)
- **Subfolders:** none — one Go file plus three JSON tables
- **Counterpart:** `portal/i18n/` (see `portal/ANATOMY.md`)

## State

- **Reads:** only the embedded tables. No file I/O at runtime, no locale files on disk, no user-supplied translations.
- **Process-global:** the selected locale is process state set once at startup from the global TUI config.

## Notes

- **Three-locale rule.** Adding a key means adding it to `en.json`, `zh.json`, AND `wen.json`. `TestLocaleJSONKeySetsAreComplete` (`tui/i18n/i18n_test.go:313`) fails when the three key sets diverge, so a half-added key is a build failure rather than a silent English string in a Chinese screen. At runtime the fallback is English-then-raw-key (`TestT_FallsBackToEnglishWhenActiveLocaleMissesKey`, `TestT_MissingEverywhereReturnsKey`) — visible, not correct.
- **Procedural / dev-only strings may stay English-only** when they are not user-facing, with a comment saying why — do not add a half-translated key to only one table.
- **`wen` is Classical Chinese**, not a variant of `zh`. Translate it deliberately rather than copying the `zh` string.
