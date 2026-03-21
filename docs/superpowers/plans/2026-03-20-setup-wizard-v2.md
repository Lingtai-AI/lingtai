# Setup Wizard V2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Redesign the setup wizard to be user-friendly for first-time users: vision/web_search model steps, direct API key entry, default file shipping, and provider defaults all pointing to MiniMax.

**Architecture:** Expand wizard steps from 6 to 8 (add StepVision, StepWebSearch between StepModel and StepIMAP). Ship default covenant and bash policy into `~/.lingtai/` on first setup. All providers default to MiniMax. Vision/web_search are optional steps (Esc to skip) that reuse or add new API keys.

**Tech Stack:** Go, Bubble Tea TUI, existing wizard framework

---

## File Structure

- Modify: `daemon/internal/setup/wizard.go` — add StepVision, StepWebSearch, vision/web_search provider selectors, default file copying, API key reuse
- Modify: `daemon/internal/config/loader.go` — add `Language` field to Config struct
- Modify: `daemon/internal/i18n/strings.go` — add new step labels
- Modify: `daemon/internal/setup/tests_test.go` — update round-trip test for new config shape
- Reference: `daemon/covenant_en.md`, `daemon/covenant_zh.md` — shipped as defaults
- Reference: `src/lingtai/capabilities/bash_policy.json` — shipped as default

---

### Task 1: Update provider defaults to MiniMax

All providers default to MiniMax. Update default model to `MiniMax-M2.7-highspeed`.

**Files:**
- Modify: `daemon/internal/setup/wizard.go`

- [ ] **Step 1: Update providerModels default**

Change the first provider (minimax) default model:
```go
var providerModels = map[string]string{
	"minimax":   "MiniMax-M2.7-highspeed",
	// ... rest unchanged
}
```

- [ ] **Step 2: Build and verify**

Run: `cd daemon && go build -o lingtai .`
Expected: clean build

- [ ] **Step 3: Commit**

```bash
git add daemon/internal/setup/wizard.go
git commit -m "fix(wizard): default model to MiniMax-M2.7-highspeed"
```

---

### Task 2: Add StepVision and StepWebSearch

Insert two new optional steps between StepModel and StepIMAP. Each has: provider (left/right selector, default minimax), model name, API key (masked), endpoint. Both skippable with Esc. When provider matches the main model's provider, hint "same key as main model" and pre-fill the API key.

**Files:**
- Modify: `daemon/internal/setup/wizard.go`
- Modify: `daemon/internal/i18n/strings.go`

- [ ] **Step 1: Add step constants**

```go
const (
	StepLang step = iota
	StepModel
	StepVision    // NEW
	StepWebSearch // NEW
	StepIMAP
	StepTelegram
	StepGeneral
	StepReview
)
```

Add `String()` cases:
```go
case StepVision:
    return "Vision (Esc)"
case StepWebSearch:
    return "Web Search (Esc)"
```

- [ ] **Step 2: Add i18n keys**

In `strings.go`, add to all three language maps:
```go
// en
"setup_vision":     "Vision Model",
"setup_websearch":  "Web Search Model",
"setup_same_key":   "Same API key as main model — leave blank to reuse",

// zh
"setup_vision":     "视觉模型",
"setup_websearch":  "搜索模型",
"setup_same_key":   "与主模型相同的 API key — 留空即复用",

// lzh
"setup_vision":     "目识之模",
"setup_websearch":  "索引之模",
"setup_same_key":   "与主模同钥 — 留空即复用",
```

- [ ] **Step 3: Add vision/web_search provider state**

Add to `wizardModel`:
```go
visionProviderIdx    int
webSearchProviderIdx int
```

- [ ] **Step 4: Add vision provider defaults**

Only minimax and gemini support vision/web_search as dedicated services:
```go
var visionProviders = []string{"minimax", "gemini"}

var visionModels = map[string]string{
	"minimax": "MiniMax-M2.7-highspeed",
	"gemini":  "gemini-3.1-pro",
}

var visionEndpoints = map[string]string{
	"minimax": "https://api.minimax.chat/v1",
	"gemini":  "https://generativelanguage.googleapis.com",
}
```

