# MiniMax Coding Plan Shortcut — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the 2-key MiniMax Quick Setup with a 1-key "MiniMax Coding Plan (Recommended)" shortcut that fills LLM + all multimodal in one step.

**Architecture:** Add `StepQuickStart` between `StepCombo` and `StepModel`. When the user picks the coding plan, a single API key + endpoint selector fills the main LLM config and all multimodal capability rows, then skips `StepModel` + `StepMultimodal` entirely. The old quick setup is removed from the multimodal chooser.

**Tech Stack:** Go (module `lingtai-tui`), Bubble Tea TUI framework, lipgloss styling

**Spec:** `docs/superpowers/specs/2026-03-23-minimax-coding-plan-shortcut-design.md`

**Code location:** `tui/` (NOT the old `app/orchestration/` which is deleted from the working tree)

**Current state:** The wizard (`wizard.go`) and its dependencies (`combo/`, `config/`) have NOT been migrated to `tui/` yet. They only exist in git HEAD under `app/orchestration/`. The `tui/` directory has `i18n/strings.go` with all the setup/wizard i18n keys already present, but no wizard code to use them. The wizard must be migrated first.

**Migration source:** `git show HEAD:app/orchestration/internal/setup/wizard.go` (1924 lines), `git show HEAD:app/orchestration/internal/combo/combo.go` (87 lines), `git show HEAD:app/orchestration/internal/config/loader.go` (193 lines). Module name changes from `lingtai-daemon` to `lingtai-tui`.

---

### Task 1: Migrate combo and config packages to tui/

**Files:**
- Create: `tui/internal/combo/combo.go`
- Create: `tui/internal/combo/combo_test.go`
- Create: `tui/internal/config/loader.go`
- Create: `tui/internal/config/loader_test.go`

- [ ] **Step 1: Copy combo package from git HEAD**

```bash
mkdir -p tui/internal/combo tui/internal/config
git show HEAD:app/orchestration/internal/combo/combo.go > tui/internal/combo/combo.go
git show HEAD:app/orchestration/internal/combo/combo_test.go > tui/internal/combo/combo_test.go
git show HEAD:app/orchestration/internal/config/loader.go > tui/internal/config/loader.go
git show HEAD:app/orchestration/internal/config/loader_test.go > tui/internal/config/loader_test.go
```

- [ ] **Step 2: Fix import paths**

In all copied files, replace `lingtai-daemon/internal/` → `lingtai-tui/internal/` (if any cross-package imports exist).

- [ ] **Step 3: Compile and test**

```bash
cd tui && go build ./... && go test ./internal/combo/ ./internal/config/
```
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add tui/internal/combo/ tui/internal/config/
git commit -m "feat(tui): migrate combo and config packages from app/orchestration"
```

---

### Task 2: Migrate wizard and setup package to tui/

**Files:**
- Create: `tui/internal/setup/wizard.go`
- Create: `tui/internal/setup/tests.go`
- Create: `tui/internal/setup/tests_test.go`
- Create: `tui/internal/setup/defaults/` (embedded files)

- [ ] **Step 1: Copy setup package from git HEAD**

```bash
mkdir -p tui/internal/setup/defaults
git show HEAD:app/orchestration/internal/setup/wizard.go > tui/internal/setup/wizard.go
git show HEAD:app/orchestration/internal/setup/tests.go > tui/internal/setup/tests.go
git show HEAD:app/orchestration/internal/setup/tests_test.go > tui/internal/setup/tests_test.go
git show HEAD:app/orchestration/internal/setup/defaults/bash_policy.json > tui/internal/setup/defaults/bash_policy.json
git show HEAD:app/orchestration/internal/setup/defaults/covenant_en.md > tui/internal/setup/defaults/covenant_en.md
git show HEAD:app/orchestration/internal/setup/defaults/covenant_zh.md > tui/internal/setup/defaults/covenant_zh.md
git show HEAD:app/orchestration/internal/setup/defaults/covenant_wen.md > tui/internal/setup/defaults/covenant_wen.md
```

- [ ] **Step 2: Fix import paths**

In all `.go` files, replace `lingtai-daemon/internal/` → `lingtai-tui/internal/`.

- [ ] **Step 3: Wire wizard into root.go**

In `tui/internal/tui/root.go`:
- Add `ViewWizard` and `ViewStarting` to the View enum
- Add `wizard setup.WizardModel` field to `RootModel`
- Add routing logic (same pattern as old `app/orchestration/internal/tui/root.go`)
- Import `lingtai-tui/internal/setup`

- [ ] **Step 4: Compile and test**

```bash
cd tui && go build ./... && go test ./...
```
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add tui/internal/setup/ tui/internal/tui/root.go
git commit -m "feat(tui): migrate setup wizard from app/orchestration"
```

