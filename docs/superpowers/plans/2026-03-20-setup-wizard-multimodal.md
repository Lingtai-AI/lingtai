# Setup Wizard Multimodal Step — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace StepVision + StepWebSearch with a single StepMultimodal page that configures all 6 multimodal capabilities (vision, web_search, talk, compose, draw, listen) individually — each with provider/key/endpoint.

**Architecture:** One wizard step with 6 capability rows. Each row has: provider selector (left/right), API key (masked), endpoint. Navigation: up/down moves between rows, tab moves between fields within a row, left/right cycles provider. The "local" provider means no API key/endpoint needed (used for listen/transcribe). Esc skips entire step.

**Tech Stack:** Go, Bubble Tea TUI

---

## Current State

StepVision and StepWebSearch were added as separate steps (Tasks 2-4 from the previous plan). These need to be replaced with a single StepMultimodal.

## Capability Configuration

| Capability | Available Providers | Default Provider | Default Endpoint |
|---|---|---|---|
| vision | minimax, gemini | minimax | https://api.minimaxi.com |
| web_search | minimax, gemini | minimax | https://api.minimaxi.com |
| talk | minimax | minimax | https://api.minimaxi.com |
| compose | minimax | minimax | https://api.minimaxi.com |
| draw | minimax | minimax | https://api.minimaxi.com |
| listen | local | local | (none) |

Note: minimax multimodal uses `api.minimaxi.com` (with `i`), different from the LLM endpoint `api.minimax.chat`.

## File Structure

- Modify: `daemon/internal/setup/wizard.go` — replace StepVision/StepWebSearch with StepMultimodal, add multimodal row model
- Modify: `daemon/internal/i18n/strings.go` — replace vision/websearch keys with multimodal key
- Modify: `daemon/internal/setup/tests_test.go` — update if needed

---

### Task 1: Define multimodal capability data structures

**Files:**
- Modify: `daemon/internal/setup/wizard.go`

- [ ] **Step 1: Define capability row type and defaults**

Add after the existing provider maps:

```go
// multimodalCapability defines one row in the multimodal config step.
type multimodalCapability struct {
    name      string   // display name
    key       string   // config key (e.g. "vision", "web_search")
    providers []string // available providers
    models    map[string]string
    endpoints map[string]string
}

var multimodalCapabilities = []multimodalCapability{
    {
        name: "Vision", key: "vision",
        providers: []string{"minimax", "gemini"},
        models: map[string]string{
            "minimax": "MiniMax-M2.7-highspeed",
            "gemini":  "gemini-3.1-pro",
        },
        endpoints: map[string]string{
            "minimax": "https://api.minimaxi.com",
            "gemini":  "https://generativelanguage.googleapis.com",
        },
    },
    {
        name: "Web Search", key: "web_search",
        providers: []string{"minimax", "gemini"},
        models: map[string]string{
            "minimax": "MiniMax-M2.7-highspeed",
            "gemini":  "gemini-3.1-pro",
        },
        endpoints: map[string]string{
            "minimax": "https://api.minimaxi.com",
            "gemini":  "https://generativelanguage.googleapis.com",
        },
    },
    {
        name: "Talk (TTS)", key: "talk",
        providers: []string{"minimax"},
        models: map[string]string{"minimax": "speech-02-hd"},
        endpoints: map[string]string{"minimax": "https://api.minimaxi.com"},
    },
    {
        name: "Compose", key: "compose",
        providers: []string{"minimax"},
        models: map[string]string{"minimax": "music-01"},
        endpoints: map[string]string{"minimax": "https://api.minimaxi.com"},
    },
    {
        name: "Draw", key: "draw",
        providers: []string{"minimax"},
        models: map[string]string{"minimax": "image-01"},
        endpoints: map[string]string{"minimax": "https://api.minimaxi.com"},
    },
    {
        name: "Listen", key: "listen",
        providers: []string{"local"},
        models: map[string]string{"local": "whisper-base"},
        endpoints: map[string]string{"local": ""},
    },
}
```

- [ ] **Step 2: Add multimodal state to wizardModel**

Replace `visionProviderIdx` and `webSearchProviderIdx` with:

```go
// multimodal step state
mmRow       int   // which capability row is focused (0-5)
mmCol       int   // which field within row (0=provider, 1=key, 2=endpoint)
mmProviders []int // provider index per capability row
```

- [ ] **Step 3: Build**

Run: `cd daemon && go build -o lingtai .`

- [ ] **Step 4: Commit**

```bash
git commit -m "refactor(wizard): define multimodal capability data structures"
```

---

### Task 2: Replace StepVision/StepWebSearch with StepMultimodal

**Files:**
- Modify: `daemon/internal/setup/wizard.go`
- Modify: `daemon/internal/i18n/strings.go`

- [ ] **Step 1: Update step constants**

Replace:
```go
StepVision
StepWebSearch
```
With:
```go
StepMultimodal
```

Update `String()`:
```go
case StepMultimodal:
    return i18n.S("setup_multimodal") + " (Esc)"
```

- [ ] **Step 2: Update i18n**

Replace `setup_vision`/`setup_websearch` with `setup_multimodal`:
```go
// en
"setup_multimodal": "Multimodal",
// zh
"setup_multimodal": "多模态",
// lzh
"setup_multimodal": "诸能",
```

- [ ] **Step 3: Initialize multimodal fields in newWizardModel**