Same maps for web search (can reuse the same — `webSearchProviders`, `webSearchModels`, `webSearchEndpoints`).

- [ ] **Step 5: Initialize fields in newWizardModel**

```go
// Step: Vision (optional)
visionKeyInput := newTextInput("leave blank to reuse main key", "")
visionKeyInput.EchoMode = textinput.EchoPassword
visionKeyInput.EchoCharacter = '•'
m.fields[StepVision] = []field{
    {label: "Provider", input: newTextInput("minimax", "minimax")},
    {label: "Model", input: newTextInput("model name", visionModels["minimax"])},
    {label: "API key (blank = reuse main)", input: visionKeyInput},
    {label: "Endpoint", input: newTextInput("https://...", visionEndpoints["minimax"])},
}

// Step: Web Search (optional) — same structure
webSearchKeyInput := newTextInput("leave blank to reuse main key", "")
webSearchKeyInput.EchoMode = textinput.EchoPassword
webSearchKeyInput.EchoCharacter = '•'
m.fields[StepWebSearch] = []field{
    {label: "Provider", input: newTextInput("minimax", "minimax")},
    {label: "Model", input: newTextInput("model name", webSearchModels["minimax"])},
    {label: "API key (blank = reuse main)", input: webSearchKeyInput},
    {label: "Endpoint", input: newTextInput("https://...", webSearchEndpoints["minimax"])},
}
```

- [ ] **Step 6: Handle Esc skip for new steps**

In the `"esc"` handler, add:
```go
if m.step == StepIMAP || m.step == StepTelegram || m.step == StepVision || m.step == StepWebSearch {
```

- [ ] **Step 7: Handle left/right provider cycling for vision/web_search**

In the `"left"` and `"right"` handlers, add cases for `StepVision` and `StepWebSearch` similar to StepModel, cycling through `visionProviders`/`webSearchProviders` and calling a `syncVisionDefaults()`/`syncWebSearchDefaults()` method.

- [ ] **Step 8: Update progress bar**

The `allSteps` slice in View() already uses the step constants, so it auto-updates. Verify it renders correctly.

- [ ] **Step 9: Update View to show endpoint for vision/web_search steps**

The endpoint field (index 3) should always show for vision/web_search steps, not just for "custom" provider. Remove the `provider != "custom"` skip logic or scope it to StepModel only.

- [ ] **Step 10: Build and verify**

Run: `cd daemon && go build -o lingtai .`

- [ ] **Step 11: Commit**

```bash
git add daemon/internal/setup/wizard.go daemon/internal/i18n/strings.go
git commit -m "feat(wizard): add vision and web search model steps"
```

---

### Task 3: Update writeConfig for vision/web_search

Write vision and web_search configs into model.json. If the API key is blank, reuse the main model's env var. If different, create a separate env var (e.g. `GEMINI_API_KEY`).

**Files:**
- Modify: `daemon/internal/setup/wizard.go` — `writeConfig()` method

- [ ] **Step 1: Update model.json writing**

After writing the main model config, add vision/web_search if fields are non-empty:

```go
// Vision config (if not skipped)
if visionProvider := m.fieldVal(StepVision, 0); visionProvider != "" && m.fieldVal(StepVision, 1) != "" {
    visionKeyEnv := apiKeyEnv // reuse main key by default
    if visionKey := m.fieldVal(StepVision, 2); visionKey != "" {
        // Different key — derive env var name
        visionKeyEnv = strings.ToUpper(visionProvider) + "_API_KEY"
        if visionKeyEnv == apiKeyEnv {
            // Same provider, different key — suffix it
            visionKeyEnv = strings.ToUpper(visionProvider) + "_VISION_API_KEY"
        }
    }
    visionCfg := map[string]interface{}{
        "provider":    visionProvider,
        "model":       m.fieldVal(StepVision, 1),
        "api_key_env": visionKeyEnv,
    }
    if endpoint := m.fieldVal(StepVision, 3); endpoint != "" {
        visionCfg["base_url"] = endpoint
    }
    modelCfg["vision"] = visionCfg
}
```

Same pattern for web_search.

- [ ] **Step 2: Update .env writing**

Add vision/web_search API keys if they differ from the main key:

```go
if visionKey := m.fieldVal(StepVision, 2); visionKey != "" {
    visionProvider := m.fieldVal(StepVision, 0)
    visionKeyEnv := strings.ToUpper(visionProvider) + "_API_KEY"
    // Only add if not already covered by main key
    if visionKey != m.fieldVal(StepModel, 2) {
        envLines = append(envLines, fmt.Sprintf("%s=%s", visionKeyEnv, visionKey))
    }
}
```

Same for web_search.

- [ ] **Step 3: Update renderReview**

Add vision and web_search sections to the review screen.

- [ ] **Step 4: Build and verify**

Run: `cd daemon && go build -o lingtai .`

- [ ] **Step 5: Commit**

```bash
git add daemon/internal/setup/wizard.go
git commit -m "feat(wizard): write vision/web_search config to model.json"
```

---

### Task 4: Update loadExisting for vision/web_search

Pre-fill vision and web_search fields when re-running setup.

**Files:**
- Modify: `daemon/internal/setup/wizard.go` — `loadExisting()` method

- [ ] **Step 1: Add vision/web_search loading**

In `loadExisting()`, after loading the main model config, check for vision/web_search sub-configs in the model JSON:

```go
// Vision
var visionCfg struct { ... }
// load from modelData and pre-fill StepVision fields

// Web Search
var webSearchCfg struct { ... }
// load from modelData and pre-fill StepWebSearch fields
```

- [ ] **Step 2: Build and verify**

Run: `cd daemon && go build -o lingtai .`

- [ ] **Step 3: Commit**

```bash
git add daemon/internal/setup/wizard.go
git commit -m "feat(wizard): pre-fill vision/web_search from existing config"
```

---

### Task 5: Ship default covenant and bash policy

On setup completion, copy default covenant (based on language) and bash policy to `~/.lingtai/` if they don't exist. Update General step labels to show default paths.

**Files:**
- Modify: `daemon/internal/setup/wizard.go`

- [ ] **Step 1: Embed default files**

Add `embed` import and embed the default files:

```go
import "embed"

//go:embed covenant_en.md
var defaultCovenantEN string

//go:embed covenant_zh.md
var defaultCovenantZH string

//go:embed bash_policy.json
var defaultBashPolicy string
```