---

### Task 3: Update i18n keys for quick-start

**Files:**
- Modify: `tui/internal/i18n/strings.go`

- [ ] **Step 1: Add new i18n keys to English locale**

After the `"combo"` key block (~line 91), add:

```go
// Quick Start
"setup_quickstart":  "Quick Start",
"qs_minimax":        "MiniMax Coding Plan (Recommended)",
"qs_manual":         "Configure Manually",
"qs_title":          "MiniMax Coding Plan",
"qs_hint":           "Tab: next field | ←/→: cycle endpoint | Enter: apply & continue | Esc: back",
```

- [ ] **Step 2: Remove old English quick-setup keys**

Remove: `"mm_quick_setup"`, `"mm_quick"`, `"mm_quick_desc"`, `"mm_key_vision_desc"`, `"mm_key_mcp_desc"`, `"mm_quick_hint"`.

Change `"mm_manual_desc"` to `"Configure each capability individually"`.

- [ ] **Step 3: Add new i18n keys to Chinese locale**

After combo block (~line 239):

```go
// Quick Start
"setup_quickstart":  "快速开始",
"qs_minimax":        "稀宇代码方案（推荐）",
"qs_manual":         "手动配置",
"qs_title":          "稀宇代码方案",
"qs_hint":           "Tab: 下一项 | ←/→: 切换 endpoint | Enter: 应用并继续 | Esc: 返回",
```

Remove same old keys from zh. Change zh `"mm_manual_desc"` to `"逐个配置每项能力"`.

- [ ] **Step 4: Add new i18n keys to Classical Chinese locale**

After combo block (~line 387):

```go
// Quick Start
"setup_quickstart":  "速始",
"qs_minimax":        "稀宇码策（荐）",
"qs_manual":         "手设",
"qs_title":          "稀宇码策",
"qs_hint":           "Tab: 次项 | ←/→: 择入口 | Enter: 用之并续 | Esc: 退",
```

Remove same old keys from lzh. Change lzh `"mm_manual_desc"` to `"逐一设之"`.

- [ ] **Step 5: Commit**

```bash
git add tui/internal/i18n/strings.go
git commit -m "feat(tui): add quick-start i18n keys, remove old mm quick-setup keys"
```

---

### Task 4: Implement StepQuickStart in wizard

**Files:**
- Modify: `tui/internal/setup/wizard.go`

This is the core task. All changes are in `wizard.go`:

- [ ] **Step 1: Add StepQuickStart to step enum** (between StepCombo and StepModel)

- [ ] **Step 2: Add StepQuickStart to String() method**

```go
case StepQuickStart:
    return i18n.S("setup_quickstart")
```

- [ ] **Step 3: Add quick-start state fields to WizardModel**

```go
// quick start state
qsMode       int             // 0=chooser, 1=coding plan form
qsChooserIdx int             // 0=MiniMax, 1=Manual
qsKey        textinput.Model // single API key
qsEndpoint   int             // index into mmQuickEndpoints
qsFocus      int             // 0=endpoint, 1=key
qsApplied    bool            // true if coding plan was applied (skip Model+Multimodal)
```

- [ ] **Step 4: Remove mmQuickKey2 field** from WizardModel and its initialization in NewWizardModel()

- [ ] **Step 5: Initialize quick-start fields in NewWizardModel()**

```go
qsKey := newTextInput("sk-...", "")
qsKey.EchoMode = textinput.EchoPassword
qsKey.EchoCharacter = '•'
m.qsKey = qsKey
m.qsEndpoint = 0 // china default
```

- [ ] **Step 6: Add qsApply() method**

```go
func (m *WizardModel) qsApply() {
	ep := mmQuickEndpoints[m.qsEndpoint]
	apiKey := m.qsKey.Value()
	m.providerIdx = 0
	m.fields[StepModel][0].input.SetValue("minimax")
	m.fields[StepModel][1].input.SetValue(providerModels["minimax"])
	m.fields[StepModel][2].input.SetValue(apiKey)
	m.fields[StepModel][3].input.SetValue(ep + "/anthropic")
	for i, cap := range mmCaps {
		if cap.providers[0] == "local" {
			continue
		}
		m.mmRows[i].providerIdx = 0
		m.mmRows[i].endpointInput.SetValue(ep)
		m.mmRows[i].keyInput.SetValue(apiKey)
	}
	m.qsApplied = true
}
```

