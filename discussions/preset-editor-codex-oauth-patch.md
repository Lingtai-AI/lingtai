# Preset Editor — Codex OAuth Gate (Patch Spec)

**Date:** 2026-05-03
**Repo:** `lingtai` (TUI). All paths are relative to repo root unless absolute.

## Problem

When the user opens the standalone preset editor on a `provider=codex` preset and activates the `API 钥` row, the editor opens an inline text-input — but Codex doesn't take an API key, it takes an OAuth token bundle written to `~/.lingtai-tui/codex-auth.json` by `startOAuthFlow()`. The user is left typing into a field that the kernel will silently ignore (the `_codex` factory in `lingtai-kernel/src/lingtai/llm/_register.py` discards `api_key` and reads from `CodexTokenManager` instead).

The OAuth gate exists in two of the three editor entry points:

| Surface | OAuth on `feAPIKey`? | File |
|---|---|---|
| Login (`/login` view) | yes | `tui/internal/tui/login.go:312-318` |
| Firstrun wizard | yes | `tui/internal/tui/firstrun.go:1170-1188` |
| **Standalone preset editor** | **no — bug** | `tui/internal/tui/preset_editor.go:517-525` |

The screenshot referenced in the originating discussion was the standalone editor surface, reached from `/preset` → 编辑预设 → `codex`. The blinking cursor on the API-key row is visible there; OAuth is not triggered.

## Root cause

`PresetEditorModel.openInline()` in `preset_editor.go:509-559` switches on the focused field. The `feAPIKey` branch (517-525) opens the inline textarea unconditionally:

```go
case feAPIKey:
    m.input.SetValue("")
    m.input.CursorEnd()
    m.input.Focus()
    m.mode = emInline
```

There is no `if provider == "codex"` short-circuit to launch OAuth.

Additionally, the editor model has no `globalDir` field — it doesn't know where to write `codex-auth.json` when OAuth completes. The `LoginModel` carries `globalDir string` (login.go:58, 88, 91, 238) and reuses the same path the kernel reads from (`filepath.Join(m.globalDir, "codex-auth.json")` at login.go:238). The fix needs to plumb that through.

## Fix outline

Three edits in `preset_editor.go`, plus minimal updates to the three host call sites (firstrun ×2, preset_library ×1) to pass `globalDir` through.

### 1. Add `globalDir` field to `PresetEditorModel`

After line 269 (`savedCursor int`), insert:

```go
    // globalDir is ~/.lingtai-tui — the directory codex-auth.json lives
    // in. Passed by hosts so the editor can write the OAuth token bundle
    // when the user authenticates a codex preset's API-key row. May be
    // empty when no global dir is available (tests); in that case the
    // codex-OAuth branch falls back to inline edit.
    globalDir string
```

### 2. Update constructors to accept `globalDir`

Change `NewPresetEditorModel` (line 292) signature:

```go
func NewPresetEditorModel(p preset.Preset, lang string, existingKeys map[string]string, globalDir string) PresetEditorModel {
    return NewPresetEditorModelWithBuiltinFlag(p, lang, existingKeys, globalDir, preset.IsBuiltin(p.Name))
}
```

And `NewPresetEditorModelWithBuiltinFlag` (line 299):

```go
func NewPresetEditorModelWithBuiltinFlag(p preset.Preset, lang string, existingKeys map[string]string, globalDir string, isBuiltin bool) PresetEditorModel {
```

In the returned struct literal (line 328-340), add:

```go
    globalDir: globalDir,
```

(after the existing `apiKey: apiKey,` line)

### 3. Codex branch in `openInline`

Replace the existing `feAPIKey` case (lines 517-525) with:

```go
case feAPIKey:
    // Codex preset → OAuth flow, not text entry. The kernel's _codex
    // adapter factory ignores api_key entirely and reads tokens from
    // ~/.lingtai-tui/codex-auth.json via CodexTokenManager. Typing a
    // string here would be silently discarded.
    if asString(m.llmMap()["provider"]) == "codex" && m.globalDir != "" {
        ch := startOAuthFlow()
        return *m, func() tea.Msg { return <-ch }
    }
    // Edit the live key buffer, not the env-var-name. We start
    // blank rather than prefilling the existing value so the user
    // can paste a new key without first deleting the masked
    // placeholder. apiKeySet flips on commit if they typed anything.
    m.input.SetValue("")
    m.input.CursorEnd()
    m.input.Focus()
    m.mode = emInline
```

### 4. `CodexOAuthDoneMsg` handler in `Update`

Add a new case to `PresetEditorModel.Update` (line 345). Insert after the `tea.WindowSizeMsg` case and before `tea.KeyMsg`:

```go
    case CodexOAuthDoneMsg:
        if msg.Err != nil {
            m.saveErr = "OAuth error: " + msg.Err.Error()
            return m, nil
        }
        if msg.Tokens == nil {
            m.saveErr = "OAuth returned no tokens"
            return m, nil
        }
        // Persist tokens to ~/.lingtai-tui/codex-auth.json — same path
        // CodexTokenManager (kernel) reads from. Identical to login.go.
        data, err := json.MarshalIndent(msg.Tokens, "", "  ")
        if err != nil {
            m.saveErr = "failed to marshal tokens: " + err.Error()
            return m, nil
        }
        authPath := filepath.Join(m.globalDir, "codex-auth.json")
        if err := os.WriteFile(authPath, data, 0o600); err != nil {
            m.saveErr = "failed to save codex-auth.json: " + err.Error()
            return m, nil
        }
        // Display masked token in the row. apiKeySet stays false —
        // the host's commit path doesn't need to write this token to
        // .env (it lives in codex-auth.json instead).
        m.apiKey = msg.Tokens.AccessToken
        return m, nil
```

This requires two new imports at the top of the file (line 3-9):

```go
    "os"
    "path/filepath"
```

### 5. Update three host call sites

**`tui/internal/tui/firstrun.go:978`:**

```go
m.presetEditor = NewPresetEditorModel(m.presets[m.cursor], i18n.Lang(), m.existingKeys, m.globalDir)
```

(The `FirstRunModel` already has `globalDir` from `NewFirstRunModel(projectDir, globalDir, hasPresets, "")` per `app.go:134`. Verify the field exists on the firstrun model — likely as `m.globalDir`.)

**`tui/internal/tui/firstrun.go:992`:** same change.

**`tui/internal/tui/preset_library.go:286`:**

```go
m.editor = NewPresetEditorModel(m.presets[m.cursor], m.lang, keys, m.globalDir)
```

The `PresetLibraryModel` may not currently carry `globalDir`. If it doesn't, follow the same plumbing pattern as `MailModel` (app.go:170): add a `globalDir` field, update the constructor, update the call site in `app.go` that creates it. Look for `NewPresetLibrary` or similar — read `app.go` and `preset_library.go` to map this. The mechanical steps are:
  1. Add `globalDir string` to `PresetLibraryModel` struct.
  2. Add `globalDir` to its constructor's signature and store it.
  3. Update the call site in `app.go` to pass `a.globalDir`.

If multiple commits feel cleaner, do the plumbing in a separate commit before the OAuth fix. Single commit is also fine — the diff is small.

### 6. i18n: optionally add a "codex OAuth" placeholder

The masked api-key row currently shows `(not set — paste here)` when empty. For codex, consider adding a distinct placeholder like `(press Enter to login via OpenAI)`. But this is a polish item; leaving the existing key works because activating the row immediately starts OAuth.

If you do add it, the lookup point is `maskAPIKey` (preset_editor.go:1620) — wrap the call in `fieldString` (line 1037-1040) to switch on provider:

```go
case feAPIKey:
    if asString(llm["provider"]) == "codex" {
        if m.apiKey == "" {
            return i18n.T("preset_editor.api_key_codex_oauth_prompt")
        }
        return "OAuth — " + maskAPIKey(m.apiKey)
    }
    return maskAPIKey(m.apiKey)
```

