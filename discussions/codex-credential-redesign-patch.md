# Codex Credential Redesign — Patch Spec

**Date:** 2026-05-03
**Repo:** `lingtai` (TUI). All paths relative to repo root.

## Problem

Codex OAuth is currently treated as part of the preset's *contents*. That's the wrong mental model and has produced a sequence of bugs we've been bandaging:

- The wizard's `stepPresetKey` for codex hard-codes a clone to `codex_oauth`, overwriting any existing preset of that name and ignoring the user's intended preset name.
- Editor's clone-on-edit flow for codex creates collisions and orphans.
- Multiple `stepPresetKey`-style screens render the OAuth model picker, drifting out of sync with the editor's model row.
- The user is sent through a "log in" flow even when codex-auth.json is already valid.
- After a successful login, the editor and wizard each have their own (sometimes conflicting) handlers for `CodexOAuthDoneMsg`.

The fix is conceptually small: **codex OAuth is a credential, not a preset field.** It belongs in the same conceptual slot as `MINIMAX_API_KEY`, just acquired via browser instead of paste. The credential is per-machine (one OAuth, stored in `~/.lingtai-tui/codex-auth.json`); presets just *use* it.

## Final shape

- **One codex preset at a time.** Multiple-codex support deferred — keep the door open architecturally but enforce a single saved codex preset for now (the simpler invariant unblocks the redesign without us needing to design name-disambiguation UX).
- **OAuth managed only on Step 1 (the preset picker page).** A new status row at the top: `Codex OAuth: ✓ 已登 / ✗ 未登 [c to login]`. Pressing the keybind launches `startOAuthFlow()` and writes `codex-auth.json` on success.
- **`stepPresetKey` is dropped for codex entirely.** `presetNeedsKey(p)` already returns `false` for codex (api_key_env is empty), so this is mostly removing the codex-specific branches that fight against that.
- **Editor's API-key row becomes read-only for codex.** Renders `已登: <email>` or `未登 — 于上一页登入`; pressing Enter does nothing. No more in-editor OAuth.
- **Codex preset rows are gated when no valid auth.** Greyed out, can't be selected as default, can't enter editor.
- **Launch-time validation.** On TUI startup, validate codex-auth.json (file parses, has refresh_token). If invalid AND any agent's active preset is codex, surface a banner and log to agent's log.
- **Migration.** Stale `codex_oauth.json` saved presets from earlier iterations get cleaned up on first launch.

## Files to change

| File | Why |
|---|---|
| `tui/internal/tui/firstrun.go` | Add OAuth status block to Step 1; drop stepPresetKey codex branches; drop codex-specific clone path; gate codex rows |
| `tui/internal/tui/preset_editor.go` | Drop in-editor OAuth trigger; make API key row read-only for codex; drop CodexOAuthDoneMsg handler |
| `tui/internal/tui/preset_library.go` | Same OAuth block on the standalone `/preset` page (mirrors firstrun for a uniform UX) |
| `tui/internal/migrate/m0XX_*.go` | Migration to clean up `saved/codex_oauth.json` orphans |
| `tui/internal/preset/preset.go` | Helper `CountSavedByProvider(presets, "codex")` for the "only one codex" check |
| `tui/i18n/{en,zh,wen}.json` | New i18n keys for OAuth status, gated-row hint, "only one codex" error |

## Patch breakdown

### §1 — Codex OAuth status block on Step 1

Render at the top of `stepPickPreset` view (firstrun.go:2085-area, before the saved/templates list rendering). Above 已存预设, draw:

```
Codex OAuth:  ✓ 已登 (foo@bar.com)            [r] 注销
Codex OAuth:  ✗ 未登 — 必先登方可用 codex     [c] 登
```

State source: `os.ReadFile(globalDir/codex-auth.json)` parsed once on Step 1 entry into a struct field on `FirstRunModel`:

```go
codexAuth struct {
    valid bool
    email string  // "" if JWT didn't carry one but tokens are valid
}
```

Refresh this field whenever `CodexOAuthDoneMsg` lands or when the user presses `r` (logout).

Add keybinds in the `stepPickPreset` Update branch:

- `c` — if `!m.codexAuth.valid`, launch `startOAuthFlow()`, return cmd that awaits the channel. Show `m.codexLoggingIn = true` while pending.
- `r` — if `m.codexAuth.valid`, delete `codex-auth.json`, flip `m.codexAuth.valid = false`. (Confirm with a y/N prompt — destructive.)

