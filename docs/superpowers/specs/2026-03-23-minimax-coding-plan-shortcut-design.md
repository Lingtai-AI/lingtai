# MiniMax Coding Plan Shortcut — Wizard Redesign

## Problem

The current multimodal "MiniMax Quick Setup" asks for 2 API keys + endpoint (3 fields) and only fills multimodal capabilities — the user still has to separately configure the main LLM. This defeats the purpose of a "quick" setup. MiniMax's Coding Plan (代码方案) provides one API key that covers everything: LLM + vision + web search + TTS + image gen + music gen.

## Design

### New Step: `StepQuickStart`

Inserted after `StepCombo`, before `StepModel`. Two bare options, no descriptions:

```
  >  MiniMax Coding Plan (Recommended)
     Configure Manually
```

### MiniMax Coding Plan Screen

One API key, one endpoint selector:

```
MiniMax Coding Plan (代码方案)

  Endpoint: ◀ https://api.minimaxi.com (China) ▶

  API Key
  [•••••••••••]

Tab: next field | ←/→: cycle endpoint | Enter: apply & continue | Esc: back
```

### On Apply

Sets everything from one key + endpoint:

1. **Main LLM**: provider=`minimax`, model=`MiniMax-M2.7-highspeed`, base_url from endpoint selector, api_key from input
2. **All multimodal caps** (vision, web_search, talk, compose, draw): provider=`minimax`, same key, same endpoint. Both `MINIMAX_API_KEY` and `MINIMAX_MCP_API_KEY` env vars set to the same key value.
3. **Listen**: stays `local` (no API key needed)
4. **Skips** `StepModel` and `StepMultimodal` entirely → advances to `StepMessaging`

### "Configure Manually" Path

Proceeds to `StepModel` (existing flow). The old "MiniMax Quick Setup" option is removed from the multimodal chooser. Multimodal chooser becomes:

- Manual Configuration
- Skip

## Step Order

```
StepLang → StepCombo → StepQuickStart → StepModel → StepMultimodal → StepMessaging → StepGeneral → StepReview
                              │                                              ▲
                              │ (MiniMax Coding Plan)                        │
                              └──────────────────────────────────────────────┘
                                         (skips Model + Multimodal)
```

## Changes

### wizard.go

1. Add `StepQuickStart` constant (between `StepCombo` and `StepModel`)
2. Add wizard state fields:
   - `qsMode int` — 0=chooser, 1=coding plan form
   - `qsChooserIdx int` — 0=MiniMax, 1=Manual
   - `qsKey textinput.Model` — single API key input
   - `qsEndpoint int` — endpoint index (reuse `mmQuickEndpoints`)
   - `qsFocus int` — 0=endpoint, 1=key
3. Add `qsApply()` method — fills LLM fields + all multimodal rows from single key + endpoint, then skips to `StepMessaging`
4. Modify `advanceStep()` — when advancing from `StepQuickStart` with coding plan applied, jump to `StepMessaging` (skip `StepModel` + `StepMultimodal`)
5. Add `renderQSChooser()` and `renderQSForm()` view methods
6. Add keyboard handling for `StepQuickStart` in `Update()`
7. Remove `mmQuickKey2` field (second MCP key — no longer needed)
8. Remove quick setup mode from multimodal chooser (mmChooserIdx 0 was "MiniMax Quick Setup")
9. Simplify multimodal chooser to just "Manual Configuration" / "Skip"

### strings.go (i18n — all 3 locales)

New keys:
- `setup_quickstart` — step label
- `qs_minimax` — "MiniMax Coding Plan (Recommended)"
- `qs_manual` — "Configure Manually"
- `qs_title` — "MiniMax Coding Plan" (form screen title)
- `qs_hint` — "Tab: next field | ←/→: cycle endpoint | Enter: apply & continue | Esc: back"

Remove keys:
- `mm_quick_setup`, `mm_quick`, `mm_quick_desc`
- `mm_key_vision_desc`, `mm_key_mcp_desc`
- `mm_quick_hint`

### Combo loading

When loading an existing combo that was created via the coding plan shortcut, `loadExisting()` should detect the pattern (all minimax, same key) and pre-select the coding plan path with pre-filled key + endpoint.

## What Stays the Same

- Manual flow (Model → Multimodal grid) — untouched except removing quick setup option
- Messaging, General, Review steps — untouched
- Combo save/load format — untouched (same JSON output)
- All other TUI views — untouched