For each capability in `multimodalCapabilities`, create 3 fields (provider, key, endpoint). Store them all in `m.fields[StepMultimodal]` as a flat array where fields[i*3+0] = provider, fields[i*3+1] = key, fields[i*3+2] = endpoint.

Initialize `m.mmProviders = make([]int, len(multimodalCapabilities))` (all 0 = first provider).

For "local" provider, the key and endpoint fields should have placeholder "(no config needed)" and be non-editable (or just visually dimmed).

- [ ] **Step 4: Remove old StepVision/StepWebSearch field initialization**

Remove the vision/web_search field init blocks and the `syncVisionDefaults()`/`syncWebSearchDefaults()` methods. Remove `visionProviders`, `visionModels`, `visionEndpoints`, `webSearchProviders`, etc. (replaced by `multimodalCapabilities`).

- [ ] **Step 5: Update navigation handlers**

For StepMultimodal:
- **up/down**: Move between capability rows (`mmRow`), reset `mmCol` to 0. Skip key/endpoint fields if provider is "local".
- **left/right**: If `mmCol == 0` (provider field), cycle through that capability's providers list. Call sync to update model/endpoint defaults. If other column, ignore.
- **tab**: Move to next field within row (mmCol 0→1→2), skip to next row after col 2. Skip key/endpoint for "local" provider.
- **shift+tab**: Reverse of tab.
- **enter**: Advance to next step.
- **esc**: Skip entire multimodal step.

- [ ] **Step 6: Update View for StepMultimodal**

Render a grid-like layout. For each capability:
```
  Vision       [minimax ◀▶]  Key: ••••••••  Endpoint: https://api.minimaxi.com
  Web Search   [minimax ◀▶]  Key: ••••••••  Endpoint: https://api.minimaxi.com
  Talk (TTS)   [minimax    ]  Key: ••••••••  Endpoint: https://api.minimaxi.com
  Compose      [minimax    ]  Key: ••••••••  Endpoint: https://api.minimaxi.com
  Draw         [minimax    ]  Key: ••••••••  Endpoint: https://api.minimaxi.com
  Listen       [local      ]  (no config needed)
```

Highlight the current row with `>`. Highlight the current field within the row. Show `◀▶` on provider field only if that capability has multiple providers.

- [ ] **Step 7: Remove old Esc/left/right handlers for StepVision/StepWebSearch**

Clean up any references to the old steps in the Update() method.

- [ ] **Step 8: Update progress bar**

The allSteps slice should now have StepMultimodal instead of StepVision + StepWebSearch.

- [ ] **Step 9: Build and verify**

Run: `cd daemon && go build -o lingtai .`

- [ ] **Step 10: Commit**

```bash
git commit -m "feat(wizard): unified multimodal configuration step"
```

---

### Task 3: Update writeConfig for multimodal

**Files:**
- Modify: `daemon/internal/setup/wizard.go`

- [ ] **Step 1: Write multimodal configs**

In writeConfig(), after writing the main model config, iterate over multimodalCapabilities. For each capability (except "listen" with "local" provider):

```go
for i, cap := range multimodalCapabilities {
    provider := m.mmFieldVal(i, 0) // provider
    model := m.mmFieldVal(i, 1)     // not stored as field — get from defaults based on provider
    key := m.mmFieldVal(i, 1)       // API key field
    endpoint := m.mmFieldVal(i, 2)  // endpoint field

    if provider == "" || provider == "local" {
        continue
    }

    capKeyEnv := strings.ToUpper(provider) + "_API_KEY"
    // ... write to model.json as sub-object
    // ... write key to .env if differs from main
}
```

Note: Need a helper `mmFieldVal(row, col int) string` to read from the flat fields array.

- [ ] **Step 2: Write multimodal API keys to .env**

For each capability with a non-empty, non-reuse key, add to envLines. Deduplicate — if multiple capabilities use the same provider and same key, write the env var only once.

- [ ] **Step 3: Update renderReview for multimodal**

Replace the old Vision/Web Search sections with a Multimodal section listing all 6 capabilities.

- [ ] **Step 4: Build and verify**

- [ ] **Step 5: Commit**

```bash
git commit -m "feat(wizard): write multimodal config to model.json"
```

---

### Task 4: Update loadExisting for multimodal

**Files:**
- Modify: `daemon/internal/setup/wizard.go`

- [ ] **Step 1: Load multimodal sub-configs**

In loadExisting(), after loading the main model, iterate over multimodalCapabilities. For each, check if `modelRaw[cap.key]` exists and pre-fill the corresponding fields + provider index.

- [ ] **Step 2: Build and verify**

- [ ] **Step 3: Commit**

```bash
git commit -m "feat(wizard): load existing multimodal config on re-setup"
```

---

### Task 5: Build and integration test

- [ ] **Step 1: Full build**

```bash
cd daemon && go build -o lingtai . && go test ./...
```

- [ ] **Step 2: Manual test**

Run `./lingtai` and verify:
- Multimodal step shows all 6 capabilities
- Up/down navigates between capabilities
- Left/right cycles providers (only on capabilities with multiple providers)
- Tab moves between provider/key/endpoint within a row
- "local" provider shows "(no config needed)" and skips key/endpoint
- Esc skips entire step
- Review shows all configured capabilities
- Config writes correctly to model.json and .env

- [ ] **Step 3: Commit**

```bash
git commit -m "feat(wizard): multimodal step complete"
```