Reuse the existing `CodexOAuthDoneMsg` handler at firstrun.go:733-area. Simplify it: just write tokens to disk and refresh `m.codexAuth`. Drop the auto-advance / clone logic — that's stepPresetKey-specific and stepPresetKey is going away for codex.

### §2 — Drop `stepPresetKey` for codex

`presetNeedsKey(p)` (firstrun.go:3624) already returns `false` for codex (api_key_env is `""`). The codex-specific branches in `stepPresetKey` exist only because past code routed there explicitly. Now that no path routes there for codex, remove the branches:

- firstrun.go: codex branch in `stepPresetKey` rendering (the `if m.selectedProvider == "codex"` block we recently simplified — delete the whole block).
- firstrun.go: codex branch in `enterPresetKeyFor` (the `if provider == "codex"` block — delete; the helper just sets up paste-key flow now).
- firstrun.go: `codexEmail`, `codexLoggingIn` fields — move to the new `codexAuth` struct on the model. The "logging in" pending state belongs to Step 1 now, not stepPresetKey.
- firstrun.go: `keyDoNext`'s codex branch (lines 1196-1230) — delete; codex never reaches keyDoNext.
- firstrun.go: `CodexOAuthDoneMsg` handler's auto-advance logic (the clone-and-enterCapabilities block we added in commit `c6b8f10`) — delete; OAuth on Step 1 doesn't advance steps, just refreshes status.

### §3 — Editor API-key row becomes read-only for codex

In `preset_editor.go`:

- `openInline()` `feAPIKey` branch: drop the `provider == "codex"` short-circuit (commit `485bc04`'s addition). Codex on this row falls through to nothing — Enter on the row no-ops or shows a faint hint `请于上一页登入`.
- `fieldString(feAPIKey)`: codex case becomes purely informational. Read `globalDir/codex-auth.json`, render `已登: <email>` (or `已登` if email is empty) or `未登 — 于首启页登入`. Don't store an API key in `m.apiKey`.
- `Update`'s `CodexOAuthDoneMsg` handler — delete. The editor no longer initiates OAuth, so this handler can never fire.
- `globalDir` field on `PresetEditorModel` — keep; used for the read-only display lookup.

The host's CodexOAuthDoneMsg forward we added in `ffdca4d` (firstrun.go:733-area `if m.step == stepEditPreset` forward) — delete; the editor doesn't need to receive these anymore.

### §4 — Codex preset row gating on Step 1

In `stepPickPreset` rendering, when iterating `m.presets`:

```go
isCodex := getPresetProvider(p) == "codex"
codexLocked := isCodex && !m.codexAuth.valid
if codexLocked {
    // Render greyed; show lock icon or hint at end of row.
    nameStyle = StyleFaint
    suffix = " " + StyleFaint.Render(i18n.T("firstrun.preset_pick.codex_locked"))
}
```

In the row's Enter handler, gate similarly: if `codexLocked` on the focused row, show `m.message = i18n.T("firstrun.preset_pick.codex_locked_hint")` and no-op instead of opening editor / advancing.

The `新建预设` codex row also gets gated — if user picks `codex` from templates with no auth, route them back to OAuth instead of editor.

### §5 — One-codex enforcement

Add a helper:

```go
// preset/preset.go
func CountSavedByProvider(presets []Preset, provider string) int {
    n := 0
    for _, p := range presets {
        if !IsTemplate(p) {
            if llm, _ := p.Manifest["llm"].(map[string]interface{}); llm != nil {
                if prov, _ := llm["provider"].(string); prov == provider {
                    n++
                }
            }
        }
    }
    return n
}
```

Gate clone-on-edit in `preset_library.go` and `firstrun.go`:

```go
if isCloneFromTemplate && getPresetProvider(p) == "codex" {
    if preset.CountSavedByProvider(allPresets, "codex") >= 1 {
        m.message = i18n.T("preset_editor.codex_already_exists")
        return ... // refuse the clone
    }
}
```

The user can still delete their existing codex preset and create a new one. They just can't have two simultaneously.

### §6 — Launch-time validation

In `tui/main.go` startup (or wherever `App` initializes), after agent discovery:

```go
codexValid := validateCodexAuth(globalDir)  // returns bool
for _, agent := range agents {
    activePreset := loadActivePresetForAgent(agent)
    if getPresetProvider(activePreset) == "codex" && !codexValid {
        // Log to that agent's logs/agent.log:
        appendToAgentLog(agent, "WARN: codex OAuth expired or missing — agent will fail on next LLM call. Run TUI and press [c] on the preset picker to re-login.")
        // Set a banner on the TUI's Mail screen too, if feasible.
    }
}
```

`validateCodexAuth`:

```go
func validateCodexAuth(globalDir string) bool {
    data, err := os.ReadFile(filepath.Join(globalDir, "codex-auth.json"))
    if err != nil {
        return false
    }
    var t CodexTokens
    if json.Unmarshal(data, &t) != nil {
        return false
    }
    return t.RefreshToken != ""
}
```

(Reuses the same predicate we already settled on in commit `c6b8f10`.)

### §7 — Migration: clean up codex_oauth orphans

Add `tui/internal/migrate/m033_cleanup_codex_oauth.go`:

```go
package migrate

import (
    "os"
    "path/filepath"
)

// m033 cleans up `saved/codex_oauth.json` orphans created by the buggy
// pre-2026-05-03 wizard flow that hard-coded the clone name to
// `codex_oauth`. Behavior:
//
//   - If both saved/codex.json and saved/codex_oauth.json exist: keep
//     codex_oauth (it's the user's edited copy) and rename it to codex,
//     overwriting the older codex.json. We assume the most recently
//     edited file is the one the user actually uses.
//   - If only saved/codex_oauth.json exists: rename to codex.json.
//   - If only saved/codex.json exists: no-op.
//
// Run once per project; tracked in .lingtai/meta.json.
func migrateCleanupCodexOauth(lingtaiDir string) error {
    globalDir, err := globalTUIDir()
    if err != nil {
        return nil // best-effort
    }
    savedDir := filepath.Join(globalDir, "presets", "saved")
    codex := filepath.Join(savedDir, "codex.json")
    oauth := filepath.Join(savedDir, "codex_oauth.json")
    if _, err := os.Stat(oauth); err != nil {
        return nil // nothing to migrate
    }
    // Pick the more recently edited as the survivor.
    var keep, drop string
    sCodex, errC := os.Stat(codex)
    sOauth, errO := os.Stat(oauth)
    switch {
    case errC != nil && errO == nil:
        keep, drop = oauth, ""
    case errO != nil:
        return nil
    default:
        if sOauth.ModTime().After(sCodex.ModTime()) {
            keep, drop = oauth, codex
        } else {
            keep, drop = codex, oauth
        }
    }
    if drop != "" {
        if err := os.Remove(drop); err != nil {
            return err
        }
    }
    if keep == oauth {
        if err := os.Rename(oauth, codex); err != nil {
            return err
        }
    }
    return nil
}
```

Register in `migrate.go` AND `portal/internal/migrate/migrate.go` (per CLAUDE.md's note about shared version space; the portal entry can be a no-op stub since this is TUI-only file movement).

Also clean up the agent's `init.json` if its `manifest.preset.{default,active,allowed}` references `presets/saved/codex_oauth.json` — rewrite to `presets/saved/codex.json`. This is per-project (the migration runs in a project context), unlike the global file rename which is one-shot.

### §8 — i18n

New keys, three locales:

| Key | en | zh | wen |
|---|---|---|---|
| `codex.oauth_status_label` | `Codex OAuth:` | `Codex OAuth：` | `Codex OAuth：` |
| `codex.oauth_logged_in` | `✓ Logged in (%s)` | `✓ 已登录（%s）` | `✓ 已登（%s）` |
| `codex.oauth_logged_in_no_email` | `✓ Logged in` | `✓ 已登录` | `✓ 已登` |
| `codex.oauth_not_logged_in` | `✗ Not logged in — required for codex presets` | `✗ 未登录 — 必先登方可用 codex` | `✗ 未登 — 必先登方可用 codex` |
| `codex.oauth_login_hint` | `[c] login` | `[c] 登录` | `[c] 登` |
| `codex.oauth_logout_hint` | `[r] logout` | `[r] 注销` | `[r] 注销` |
| `codex.oauth_logout_confirm` | `Log out of Codex? [y/N]` | `注销 Codex？[y/N]` | `注销 Codex 否？[y/N]` |
| `firstrun.preset_pick.codex_locked` | `(login required)` | `（须先登）` | `（须先登）` |
| `firstrun.preset_pick.codex_locked_hint` | `Press [c] above to log in to Codex first.` | `请按 [c] 先登入 Codex。` | `请按 [c] 先登入 Codex。` |
| `preset_editor.codex_already_exists` | `Only one codex preset allowed. Delete the existing one first.` | `只允许一个 codex 预设。请先删除已有的。` | `只许一 codex 预设。请先删之。` |
| `preset_editor.api_key_codex_readonly` | `OAuth credential — manage on the preset picker page.` | `OAuth 凭据 — 于预设选择页管理。` | `OAuth 凭据——于择预页管之。` |

Replace the existing `preset_editor.api_key_codex_oauth_prompt` keys (commit `5a86b89`) with `api_key_codex_readonly` since the field is no longer interactive.

## Verification

1. **Build + vet:**
   ```bash
   cd tui && go vet ./... && go test ./... && make build
   ```
2. **Migration:** create test project with both `saved/codex.json` and `saved/codex_oauth.json`. Run TUI. Confirm only `codex.json` remains, holding the more-recent file's contents. Project init.json's preset paths get rewritten.
3. **Unauthed flow:** `rm ~/.lingtai-tui/codex-auth.json`. Launch TUI. Step 1 shows `✗ 未登`. Pressing Enter on a codex preset row shows the lock hint, doesn't open editor. Press `[c]` → browser → complete OAuth → return → Step 1 now shows `✓ 已登`. Codex rows now interactive.
4. **Already-authed flow:** With valid `codex-auth.json`, launch TUI. Step 1 shows `✓ 已登`. Pressing Enter on codex row opens the editor directly (no OAuth detour). Editor's API-key row is greyed and shows `已登 — 于首启页登入`.
5. **One-codex enforcement:** with a saved `codex` preset, try to clone the codex template again from `/preset` library. Refused with `preset_editor.codex_already_exists` message.
6. **Logout:** with valid auth and saved codex preset, press `r` on Step 1, confirm. File deleted, codex rows lock again.
7. **Launch-time validation:** with an agent whose active preset is codex but `codex-auth.json` is invalid, launch TUI. Banner appears; agent's logs/agent.log gets the WARN line.

## Lines of change estimate

- **Adds:** ~200 (new Step 1 OAuth block, gating logic, migration, helper)
- **Edits:** ~100 (delete codex branches in stepPresetKey, simplify CodexOAuthDoneMsg handler, editor read-only path)
- **Deletes:** ~80 (codex-specific code paths that are no longer reachable)
- **Touched files:** ~7
- **i18n keys:** 11 new entries × 3 locales = 33

## Out of scope (defer)

- **Multiple codex presets.** Punted — single-codex invariant unblocks this redesign. Add later: per-preset `codex_account_id` field that points at one of N saved auth files (`codex-auth-N.json`), and a Step 1 picker for "which Codex account this preset uses." Not needed now.
- **Codex auto-refresh validation in launch path.** We only check token *presence*. Detecting an expired refresh_token requires hitting the refresh endpoint, which adds a network call to TUI startup. Skip — the kernel will fail loud on the first LLM call if the refresh is rejected, which is good enough.
- **Refactoring credentials more broadly.** Other providers (minimax, zhipu, etc.) still have their API keys baked into per-preset env-var-name fields. The "codex is a credential, not a preset field" pattern points at a future "credentials slot in the preset" abstraction, but porting all providers is a much bigger redesign. Save for later.

## Cross-references

- `~/.lingtai-tui/codex-auth.json` — the credential file (~/.lingtai-tui is `globalDir`)
- `lingtai-kernel/src/lingtai/auth/codex.py` — `CodexTokenManager` reads this file and refreshes access tokens as needed
- `lingtai-kernel/src/lingtai/llm/_register.py:_codex` — provider factory; ignores api_key entirely, uses `CodexTokenManager`
- `tui/internal/tui/oauth.go` — PKCE OAuth flow, browser launch, callback server
- `tui/internal/tui/login.go` — separate `/login` view that also does codex OAuth (its UI stays as-is; this patch doesn't touch it)
- `tui/internal/tui/SKILL.md` — provider model maintenance protocol
- Earlier commits in this thread:
  - `485bc04` — added in-editor OAuth (will be partially reverted by §3)
  - `c6b8f10` — auto-advance after OAuth in stepPresetKey (deleted by §2)
  - `ffdca4d` — forward CodexOAuthDoneMsg to editor (deleted by §3)

The previous in-editor OAuth additions weren't wasted — they got us to a cleaner mental model. The redesign here is what we should have done from the start.