Note: The covenant files are in `daemon/` (same directory as the setup package's parent). The bash policy is in `src/lingtai/`. We need to copy `bash_policy.json` into `daemon/` for embedding, or use a relative embed path. Simplest: copy `bash_policy.json` to `daemon/bash_policy.json`.

Actually, embed paths are relative to the Go source file. Since `wizard.go` is in `daemon/internal/setup/`, we need the files there or use `//go:embed ../../../covenant_en.md`. The cleanest approach: copy the defaults into `daemon/internal/setup/defaults/` and embed from there.

- [ ] **Step 2: Create defaults directory**

```bash
mkdir -p daemon/internal/setup/defaults
cp daemon/covenant_en.md daemon/internal/setup/defaults/
cp daemon/covenant_zh.md daemon/internal/setup/defaults/
cp src/lingtai/capabilities/bash_policy.json daemon/internal/setup/defaults/
```

- [ ] **Step 3: Add embed in wizard.go**

```go
//go:embed defaults/covenant_en.md
var defaultCovenantEN string

//go:embed defaults/covenant_zh.md
var defaultCovenantZH string

//go:embed defaults/bash_policy.json
var defaultBashPolicy string
```

- [ ] **Step 4: Update General step labels**

```go
m.fields[StepGeneral] = []field{
    {label: "Agent name", input: newTextInput("orchestrator", "orchestrator")},
    {label: "Base directory", input: newTextInput(defaultBase, defaultBase)},
    {label: "Agent port", input: newTextInput("8501", "8501")},
    {label: "Bash policy (default: ~/.lingtai/bash_policy.json)", input: newTextInput("Enter = use default", "")},
    {label: "Covenant (default: ~/.lingtai/covenant.md)", input: newTextInput("Enter = use default", "")},
}
```

- [ ] **Step 5: Write defaults in writeConfig**

At the end of `writeConfig()`, write default files if they don't exist:

```go
// 4. Default files
bashPolicyPath := filepath.Join(m.outputDir, "bash_policy.json")
if _, err := os.Stat(bashPolicyPath); os.IsNotExist(err) {
    os.WriteFile(bashPolicyPath, []byte(defaultBashPolicy), 0644)
    written = append(written, bashPolicyPath)
}

covenantPath := filepath.Join(m.outputDir, "covenant.md")
if _, err := os.Stat(covenantPath); os.IsNotExist(err) {
    covenant := defaultCovenantEN
    langCode := i18n.Languages[m.langIdx]
    if langCode == "zh" || langCode == "lzh" {
        covenant = defaultCovenantZH
    }
    os.WriteFile(covenantPath, []byte(covenant), 0644)
    written = append(written, covenantPath)
}
```

- [ ] **Step 6: Update config.json writing**

If bash_policy or covenant fields are empty, use the default paths:

```go
bashPolicy := m.fieldVal(StepGeneral, 3)
if bashPolicy == "" {
    bashPolicy = filepath.Join(m.outputDir, "bash_policy.json")
}
cfg["bash_policy"] = bashPolicy

covenant := m.fieldVal(StepGeneral, 4)
if covenant == "" {
    covenant = filepath.Join(m.outputDir, "covenant.md")
}
cfg["covenant"] = covenant
```

- [ ] **Step 7: Build and verify**

Run: `cd daemon && go build -o lingtai .`

- [ ] **Step 8: Commit**

```bash
git add daemon/internal/setup/
git commit -m "feat(wizard): ship default covenant and bash policy to ~/.lingtai/"
```

---

### Task 6: Fix connection test functions for direct values

The IMAP and Telegram test functions currently read env vars by name. Now we have direct values in the fields. Update `runTest()`.

**Files:**
- Modify: `daemon/internal/setup/wizard.go` — `runTest()` method

- [ ] **Step 1: Update IMAP test**

```go
case StepIMAP:
    return func() tea.Msg {
        email := m.fieldVal(StepIMAP, 0)
        pass := m.fieldVal(StepIMAP, 1) // direct value now
        imapHost := m.fieldVal(StepIMAP, 2)
        imapPortStr := m.fieldVal(StepIMAP, 3)

        if pass == "" {
            return testResultMsg{step: StepIMAP, result: TestResult{OK: false, Message: "password is required"}}
        }
        // ... rest same
    }
```

- [ ] **Step 2: Update Telegram test**

```go
case StepTelegram:
    return func() tea.Msg {
        token := m.fieldVal(StepTelegram, 0) // direct value now
        if token == "" {
            return testResultMsg{step: StepTelegram, result: TestResult{OK: false, Message: "bot token is required"}}
        }
        r := TestTelegram(token)
        return testResultMsg{step: StepTelegram, result: r}
    }
```

- [ ] **Step 3: Build and verify**

Run: `cd daemon && go build -o lingtai .`

- [ ] **Step 4: Update test**

Update `TestWizardConfigRoundTrip` in `tests_test.go` to include `language` field and verify it round-trips.

- [ ] **Step 5: Run tests**

Run: `cd daemon && go test ./... -v`

- [ ] **Step 6: Commit**

```bash
git add daemon/internal/setup/
git commit -m "fix(wizard): use direct values for connection tests, update round-trip test"
```

---

### Task 7: Final integration test

- [ ] **Step 1: Build final binary**

Run: `cd daemon && go build -o lingtai .`

- [ ] **Step 2: Test fresh setup**

Run `./lingtai` with no existing `~/.lingtai/`. Verify:
- Language selector works (up/down)
- Banner changes with language
- Model step shows MiniMax-M2.7-highspeed as default
- Provider cycling updates model + endpoint
- Vision step shows, skippable with Esc
- Web Search step shows, skippable with Esc
- IMAP/Telegram skippable with Esc
- General shows default paths for bash policy and covenant
- Review shows all configured values
- Save writes config.json, model.json, .env, covenant.md, bash_policy.json

- [ ] **Step 3: Test re-setup**

Run `./lingtai setup` again. Verify all fields pre-filled from saved config.

- [ ] **Step 4: Commit**

```bash
git add .
git commit -m "feat(wizard): v2 complete — vision, web_search, defaults, direct API keys"
```