- [ ] **Step 7: Add renderQSChooser() and renderQSForm() methods**

Chooser: two bare options (`qs_minimax`, `qs_manual`), no descriptions.

Form: endpoint selector (◀ url (label) ▶) + single API key field.

- [ ] **Step 8: Add StepQuickStart handling in Update()**

Handle all key events: Esc (back to chooser), Tab/Down + Shift+Tab/Up (cycle fields/options), Left/Right (cycle endpoint), Enter (select option or apply+skip to StepMessaging).

Text input forwarding for qsKey when focused.

- [ ] **Step 9: Add StepQuickStart rendering in View()**

Dispatch to renderQSChooser() or renderQSForm() based on qsMode.

- [ ] **Step 10: Update progress bar in View()**

Add StepQuickStart to allSteps. Filter out StepModel + StepMultimodal when qsApplied is true.

- [ ] **Step 11: Compile and verify**

```bash
cd tui && go build ./...
```

- [ ] **Step 12: Commit**

```bash
git add tui/internal/setup/wizard.go
git commit -m "feat(tui): implement StepQuickStart with MiniMax Coding Plan shortcut"
```

---

### Task 5: Clean up old quick-setup code from multimodal step

**Files:**
- Modify: `tui/internal/setup/wizard.go`

- [ ] **Step 1: Remove all mmMode=1 (quick setup) code from Update()**

Remove `case 1:` blocks under StepMultimodal in esc, tab/down, shift+tab/up, left, right, enter handlers.

- [ ] **Step 2: Update multimodal chooser to 2 options**

Change mmChooserIdx cycling from `% 3` → `% 2`. Update enter handler:
- case 0 = Manual Configuration (mmMode=2)
- case 1 = Skip (advanceStep)

- [ ] **Step 3: Update renderMMChooser()** to show only Manual/Skip

- [ ] **Step 4: Remove renderMMQuick() and mmApplyQuickSetup()** — dead code

- [ ] **Step 5: Remove mmQuickKey1, mmQuickFocus, mmQuickEndpoint** fields and initialization

- [ ] **Step 6: Remove View() case for mmMode 1**

- [ ] **Step 7: Compile and verify**

```bash
cd tui && go build ./...
```

- [ ] **Step 8: Commit**

```bash
git add tui/internal/setup/wizard.go
git commit -m "refactor(tui): remove old MiniMax Quick Setup from multimodal step"
```

---

### Task 6: Pre-fill quick start from existing config (loadExisting)

**Files:**
- Modify: `tui/internal/setup/wizard.go`

- [ ] **Step 1: Add detection logic to loadExisting()**

At the end of `loadExisting()`, after multimodal sub-config loading, detect the coding plan pattern (main provider=minimax, all non-local caps use minimax with same key):

```go
if modelCfg.Provider == "minimax" {
	mainKey := m.fields[StepModel][2].input.Value()
	allMinimax := mainKey != ""
	for i, cap := range mmCaps {
		if cap.providers[0] == "local" {
			continue
		}
		rowKey := m.mmRows[i].keyInput.Value()
		if rowKey == "" || rowKey != mainKey {
			allMinimax = false
			break
		}
		p := cap.providers[m.mmRows[i].providerIdx]
		if p != "minimax" {
			allMinimax = false
			break
		}
	}
	if allMinimax {
		m.qsKey.SetValue(mainKey)
		m.qsApplied = true
		baseURL := m.fields[StepModel][3].input.Value()
		for idx, ep := range mmQuickEndpoints {
			if strings.HasPrefix(baseURL, ep) {
				m.qsEndpoint = idx
				break
			}
		}
	}
}
```

- [ ] **Step 2: Compile and verify**

```bash
cd tui && go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add tui/internal/setup/wizard.go
git commit -m "feat(tui): pre-fill quick start from existing coding plan config"
```

---

### Task 7: Build and test

**Files:** None (testing only)

- [ ] **Step 1: Build the binary**

```bash
cd tui && go build -o /tmp/lingtai-tui .
```

- [ ] **Step 2: Run all Go tests**

```bash
cd tui && go test ./...
```

- [ ] **Step 3: Verify wizard integration in root.go**

Ensure the wizard view is properly wired and accessible from the TUI.