Add three i18n keys:
- `en.json`: `"preset_editor.api_key_codex_oauth_prompt": "(press Enter to login via OpenAI)"`
- `zh.json`: `"preset_editor.api_key_codex_oauth_prompt": "（按 Enter 通过 OpenAI 登录）"`
- `wen.json`: `"preset_editor.api_key_codex_oauth_prompt": "（按回车以 OpenAI 登）"`

Mark this section optional. Do it if it falls naturally; skip if any plumbing problem comes up.

## Why this design

- **OAuth as Cmd, not state-flip.** `startOAuthFlow()` returns `<-chan CodexOAuthDoneMsg`; the standard pattern in this codebase (login.go:314-317, firstrun.go:1170) is to return a `tea.Cmd` that reads the channel. We can't substitute it inline for the textarea-based edit; the codex branch must return a Cmd from `openInline`. The function signature already accommodates that — it returns `(PresetEditorModel, tea.Cmd)`.
- **Reuse `login.go`'s persistence recipe verbatim.** Same path (`globalDir/codex-auth.json`), same JSON-marshal-indent shape, same `0o600` mode. Token files written by either surface are interoperable, and the kernel's `CodexTokenManager` doesn't care which surface produced them.
- **`apiKeySet` stays false.** Codex tokens don't go into `Config.Keys` / `.env` like other providers' API keys — they go into `codex-auth.json`. So the editor should not flip `apiKeySet=true` and trigger the host's `.env` write path on commit.
- **`globalDir==""` fallback.** Tests construct `PresetEditorModel` with `existingKeys=nil` and may not have a global dir. The codex branch checks `m.globalDir != ""` so test fixtures that hit the row degrade gracefully to inline edit instead of crashing on a nil filepath join.

## Verification

1. **Build + vet:**
   ```bash
   cd ~/Documents/GitHub/lingtai/tui && go vet ./... && make build
   ```
2. **Unit tests:** the editor has table tests at `tui/internal/tui/preset_editor_test.go`. Run them — none of them should currently exercise the codex-OAuth branch, but make sure existing assertions still pass after the constructor signature change. Update test fixtures if they call `NewPresetEditorModel` directly to pass an empty string for the new `globalDir` arg.
   ```bash
   cd ~/Documents/GitHub/lingtai/tui && go test ./...
   ```
3. **Manual:** launch the TUI, navigate to `/preset` → 编辑预设 → codex preset → focus the `API 钥` row → press Enter. A browser tab should open at `https://auth.openai.com/oauth/authorize?...`. After completing OAuth, the row should display `OAuth — <email>` (or masked token) and `~/.lingtai-tui/codex-auth.json` should exist with mode 0600. Restart Cohen (or any codex-provider agent); kernel calls should succeed.

## Lines of change estimate

- **Adds:** ~25 (codex branch in openInline, OAuth handler in Update, struct field, struct literal entry)
- **Edits:** 4 lines (3 constructor signatures + 1 import block)
- **Touched files:** 4 (`preset_editor.go`, `firstrun.go`, `preset_library.go`, possibly `app.go` if `PresetLibraryModel` needs new plumbing)
- **No deletes.**

## Out of scope

- **Token refresh handling on the editor side.** Already handled in the kernel by `CodexTokenManager` per call. The editor doesn't need a refresh path.
- **Other OAuth-gated providers.** None exist today. If/when another OAuth provider is added (e.g., Anthropic Claude with browser-login), generalize the gate then — premature to abstract on a single provider.
- **Logout / disconnect from editor.** The user can already do this from `/login`. Don't duplicate.
- **Visual indication that OAuth is in flight.** The login view sets `m.codexLogging = true` to show a spinner. The editor's modes (`emInline` / `emBrowse` / etc.) don't currently have a "waiting for OAuth" mode. The OAuth wait is short (< 2 minutes typical, 5-minute timeout) — leaving the row showing the previous masked value during the wait is acceptable. If desired, add a new `emCodexOAuth` mode in a follow-up; not required for the bug fix.
