package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/preset"
)

func testPresetEditorPreset() preset.Preset {
	return preset.Preset{
		Name: "scroll-test",
		Description: preset.PresetDescription{
			Summary: "A preset used by preset editor tests",
			Tier:    "3",
			Extra: map[string]interface{}{
				"gains": "good at testing",
				"loses": "not real",
			},
		},
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{
				"provider":    "openai",
				"model":       "gpt-test",
				"base_url":    "https://api.example.com/v1",
				"api_key_env": "EXAMPLE_API_KEY",
				"wire_api":    "chat_completions",
			},
			"capabilities": map[string]interface{}{
				"file":       map[string]interface{}{},
				"shell":      map[string]interface{}{"yolo": true},
				"avatar":     map[string]interface{}{},
				"daemon":     map[string]interface{}{},
				"web_search": map[string]interface{}{"provider": "duckduckgo"},
				"vision":     map[string]interface{}{"provider": "inherit"},
			},
		},
	}
}

func TestPresetEditorValidationErrorUsesCurrentLocale(t *testing.T) {
	t.Cleanup(func() { _ = i18n.SetLang("en") })
	if err := i18n.SetLang("zh"); err != nil {
		t.Fatalf("SetLang zh: %v", err)
	}

	p := testPresetEditorPreset()
	llm := p.Manifest["llm"].(map[string]interface{})
	llm["model"] = ""

	m := NewPresetEditorModelWithBuiltinFlag(p, "zh", nil, "", false)
	updated, cmd := m.commit()
	if cmd != nil {
		t.Fatal("invalid preset commit must not emit a command")
	}
	if got, want := updated.saveErr, "模型不能为空。"; got != want {
		t.Fatalf("saveErr = %q, want %q", got, want)
	}
	if strings.Contains(updated.saveErr, "manifest.llm.model") {
		t.Fatalf("localized saveErr leaked schema text: %q", updated.saveErr)
	}
}

func testCodexPresetEditorPreset(serviceTier interface{}) preset.Preset {
	return testCodexPresetEditorPresetWithThinking(serviceTier, nil)
}

func testCodexPresetEditorPresetWithThinking(serviceTier interface{}, thinking interface{}) preset.Preset {
	llm := map[string]interface{}{
		"provider":    "codex",
		"model":       "gpt-5.6-sol",
		"api_key":     nil,
		"api_key_env": "",
		"base_url":    "https://chatgpt.com/backend-api/codex",
	}
	if serviceTier != nil {
		llm["service_tier"] = serviceTier
	}
	if thinking != nil {
		llm["thinking"] = thinking
	}
	return preset.Preset{
		Name:        "codex-test",
		Description: preset.PresetDescription{Summary: "Codex editor test preset"},
		Manifest: map[string]interface{}{
			"llm": llm,
			"capabilities": map[string]interface{}{
				"web_search": map[string]interface{}{"provider": "codex"},
				"vision":     map[string]interface{}{"provider": "codex"},
			},
		},
	}
}

func builtinPresetForEditorTest(t *testing.T, name string) preset.Preset {
	t.Helper()
	for _, p := range preset.BuiltinPresets() {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("built-in preset %q not found", name)
	return preset.Preset{}
}

// TestPresetEditorProviderModelLineupsPinRequestedDefaults pins the only two
// curated catalogs: the Codex OAuth route and the Claude Code CLI aliases.
// The openai and anthropic families point at arbitrary endpoints, so their
// model row is free text.
func TestPresetEditorProviderModelLineupsPinRequestedDefaults(t *testing.T) {
	// GPT-6 Astra is documented but account/client rollout is not proven, so
	// Sol remains the default-first entry. The named GPT-5.6 routes are one
	// generation's variants.
	wantCodexModels := []string{
		"gpt-5.6-sol", "gpt-6-astra", "gpt-5.6-terra", "gpt-5.6-luna",
	}
	if models := providerModels["codex"]; !reflect.DeepEqual(models, wantCodexModels) {
		t.Fatalf("codex provider models = %#v, want %#v", models, wantCodexModels)
	}
	wantClaudeModels := []string{"opus", "fable", "sonnet", "haiku"}
	if got := providerModels["claude-code"]; !reflect.DeepEqual(got, wantClaudeModels) {
		t.Fatalf("claude-code provider models = %#v, want %#v", got, wantClaudeModels)
	}
	gotProviders := make([]string, 0, len(providerModels))
	for provider := range providerModels {
		gotProviders = append(gotProviders, provider)
	}
	sort.Strings(gotProviders)
	if want := []string{"claude-code", "codex"}; !reflect.DeepEqual(gotProviders, want) {
		t.Fatalf("curated catalogs = %#v, want only %#v", gotProviders, want)
	}
	for _, provider := range []string{"openai", "anthropic"} {
		if got := modelOptions(provider); got != nil {
			t.Fatalf("%s must keep a free-text model row, got catalog %#v", provider, got)
		}
	}
	// Each template's default model is the first entry of its catalog.
	for _, name := range []string{"codex", "claude"} {
		p := builtinPresetForEditorTest(t, name)
		llm := p.Manifest["llm"].(map[string]interface{})
		provider := asString(llm["provider"])
		if got, want := asString(llm["model"]), providerModels[provider][0]; got != want {
			t.Fatalf("%s template model = %q, want catalog default %q", name, got, want)
		}
	}
}

// TestPresetEditorVisionProviderIdentityIsCommitImmutable is the
// regression test for the always-included-capabilities change: there is
// no longer any editor control (checkbox, provider cycle, or model
// switch) that can change a capability's provider or other config.
// Committing a built-in preset must round-trip its vision declaration
// (the provider-inheriting "inherit" route) byte-for-byte.
func TestPresetEditorVisionProviderIdentityIsCommitImmutable(t *testing.T) {
	for _, name := range []string{"codex", "openai", "anthropic"} {
		t.Run(name, func(t *testing.T) {
			p := builtinPresetForEditorTest(t, name)
			m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", true)
			if asString(m.llmMap()["model"]) == "" {
				m.llmMap()["model"] = "test-model"
			}

			_, cmd := m.commit()
			commit := cmd().(PresetEditorCommitMsg)
			caps := commit.Preset.Manifest["capabilities"].(map[string]interface{})
			vision := caps["vision"].(map[string]interface{})
			if !reflect.DeepEqual(vision, map[string]interface{}{"provider": "inherit"}) {
				t.Fatalf("commit changed vision declaration: got %#v, want provider inherit", vision)
			}
		})
	}
}

func TestPresetEditorSmallHeightKeepsSaveVisible(t *testing.T) {
	m := NewPresetEditorModelWithBuiltinFlag(testPresetEditorPreset(), "en", nil, "", false)
	var cmd tea.Cmd
	m, cmd = m.Update(tea.WindowSizeMsg{Width: 100, Height: 14})
	if cmd != nil {
		t.Fatalf("WindowSizeMsg returned unexpected cmd")
	}

	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	view := m.View()

	if !strings.Contains(view, "Save preset") {
		t.Fatalf("small editor view after End should contain save button; view:\n%s", view)
	}
	if got := renderedLineCount(view); got > 14 {
		t.Fatalf("small editor view after End must fit terminal height, got %d lines; view:\n%s", got, view)
	}
	if strings.Contains(view, "scroll-test") && strings.Contains(view, "good at testing") {
		t.Fatalf("expected top identity rows to scroll away when save is focused; view:\n%s", view)
	}
}

func TestPresetEditorTabJumpsToSaveInSmallHeight(t *testing.T) {
	m := NewPresetEditorModelWithBuiltinFlag(testPresetEditorPreset(), "en", nil, "", false)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 14})

	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	view := m.View()

	if !strings.Contains(view, "Save preset") {
		t.Fatalf("small editor view after Tab should contain save button; view:\n%s", view)
	}
	if got := renderedLineCount(view); got > 14 {
		t.Fatalf("small editor view after Tab must fit terminal height, got %d lines; view:\n%s", got, view)
	}
}

func TestPresetEditorShortTerminalDoesNotWrapRowsPastHeight(t *testing.T) {
	for _, size := range []struct {
		width  int
		height int
	}{
		{width: 50, height: 10},
		{width: 60, height: 12},
		{width: 80, height: 14},
		{width: 100, height: 16},
	} {
		m := NewPresetEditorModelWithBuiltinFlag(testPresetEditorPreset(), "en", nil, "", false)
		m, _ = m.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
		m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
		view := m.View()
		if !strings.Contains(view, "Save preset") {
			t.Fatalf("%dx%d view after End should contain save button; view:\n%s", size.width, size.height, view)
		}
		if got := renderedLineCount(view); got > size.height {
			t.Fatalf("%dx%d view must fit terminal height, got %d lines; view:\n%s", size.width, size.height, got, view)
		}
	}
}

// TestPresetEditorAPIKeyEditableWhenAlreadyStored verifies that opening the
// editor on a preset whose api_key_env slot already holds a value still allows
// an explicit replacement. The existing key is shown masked, Enter opens a
// blank paste target, and commit emits APIKeySet only after the user edits.
func TestPresetEditorAPIKeyEditableWhenAlreadyStored(t *testing.T) {
	keys := map[string]string{"EXAMPLE_API_KEY": "sk-existing-value"}
	p := testPresetEditorPreset()
	p.Source = preset.SourceSaved
	m := NewPresetEditorModel(p, "en", keys, "")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	if got := m.fieldString(feAPIKey); got == "" || got == "sk-existing-value" {
		t.Fatalf("expected existing key to render masked, got %q", got)
	}

	m.cursor = editorFieldOrderIndex(t, feAPIKey)
	if editorFieldOrder[m.cursor] != feAPIKey {
		t.Fatalf("expected cursor on feAPIKey, got %v", editorFieldOrder[m.cursor])
	}

	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if m.mode != emInline {
		t.Fatalf("expected emInline after Enter on api_key with stored key, got mode=%v", m.mode)
	}
	if got := m.input.Value(); got != "" {
		t.Fatalf("api_key replacement input should start blank for easy paste, got %q", got)
	}

	m.input.SetValue("sk-replacement-value")
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.apiKeySet || m.apiKey != "sk-replacement-value" {
		t.Fatalf("expected replacement key to be staged; apiKeySet=%v apiKey=%q", m.apiKeySet, m.apiKey)
	}
}

func TestPresetEditorAPIKeyUnchangedWhenStoredKeyUntouched(t *testing.T) {
	keys := map[string]string{"EXAMPLE_API_KEY": "sk-existing-value"}
	p := testPresetEditorPreset()
	p.Source = preset.SourceSaved
	m := NewPresetEditorModel(p, "en", keys, "")

	_, cmd := m.commit()
	if cmd == nil {
		t.Fatalf("commit returned nil cmd")
	}
	msg := cmd()
	commit, ok := msg.(PresetEditorCommitMsg)
	if !ok {
		t.Fatalf("commit cmd returned %T, want PresetEditorCommitMsg", msg)
	}
	if commit.APIKeySet {
		t.Fatalf("untouched stored API key should not be emitted as a replacement")
	}
}

func TestPresetEditorAPIKeyBlankEditKeepsStoredKey(t *testing.T) {
	keys := map[string]string{"EXAMPLE_API_KEY": "sk-existing-value"}
	p := testPresetEditorPreset()
	p.Source = preset.SourceSaved
	m := NewPresetEditorModel(p, "en", keys, "")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	m.cursor = editorFieldOrderIndex(t, feAPIKey)
	if editorFieldOrder[m.cursor] != feAPIKey {
		t.Fatalf("expected cursor on feAPIKey, got %v", editorFieldOrder[m.cursor])
	}

	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.mode != emInline {
		t.Fatalf("expected emInline after opening api_key row, got mode=%v", m.mode)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.apiKeySet {
		t.Fatalf("blank API key edit should be a no-op, not stage a clear")
	}

	_, cmd := m.commit()
	if cmd == nil {
		t.Fatalf("commit returned nil cmd")
	}
	commit, ok := cmd().(PresetEditorCommitMsg)
	if !ok {
		t.Fatalf("commit cmd returned non-commit msg")
	}
	if commit.APIKeySet {
		t.Fatalf("blank API key edit should not emit APIKeySet=true")
	}
}

func TestPresetEditorTemplateDoesNotInheritStoredProviderKey(t *testing.T) {
	keys := map[string]string{"EXAMPLE_API_KEY": "sk-existing-value"}
	p := testPresetEditorPreset()
	p.Source = preset.SourceTemplate
	m := NewPresetEditorModel(p, "en", keys, "")

	if m.apiKey != "" {
		t.Fatalf("template editor should not preload old provider key, apiKey=%q", m.apiKey)
	}
	if got := m.fieldString(feAPIKey); got == "sk-existing-value" {
		t.Fatalf("template editor should not render old provider key, got %q", got)
	}

	_, cmd := m.commit()
	if cmd == nil {
		t.Fatalf("commit returned nil cmd")
	}
	commit, ok := cmd().(PresetEditorCommitMsg)
	if !ok {
		t.Fatalf("commit cmd returned non-commit msg")
	}
	if commit.APIKeySet || commit.APIKey != "" {
		t.Fatalf("untouched template key should not emit old provider key; APIKeySet=%v APIKey=%q", commit.APIKeySet, commit.APIKey)
	}
}

// Every editor result is destined for preset.Save, which always writes under
// presets/saved/. The emitted runtime-only Source must therefore agree with
// that host write even when the committed name matches a built-in template.
func TestPresetEditorCommitPathsIdentifySavedSource(t *testing.T) {
	template := builtinPresetForEditorTest(t, "codex")
	template.Source = preset.SourceTemplate

	tests := []struct {
		name string
		cmd  func(t *testing.T) tea.Cmd
	}{
		{
			name: "normal save",
			cmd: func(t *testing.T) tea.Cmd {
				m := NewPresetEditorModelWithBuiltinFlag(template, "en", nil, "", false)
				_, cmd := m.commit()
				return cmd
			},
		},
		{
			name: "clone prompt enter",
			cmd: func(t *testing.T) tea.Cmd {
				m := NewPresetEditorModel(template, "en", nil, "")
				m.mode = emClonePrompt
				m.cloneNameInput.SetValue("codex-copy")
				_, cmd := m.updateClonePrompt(tea.KeyPressMsg{Code: tea.KeyEnter})
				return cmd
			},
		},
		{
			name: "expert built-in overwrite",
			cmd: func(t *testing.T) tea.Cmd {
				m := NewPresetEditorModel(template, "en", nil, "")
				m.mode = emClonePrompt
				_, cmd := m.updateClonePrompt(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
				return cmd
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := tt.cmd(t)
			if cmd == nil {
				t.Fatal("commit returned nil cmd")
			}
			commit, ok := cmd().(PresetEditorCommitMsg)
			if !ok {
				t.Fatalf("commit cmd returned a non-commit message")
			}
			if commit.Preset.Source != preset.SourceSaved {
				t.Fatalf("commit source = %v, want SourceSaved", commit.Preset.Source)
			}
			wantRef := "~/.lingtai-tui/presets/saved/" + commit.Preset.Name + ".json"
			if got := preset.RefFor(commit.Preset); got != wantRef {
				t.Fatalf("commit ref = %q, want %q", got, wantRef)
			}
		})
	}
}

// TestPresetEditorAPIKeyEditableWhenNoStoredKey verifies that a preset
// with no stored key (typical for first-run flow on a fresh template)
// allows inline edit so initial setup works.
func TestPresetEditorAPIKeyEditableWhenNoStoredKey(t *testing.T) {
	m := NewPresetEditorModelWithBuiltinFlag(testPresetEditorPreset(), "en", nil, "", false)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	m.cursor = editorFieldOrderIndex(t, feAPIKey)
	if editorFieldOrder[m.cursor] != feAPIKey {
		t.Fatalf("expected cursor on feAPIKey, got %v", editorFieldOrder[m.cursor])
	}

	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if m.mode != emInline {
		t.Fatalf("expected emInline after Enter on editable api_key, got mode=%v", m.mode)
	}
}

func TestPresetEditorCanonicalizesLegacyShellForDisplay(t *testing.T) {
	p := testPresetEditorPreset()
	caps := p.Manifest["capabilities"].(map[string]interface{})
	legacy := caps["shell"]
	delete(caps, "shell")
	caps["bash"] = legacy

	m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)
	workingCaps := m.working.Manifest["capabilities"].(map[string]interface{})
	if _, ok := workingCaps["bash"]; ok {
		t.Fatalf("editor retained legacy bash capability: %#v", workingCaps)
	}
	if got := workingCaps["shell"].(map[string]interface{})["yolo"]; got != true {
		t.Fatalf("editor lost legacy shell configuration: %#v", workingCaps["shell"])
	}
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 80})
	if !strings.Contains(m.View(), "shell") {
		t.Fatalf("editor view does not display canonical shell capability")
	}
}

// TestPresetEditorNoCapabilityFieldsInEditorFieldOrder is the regression
// test for removing the separate editable-capability concept. The prior
// editorField enum had feCapFile/feCapBash/feCapWebSearch/feCapAvatar/
// feCapDaemon/feCapVision entries; none of them exist anymore, so
// editorFieldOrder's fixed length below is a compile-time-checked proxy
// for "no capability slot was added back in". The Capabilities section
// is rendered entirely from formRows' fixed capabilityRows list, never
// from editorFieldOrder or the cursor.
func TestPresetEditorNoCapabilityFieldsInEditorFieldOrder(t *testing.T) {
	wantOrder := []editorField{
		feName, feSummary, feTier, feGains, feLoses,
		feProvider, feModel, feServiceTier, feCodexCredits, feThinking, feWireAPI, feResponsesTransport, feBaseURL, feAPIKey,
		feSave,
	}
	if !reflect.DeepEqual(editorFieldOrder, wantOrder) {
		t.Fatalf("editorFieldOrder = %#v, want %#v (no capability field should ever appear here)", editorFieldOrder, wantOrder)
	}
}

func TestPresetEditorCommitDoesNotInjectLegacyCoreCaps(t *testing.T) {
	p := testPresetEditorPreset()
	p.Manifest["capabilities"] = map[string]interface{}{
		"web_search": map[string]interface{}{"provider": "duckduckgo"},
	}
	m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)

	_, cmd := m.commit()
	if cmd == nil {
		t.Fatalf("commit returned nil cmd")
	}
	msg := cmd()
	commit, ok := msg.(PresetEditorCommitMsg)
	if !ok {
		t.Fatalf("commit cmd returned %T, want PresetEditorCommitMsg", msg)
	}
	caps, ok := commit.Preset.Manifest["capabilities"].(map[string]interface{})
	if !ok {
		t.Fatalf("committed capabilities missing/wrong type: %T", commit.Preset.Manifest["capabilities"])
	}
	for _, capName := range []string{"library", "skills", "file", "shell", "avatar", "daemon"} {
		if _, ok := caps[capName]; ok {
			t.Fatalf("commit injected core/legacy capability %q: %#v", capName, caps)
		}
	}
	if _, ok := caps["web_search"]; !ok {
		t.Fatalf("commit lost optional web_search capability: %#v", caps)
	}
}

// TestModelSwitchNeverTouchesCapabilities is the regression test for the
// always-included-capabilities change: switching the LLM model — via
// direct field edit (applyInline) or via cycling (cycleFocused) — and
// switching provider must never add, remove, or modify any capability.
func TestModelSwitchNeverTouchesCapabilities(t *testing.T) {
	skillsPaths := []interface{}{"../.library_shared", "~/.lingtai-tui/utilities"}
	p := testPresetEditorPreset()
	p.Manifest["capabilities"] = map[string]interface{}{
		"web_search": map[string]interface{}{"provider": "duckduckgo"},
		"vision":     map[string]interface{}{"provider": "inherit"},
		"skills":     map[string]interface{}{"paths": skillsPaths},
		"shell":      map[string]interface{}{"yolo": true},
	}
	m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)
	before := deepCopyCaps(t, m.working.Manifest["capabilities"])

	// Direct field edit (feModel + applyInline) must not touch capabilities.
	m.cursor = editorFieldOrderIndex(t, feModel)
	m.applyInline("some-text-only-model")
	assertCapsUnchanged(t, "applyInline model edit", m, before)

	// Cycling a curated model row must not touch capabilities either, in
	// either direction.
	m.llmMap()["provider"] = "codex"
	m.llmMap()["model"] = "gpt-5.6-sol"
	m.cycleFocused(+1)
	assertCapsUnchanged(t, "cycleFocused model forward", m, before)
	m.cycleFocused(-1)
	assertCapsUnchanged(t, "cycleFocused model back", m, before)

	// Switching provider (which can also reset the model) must not touch
	// capabilities.
	m.cursor = editorFieldOrderIndex(t, feProvider)
	for i := 0; i < len(editorProviders); i++ {
		m.cycleFocused(+1)
		assertCapsUnchanged(t, "provider switch", m, before)
	}
}

func deepCopyCaps(t *testing.T, caps interface{}) map[string]interface{} {
	t.Helper()
	m, ok := caps.(map[string]interface{})
	if !ok {
		t.Fatalf("capabilities missing/wrong type: %T", caps)
	}
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func assertCapsUnchanged(t *testing.T, step string, m PresetEditorModel, before map[string]interface{}) {
	t.Helper()
	after, ok := m.working.Manifest["capabilities"].(map[string]interface{})
	if !ok {
		t.Fatalf("%s: capabilities missing/wrong type: %T", step, m.working.Manifest["capabilities"])
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("%s: capabilities changed: got %#v, want %#v", step, after, before)
	}
}

func TestPresetEditorCodexServiceTierFastAndNormal(t *testing.T) {
	m := NewPresetEditorModelWithBuiltinFlag(testCodexPresetEditorPreset(nil), "en", nil, "", false)
	m.cursor = editorFieldOrderIndex(t, feServiceTier)

	if !m.fieldVisible(feServiceTier) {
		t.Fatalf("codex service tier row should be visible")
	}
	if got := m.fieldString(feServiceTier); got != "normal" {
		t.Fatalf("empty llm.service_tier displays %q, want normal", got)
	}

	m.cycleFocused(+1)
	llm := m.working.Manifest["llm"].(map[string]interface{})
	if got, _ := llm["service_tier"].(string); got != "fast" {
		t.Fatalf("cycling normal -> fast wrote service_tier=%#v, want fast", llm["service_tier"])
	}
	_, cmd := m.commit()
	commit := cmd().(PresetEditorCommitMsg)
	committedLLM := commit.Preset.Manifest["llm"].(map[string]interface{})
	if got, _ := committedLLM["service_tier"].(string); got != "fast" {
		t.Fatalf("committed fast service_tier=%#v, want fast", committedLLM["service_tier"])
	}

	m.cycleFocused(+1)
	if _, ok := llm["service_tier"]; ok {
		t.Fatalf("cycling fast -> normal should remove llm.service_tier; got %#v", llm["service_tier"])
	}
	_, cmd = m.commit()
	commit = cmd().(PresetEditorCommitMsg)
	committedLLM = commit.Preset.Manifest["llm"].(map[string]interface{})
	if _, ok := committedLLM["service_tier"]; ok {
		t.Fatalf("committed normal service tier should omit llm.service_tier; got %#v", committedLLM["service_tier"])
	}
}

func TestPresetEditorCodexServiceTierDisplayAndCommitNormalization(t *testing.T) {
	cases := []struct {
		name        string
		serviceTier interface{}
		wantDisplay string
		wantSaved   bool
	}{
		{name: "absent", serviceTier: nil, wantDisplay: "normal"},
		{name: "fast", serviceTier: "fast", wantDisplay: "fast", wantSaved: true},
		{name: "unknown", serviceTier: "flex", wantDisplay: "normal"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewPresetEditorModelWithBuiltinFlag(testCodexPresetEditorPreset(tc.serviceTier), "en", nil, "", false)
			if got := m.fieldString(feServiceTier); got != tc.wantDisplay {
				t.Fatalf("service tier display = %q, want %q", got, tc.wantDisplay)
			}
			_, cmd := m.commit()
			commit := cmd().(PresetEditorCommitMsg)
			llm := commit.Preset.Manifest["llm"].(map[string]interface{})
			got, saved := llm["service_tier"].(string)
			if saved != tc.wantSaved {
				t.Fatalf("committed service_tier saved=%v, want %v; value=%#v", saved, tc.wantSaved, llm["service_tier"])
			}
			if saved && got != "fast" {
				t.Fatalf("committed service_tier=%q, want fast", got)
			}
		})
	}
}

func TestPresetEditorCodexThinkingRowAndOptions(t *testing.T) {
	m := NewPresetEditorModelWithBuiltinFlag(testCodexPresetEditorPreset(nil), "en", nil, "", false)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 80})
	view := m.View()

	if !m.fieldVisible(feThinking) {
		t.Fatalf("codex reasoning effort row should be visible")
	}
	if got := m.fieldString(feThinking); got != "xhigh" {
		t.Fatalf("empty llm.thinking displays %q, want xhigh", got)
	}
	if !strings.Contains(view, "Reasoning effort") {
		t.Fatalf("codex editor should render Reasoning effort row; view:\n%s", view)
	}
	for _, effort := range codexThinkingOptions {
		if !strings.Contains(view, effort) {
			t.Fatalf("codex editor should render thinking option %q; view:\n%s", effort, view)
		}
	}
}

func TestPresetEditorCodexThinkingSelectionAndCommit(t *testing.T) {
	cases := []struct {
		effort    string
		wantSaved bool
	}{
		{effort: "low", wantSaved: true},
		{effort: "medium", wantSaved: true},
		{effort: "high", wantSaved: true},
		{effort: "xhigh", wantSaved: true},
	}

	for _, tc := range cases {
		t.Run(tc.effort, func(t *testing.T) {
			m := NewPresetEditorModelWithBuiltinFlag(testCodexPresetEditorPreset(nil), "en", nil, "", false)
			m.cursor = editorFieldOrderIndex(t, feThinking)
			m.setCodexThinking(tc.effort)
			if got := m.fieldString(feThinking); got != tc.effort {
				t.Fatalf("thinking display = %q, want %q", got, tc.effort)
			}

			_, cmd := m.commit()
			commit := cmd().(PresetEditorCommitMsg)
			llm := commit.Preset.Manifest["llm"].(map[string]interface{})
			got, saved := llm["thinking"].(string)
			if saved != tc.wantSaved {
				t.Fatalf("committed thinking saved=%v, want %v; value=%#v", saved, tc.wantSaved, llm["thinking"])
			}
			if saved && got != tc.effort {
				t.Fatalf("committed thinking=%q, want %q", got, tc.effort)
			}
		})
	}
}

func TestPresetEditorCodexThinkingDisplayAndCommitNormalization(t *testing.T) {
	cases := []struct {
		name        string
		thinking    interface{}
		wantDisplay string
		wantSaved   bool
		wantValue   string
	}{
		{name: "absent", thinking: nil, wantDisplay: "xhigh", wantSaved: true, wantValue: "xhigh"},
		{name: "explicit high", thinking: "high", wantDisplay: "high", wantSaved: true, wantValue: "high"},
		{name: "low", thinking: "low", wantDisplay: "low", wantSaved: true, wantValue: "low"},
		{name: "explicit xhigh", thinking: "xhigh", wantDisplay: "xhigh", wantSaved: true, wantValue: "xhigh"},
		{name: "unknown", thinking: "turbo", wantDisplay: "xhigh", wantSaved: true, wantValue: "xhigh"},
		{name: "wrong type", thinking: 12, wantDisplay: "xhigh", wantSaved: true, wantValue: "xhigh"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewPresetEditorModelWithBuiltinFlag(testCodexPresetEditorPresetWithThinking(nil, tc.thinking), "en", nil, "", false)
			if got := m.fieldString(feThinking); got != tc.wantDisplay {
				t.Fatalf("thinking display = %q, want %q", got, tc.wantDisplay)
			}
			_, cmd := m.commit()
			commit := cmd().(PresetEditorCommitMsg)
			llm := commit.Preset.Manifest["llm"].(map[string]interface{})
			got, saved := llm["thinking"].(string)
			if saved != tc.wantSaved {
				t.Fatalf("committed thinking saved=%v, want %v; value=%#v", saved, tc.wantSaved, llm["thinking"])
			}
			if saved && got != tc.wantValue {
				t.Fatalf("committed thinking=%q, want %q", got, tc.wantValue)
			}
		})
	}
}

// claude-code has a CLI-specific effort vocabulary the editor does not
// expose, and a legacy provider outside the four families has no thinking
// scope: the row stays hidden and a stale value is stripped on commit.
func TestPresetEditorThinkingHiddenAndRemovedForNonThinkingProvider(t *testing.T) {
	for _, provider := range []string{"claude-code", "legacy-vendor"} {
		t.Run(provider, func(t *testing.T) {
			m := NewPresetEditorModelWithBuiltinFlag(testPresetEditorPreset(), "en", nil, "", false)
			llm := m.working.Manifest["llm"].(map[string]interface{})
			llm["provider"] = provider
			llm["thinking"] = "low"
			m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 80})
			view := m.View()

			if m.fieldVisible(feThinking) {
				t.Fatalf("reasoning effort row should be hidden for a non-thinking provider")
			}
			if m.isCyclable(feThinking) {
				t.Fatalf("reasoning effort row should not be cyclable for a non-thinking provider")
			}
			if strings.Contains(view, "Reasoning effort") || strings.Contains(view, "llm.thinking") {
				t.Fatalf("non-thinking editor should not render thinking row; view:\n%s", view)
			}

			m.cursor = editorFieldOrderIndex(t, feThinking)
			m.normalizeCursor()
			if editorFieldOrder[m.cursor] == feThinking {
				t.Fatalf("cursor landed on hidden thinking field for non-thinking preset")
			}

			_, cmd := m.commit()
			commit := cmd().(PresetEditorCommitMsg)
			committedLLM := commit.Preset.Manifest["llm"].(map[string]interface{})
			if _, ok := committedLLM["thinking"]; ok {
				t.Fatalf("non-thinking commit should remove llm.thinking; got %#v", committedLLM["thinking"])
			}
		})
	}
}

func TestPresetEditorProviderSwitchClearsThinking(t *testing.T) {
	m := NewPresetEditorModelWithBuiltinFlag(testCodexPresetEditorPresetWithThinking(nil, "low"), "en", nil, "", false)
	m.cursor = editorFieldOrderIndex(t, feProvider)

	m.cycleFocused(+1) // codex -> claude-code in provider picker order.
	llm := m.working.Manifest["llm"].(map[string]interface{})
	if got := llm["provider"]; got != "claude-code" {
		t.Fatalf("provider after cycling from codex = %#v, want claude-code", got)
	}
	if _, ok := llm["thinking"]; ok {
		t.Fatalf("provider switch away from codex should remove llm.thinking; got %#v", llm["thinking"])
	}

	_, cmd := m.commit()
	commit := cmd().(PresetEditorCommitMsg)
	committedLLM := commit.Preset.Manifest["llm"].(map[string]interface{})
	if _, ok := committedLLM["thinking"]; ok {
		t.Fatalf("non-codex commit after provider switch should omit llm.thinking; got %#v", committedLLM["thinking"])
	}
}

// TestPresetEditorServiceTierOnlyForOpenAIAndCodex pins the four templates
// and the service-tier scope: normal/fast (fast is sent as priority) exists
// for the openai and codex families only. The anthropic and claude templates
// never show the row, and a stale value is dropped on commit.
func TestPresetEditorServiceTierOnlyForOpenAIAndCodex(t *testing.T) {
	wantNames := []string{"codex", "claude", "openai", "anthropic"}
	gotNames := make([]string, 0, len(preset.BuiltinPresets()))
	for _, p := range preset.BuiltinPresets() {
		gotNames = append(gotNames, p.Name)
	}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("BuiltinPresets() names = %#v, want %#v", gotNames, wantNames)
	}

	for _, tc := range []struct {
		name string
		want bool
	}{
		{"codex", true},
		{"openai", true},
		{"anthropic", false},
		{"claude", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewPresetEditorModelWithBuiltinFlag(builtinPresetForEditorTest(t, tc.name), "en", nil, "", false)
			// The openai/anthropic templates intentionally start with an empty
			// model; give this editor behavior test a valid user value.
			if asString(m.llmMap()["model"]) == "" {
				m.llmMap()["model"] = "test-model"
			}
			m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 80})
			view := m.View()

			if got := m.fieldVisible(feServiceTier); got != tc.want {
				t.Fatalf("service tier visible = %v for %s, want %v", got, tc.name, tc.want)
			}
			if got := m.isCyclable(feServiceTier); got != tc.want {
				t.Fatalf("service tier cyclable = %v for %s, want %v", got, tc.name, tc.want)
			}
			if got := strings.Contains(view, i18n.T("preset_editor.field_service_tier")); got != tc.want {
				t.Fatalf("view renders service tier = %v for %s, want %v; view:\n%s", got, tc.name, tc.want, view)
			}

			if !tc.want {
				m.llmMap()["service_tier"] = "fast"
				_, cmd := m.commit()
				committed := cmd().(PresetEditorCommitMsg).Preset.Manifest["llm"].(map[string]interface{})
				if _, ok := committed["service_tier"]; ok {
					t.Fatalf("%s commit must drop service_tier: %#v", tc.name, committed["service_tier"])
				}
				return
			}

			// The cursor reaches the row directly after the model row.
			m.cursor = editorFieldOrderIndex(t, feModel)
			m.moveCursor(+1)
			if editorFieldOrder[m.cursor] != feServiceTier {
				t.Fatalf("cursor after model = %v for %s, want feServiceTier", editorFieldOrder[m.cursor], tc.name)
			}
			if got := m.fieldString(feServiceTier); got != "normal" {
				t.Fatalf("missing service tier displays %q for %s, want normal", got, tc.name)
			}

			m.cycleFocused(+1)
			if got := m.llmMap()["service_tier"]; got != "fast" {
				t.Fatalf("normal -> fast stored %#v for %s, want string fast", got, tc.name)
			}
			_, cmd := m.commit()
			committed := cmd().(PresetEditorCommitMsg).Preset.Manifest["llm"].(map[string]interface{})
			if got := committed["service_tier"]; got != "fast" {
				t.Fatalf("fast commit stored %#v for %s, want string fast", got, tc.name)
			}

			m.cycleFocused(+1)
			if _, ok := m.llmMap()["service_tier"]; ok {
				t.Fatalf("fast -> normal kept service_tier for %s: %#v", tc.name, m.llmMap()["service_tier"])
			}

			// Unknown legacy values display as normal and are dropped on commit.
			m.llmMap()["service_tier"] = "provider-specific"
			if got := m.fieldString(feServiceTier); got != "normal" {
				t.Fatalf("unknown service tier displays %q for %s, want normal", got, tc.name)
			}
			_, cmd = m.commit()
			committed = cmd().(PresetEditorCommitMsg).Preset.Manifest["llm"].(map[string]interface{})
			if _, ok := committed["service_tier"]; ok {
				t.Fatalf("unknown service tier should be omitted for %s: %#v", tc.name, committed["service_tier"])
			}
		})
	}
}

// Switching between the two service-tier families keeps the selection;
// switching to a family without service tiers drops it immediately.
func TestPresetEditorProviderSwitchServiceTierScope(t *testing.T) {
	m := NewPresetEditorModelWithBuiltinFlag(testCodexPresetEditorPreset("fast"), "en", nil, "", false)
	m.cursor = editorFieldOrderIndex(t, feProvider)
	m.cycleFocused(-1) // codex -> anthropic in picker order.
	if got := m.llmMap()["provider"]; got != "anthropic" {
		t.Fatalf("provider after cycling back from codex = %#v, want anthropic", got)
	}
	if _, ok := m.llmMap()["service_tier"]; ok {
		t.Fatalf("anthropic must not keep service_tier; got %#v", m.llmMap()["service_tier"])
	}

	m = NewPresetEditorModelWithBuiltinFlag(testCodexPresetEditorPreset("fast"), "en", nil, "", false)
	m.switchProvider("codex", "openai")
	if got, _ := m.llmMap()["service_tier"].(string); got != "fast" {
		t.Fatalf("codex -> openai should keep fast; got %#v", m.llmMap()["service_tier"])
	}
	m.llmMap()["model"] = "gpt-test" // the curated Codex model was cleared
	_, cmd := m.commit()
	committed := cmd().(PresetEditorCommitMsg).Preset.Manifest["llm"].(map[string]interface{})
	if got := committed["service_tier"]; got != "fast" {
		t.Fatalf("openai commit service_tier = %#v, want fast", got)
	}
}

// TestPresetEditorViewShowsOneCapabilitiesSectionWithAllRows verifies the
// human-facing contract: a single "Capabilities" section (not "Always
// Included") lists every capability the runtime can grant an agent,
// including web_search and vision, alongside the kernel core floor.
func TestPresetEditorViewShowsOneCapabilitiesSectionWithAllRows(t *testing.T) {
	m := NewPresetEditorModelWithBuiltinFlag(testPresetEditorPreset(), "en", nil, "", false)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 80})
	view := m.View()

	if !strings.Contains(view, "Capabilities") {
		t.Fatalf("view missing renamed \"Capabilities\" section header; view:\n%s", view)
	}
	if strings.Contains(view, "Always Included") {
		t.Fatalf("view still shows the old \"Always Included\" section header; view:\n%s", view)
	}
	for _, capName := range []string{
		"knowledge", "skills", "shell", "avatar", "daemon", "mcp", "file",
		"web_search", "vision",
	} {
		if !strings.Contains(view, capName) {
			t.Fatalf("view missing always-included capability %q; view:\n%s", capName, view)
		}
	}
}

// TestPresetEditorViewShowsCapabilitiesGuidanceLine asserts the one-line
// explanation of how to customize capabilities (ask the agent to explain
// init.json, then edit init.json) is present in the rendered view, and
// that it is actually localized — not just falling back to English —
// in all three shipped locales.
func TestPresetEditorViewShowsCapabilitiesGuidanceLine(t *testing.T) {
	for _, tc := range []struct {
		lang string
		want string
	}{
		{lang: "en", want: "init.json"},
		{lang: "zh", want: "init.json"},
		{lang: "wen", want: "init.json"},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			i18n.SetLang(tc.lang)
			t.Cleanup(func() { i18n.SetLang("en") })

			m := NewPresetEditorModelWithBuiltinFlag(testPresetEditorPreset(), tc.lang, nil, "", false)
			m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 80})
			view := m.View()

			guidance := i18n.T("preset_editor.capabilities_guidance")
			if guidance == "" || guidance == "preset_editor.capabilities_guidance" {
				t.Fatalf("lang %q: capabilities_guidance key missing/untranslated", tc.lang)
			}
			if !strings.Contains(view, tc.want) {
				t.Fatalf("lang %q: view missing capabilities guidance mentioning %q; view:\n%s", tc.lang, tc.want, view)
			}
		})
	}
}

// TestPresetEditorWebSearchAndVisionViewHasNoCheckboxOrProviderNames
// asserts the web_search/vision rows in the live editor view render
// exactly like the other always-included tools (plain "[✓] name  desc"
// via mandatoryCapRow) with no radio strip of provider options — those
// are the fixed/default tool routes, not user choices. Scoped to just
// those two row lines, since provider names like "minimax"/"gemini"
// legitimately appear elsewhere in the view (the LLM provider/model
// rows and their radio strips).
func TestPresetEditorWebSearchAndVisionViewHasNoCheckboxOrProviderNames(t *testing.T) {
	p := testPresetEditorPreset()
	caps := p.Manifest["capabilities"].(map[string]interface{})
	caps["web_search"] = map[string]interface{}{"provider": "zhipu"}
	caps["vision"] = map[string]interface{}{"provider": "gemini"}

	m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 80})
	view := m.View()

	for _, capName := range []string{"web_search", "vision"} {
		line := findLineContaining(t, view, capName)
		if !strings.Contains(line, "[✓]") {
			t.Fatalf("%s row must render the informational [✓] marker; got: %q", capName, line)
		}
		if strings.Contains(line, "[ ]") {
			t.Fatalf("%s row must not render an unchecked/toggleable checkbox; got: %q", capName, line)
		}
		if strings.Contains(line, "●") || strings.Contains(line, "○") {
			t.Fatalf("%s row must not render a provider radio strip; got: %q", capName, line)
		}
		for _, providerName := range []string{"duckduckgo", "zhipu", "gemini", "inherit"} {
			if strings.Contains(line, providerName) {
				t.Fatalf("%s row must not display provider name %q; got: %q", capName, providerName, line)
			}
		}
	}
}

func findLineContaining(t *testing.T, view, substr string) string {
	t.Helper()
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, substr) {
			return line
		}
	}
	t.Fatalf("view has no line containing %q", substr)
	return ""
}

// TestPresetEditorNoKeypressAtAnyCursorPositionChangesCapabilities is the
// acceptance test for "no reachable checkbox/provider control can remove
// or change a capability on this page": walk every cursor position in the
// form and, at each one, try every input this page recognizes as a
// mutation trigger (Space, Enter, Left, Right). None of it may change
// manifest.capabilities.
func TestPresetEditorNoKeypressAtAnyCursorPositionChangesCapabilities(t *testing.T) {
	p := testPresetEditorPreset()
	before := deepCopyCaps(t, p.Manifest["capabilities"])

	m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 80})

	for i := 0; i < len(editorFieldOrder); i++ {
		m.cursor = i
		for _, key := range []tea.KeyPressMsg{
			{Text: " "},
			{Code: tea.KeyEnter},
			{Code: tea.KeyLeft},
			{Code: tea.KeyRight},
		} {
			trial := m
			trial, _ = trial.Update(key)
			assertCapsUnchanged(t, "keypress at cursor "+lbl(editorFieldOrder[i]), trial, before)
		}
	}
}

func lbl(f editorField) string {
	return fmt.Sprintf("field=%d", int(f))
}

// TestPresetEditorCommitPreservesExistingCapabilityValuesByteForValue
// ensures saving an existing preset with old init.json-style capability
// values (including a non-default web_search provider) round-trips them
// unchanged. The editor's field-list change must not normalize, rewrite,
// or migrate values it no longer exposes as editable UI.
func TestPresetEditorCommitPreservesExistingCapabilityValuesByteForValue(t *testing.T) {
	p := testPresetEditorPreset()
	caps := p.Manifest["capabilities"].(map[string]interface{})
	caps["web_search"] = map[string]interface{}{"provider": "zhipu"}
	caps["vision"] = map[string]interface{}{"provider": "gemini", "api_key_env": "GEMINI_API_KEY"}

	m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)

	_, cmd := m.commit()
	msg := cmd()
	commit, ok := msg.(PresetEditorCommitMsg)
	if !ok {
		t.Fatalf("commit cmd returned %T, want PresetEditorCommitMsg", msg)
	}
	gotCaps, ok := commit.Preset.Manifest["capabilities"].(map[string]interface{})
	if !ok {
		t.Fatalf("committed capabilities missing/wrong type: %T", commit.Preset.Manifest["capabilities"])
	}
	if !reflect.DeepEqual(gotCaps["web_search"], caps["web_search"]) {
		t.Fatalf("web_search capability changed on save: got %#v, want %#v", gotCaps["web_search"], caps["web_search"])
	}
	if !reflect.DeepEqual(gotCaps["vision"], caps["vision"]) {
		t.Fatalf("vision capability changed on save: got %#v, want %#v", gotCaps["vision"], caps["vision"])
	}
}

// TestPresetEditorSaveNotBlockedByCapabilityState confirms Save is
// unaffected by web_search/vision capability presence or absence —
// capability state must never block Save/Next. Save only performs local
// structural validation (Preset.Validate); it never blocks on a live
// provider/model check.
func TestPresetEditorSaveNotBlockedByCapabilityState(t *testing.T) {
	p := testPresetEditorPreset()
	caps := p.Manifest["capabilities"].(map[string]interface{})
	delete(caps, "web_search")
	delete(caps, "vision")

	m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)

	_, cmd := m.commit()
	if cmd == nil {
		t.Fatalf("commit returned nil cmd with no capabilities present and a valid model")
	}
	msg := cmd()
	if _, ok := msg.(PresetEditorCommitMsg); !ok {
		t.Fatalf("commit cmd returned %T, want PresetEditorCommitMsg (save must not be blocked by missing capabilities)", msg)
	}
}

// TestPresetEditorSaveDoesNotProbeAPIKeyProvider is the regression test
// for the reported (Jason, 2026-07-23) bug where every provider — Codex
// and API-key providers alike — was rejected by a save-time
// live-availability check even though the configured provider worked.
// Save must only run local structural validation (Preset.Validate) and must
// never make a live network call: base_url points at a closed local port, so
// any HTTP attempt would fail/hang and this test would time out or fail if
// commit() still probed.
func TestPresetEditorSaveDoesNotProbeAPIKeyProvider(t *testing.T) {
	unreachable := "http://127.0.0.1:1" // reserved port; connection refused instantly, never a real server
	for _, provider := range []string{"openai", "anthropic"} {
		t.Run(provider, func(t *testing.T) {
			p := preset.Preset{
				Name:        provider + "-test",
				Description: preset.PresetDescription{Summary: "API-key editor test preset"},
				Manifest: map[string]interface{}{
					"llm": map[string]interface{}{
						"provider":    provider,
						"model":       "test-model",
						"base_url":    unreachable,
						"api_key_env": "EXAMPLE_API_KEY",
					},
				},
			}
			m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)
			m.apiKey = "sk-test"

			updated, cmd := m.commit()
			if updated.saveErr != "" {
				t.Fatalf("save must not be blocked by a pending/failed availability check; saveErr=%q", updated.saveErr)
			}
			if cmd == nil {
				t.Fatalf("expected commit() to return the commit cmd immediately, not a pending validity-check cmd")
			}
			msg := cmd()
			commit, ok := msg.(PresetEditorCommitMsg)
			if !ok {
				t.Fatalf("commit cmd returned %T, want PresetEditorCommitMsg (save must succeed without a live provider probe)", msg)
			}
			if commit.Preset.Manifest["llm"].(map[string]interface{})["provider"] != provider {
				t.Fatalf("committed preset lost its provider: %#v", commit.Preset.Manifest["llm"])
			}
		})
	}
}

// TestPresetEditorSaveDoesNotProbeCodexProvider is the Codex half of the
// same regression: Codex presets (OAuth-based, no api_key_env) must also
// reach PresetEditorCommitMsg on the first Save with no pending/checking
// state and no live Responses-endpoint call.
func TestPresetEditorSaveDoesNotProbeCodexProvider(t *testing.T) {
	p := testCodexPresetEditorPreset(nil)
	llm := p.Manifest["llm"].(map[string]interface{})
	llm["base_url"] = "http://127.0.0.1:1" // reserved port; would fail/hang if ever dialed
	m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", true)

	updated, cmd := m.commit()
	if updated.saveErr != "" {
		t.Fatalf("save must not be blocked by a pending/failed availability check; saveErr=%q", updated.saveErr)
	}
	if cmd == nil {
		t.Fatalf("expected commit() to return the commit cmd immediately, not a pending validity-check cmd")
	}
	msg := cmd()
	commit, ok := msg.(PresetEditorCommitMsg)
	if !ok {
		t.Fatalf("commit cmd returned %T, want PresetEditorCommitMsg (save must succeed without a live Codex probe)", msg)
	}
	if commit.Preset.Manifest["llm"].(map[string]interface{})["provider"] != "codex" {
		t.Fatalf("committed preset lost its provider: %#v", commit.Preset.Manifest["llm"])
	}
}

func editorFieldOrderIndex(t *testing.T, want editorField) int {
	t.Helper()
	for i, got := range editorFieldOrder {
		if got == want {
			return i
		}
	}
	t.Fatalf("field %v missing from editorFieldOrder", want)
	return -1
}

func renderedLineCount(s string) int {
	if s == "" {
		return 0
	}
	return len(strings.Split(s, "\n"))
}

// ─────────────────────────────────────────────────────────────────────────────
// wire_api (wire-format selector for the openai family)
// ─────────────────────────────────────────────────────────────────────────────

func testOpenAIPresetEditorPreset() preset.Preset {
	return preset.Preset{
		Name:        "openai-test",
		Description: preset.PresetDescription{Summary: "OpenAI-compatible test preset"},
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{
				"provider":    "openai",
				"model":       "gpt-oss-test",
				"base_url":    "https://api.example.com/v1",
				"api_key_env": "EXAMPLE_API_KEY",
			},
			"capabilities": map[string]interface{}{},
		},
	}
}

func TestPresetEditorWireAPIVisibleForOpenAIFamily(t *testing.T) {
	m := NewPresetEditorModelWithBuiltinFlag(testOpenAIPresetEditorPreset(), "en", nil, "", false)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 80})
	view := m.View()

	if !m.fieldVisible(feWireAPI) {
		t.Fatalf("wire_api row should be visible for the openai family")
	}
	if !m.isCyclable(feWireAPI) {
		t.Fatalf("wire_api should be cyclable for the openai family")
	}
	if !strings.Contains(view, "wire_api") {
		t.Fatalf("openai editor should render wire_api row; view:\n%s", view)
	}
	for _, option := range wireAPIOptions {
		if !strings.Contains(view, option) {
			t.Fatalf("openai editor should render wire option %q; view:\n%s", option, view)
		}
	}
}

func TestPresetEditorWireAPIHiddenOutsideOpenAIFamily(t *testing.T) {
	for _, provider := range []string{"anthropic", "codex", "claude-code", "custom"} {
		t.Run(provider, func(t *testing.T) {
			p := testOpenAIPresetEditorPreset()
			llm := p.Manifest["llm"].(map[string]interface{})
			llm["provider"] = provider
			llm["wire_api"] = "responses"
			m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)
			m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 80})
			if m.fieldVisible(feWireAPI) || m.isCyclable(feWireAPI) {
				t.Fatalf("wire_api row should be hidden for %s", provider)
			}
			if strings.Contains(m.View(), "wire_api") {
				t.Fatalf("%s editor should not render wire_api row", provider)
			}
			_, cmd := m.commit()
			committed := cmd().(PresetEditorCommitMsg).Preset.Manifest["llm"].(map[string]interface{})
			if _, ok := committed["wire_api"]; ok {
				t.Fatalf("%s commit must drop wire_api; got %#v", provider, committed["wire_api"])
			}
		})
	}
}

func TestPresetEditorWireAPICursorSkipsHiddenField(t *testing.T) {
	// For a non-openai preset, cursor navigation must skip the hidden
	// feWireAPI (and feResponsesTransport) and advance to feBaseURL.
	p := testOpenAIPresetEditorPreset()
	p.Manifest["llm"].(map[string]interface{})["provider"] = "anthropic"
	m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)
	m.cursor = editorFieldOrderIndex(t, feThinking)
	m.moveCursor(+1)
	if editorFieldOrder[m.cursor] != feBaseURL {
		t.Fatalf("cursor after thinking = %v, want feBaseURL", editorFieldOrder[m.cursor])
	}
}

func TestPresetEditorWireAPIDefaultsToChatCompletions(t *testing.T) {
	for _, stored := range []interface{}{nil, "auto", "bogus"} {
		p := testOpenAIPresetEditorPreset()
		if stored != nil {
			p.Manifest["llm"].(map[string]interface{})["wire_api"] = stored
		}
		m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)
		if got := m.fieldString(feWireAPI); got != "chat_completions" {
			t.Fatalf("wire_api %#v displays %q, want chat_completions", stored, got)
		}
		// Commit writes the default explicitly so the manifest never depends
		// on a kernel-side default (and a legacy "auto" is not carried over).
		_, cmd := m.commit()
		committed := cmd().(PresetEditorCommitMsg).Preset.Manifest["llm"].(map[string]interface{})
		if got := committed["wire_api"]; got != "chat_completions" {
			t.Fatalf("wire_api %#v committed as %#v, want chat_completions", stored, got)
		}
	}
	if got := asString(builtinPresetForEditorTest(t, "openai").Manifest["llm"].(map[string]interface{})["wire_api"]); got != "chat_completions" {
		t.Fatalf("openai template wire_api = %q, want chat_completions", got)
	}
}

func TestPresetEditorWireAPICycling(t *testing.T) {
	m := NewPresetEditorModelWithBuiltinFlag(testOpenAIPresetEditorPreset(), "en", nil, "", false)
	m.cursor = editorFieldOrderIndex(t, feWireAPI)

	// chat_completions -> responses
	m.cycleFocused(+1)
	if got := m.fieldString(feWireAPI); got != "responses" {
		t.Fatalf("cycling chat_completions -> +1 = %q, want responses", got)
	}
	if got := m.llmMap()["wire_api"]; got != "responses" {
		t.Fatalf("wire_api should be persisted as responses, got %#v", got)
	}

	// responses -> chat_completions, written explicitly.
	m.cycleFocused(+1)
	if got := m.llmMap()["wire_api"]; got != "chat_completions" {
		t.Fatalf("cycling responses -> +1 stored %#v, want chat_completions", got)
	}

	// Reverse wraps the two options.
	m.cycleFocused(-1)
	if got := m.fieldString(feWireAPI); got != "responses" {
		t.Fatalf("cycling chat_completions -> -1 = %q, want responses", got)
	}
}

func TestPresetEditorWireAPICommitPersistsSelection(t *testing.T) {
	m := NewPresetEditorModelWithBuiltinFlag(testOpenAIPresetEditorPreset(), "en", nil, "", false)
	m.cursor = editorFieldOrderIndex(t, feWireAPI)

	m.cycleFocused(+1) // chat_completions -> responses
	_, cmd := m.commit()
	committedLLM := cmd().(PresetEditorCommitMsg).Preset.Manifest["llm"].(map[string]interface{})
	if got, _ := committedLLM["wire_api"].(string); got != "responses" {
		t.Fatalf("committed wire_api=%#v, want responses", committedLLM["wire_api"])
	}

	m.cycleFocused(+1) // responses -> chat_completions
	_, cmd = m.commit()
	committedLLM = cmd().(PresetEditorCommitMsg).Preset.Manifest["llm"].(map[string]interface{})
	if got, _ := committedLLM["wire_api"].(string); got != "chat_completions" {
		t.Fatalf("committed wire_api=%#v, want chat_completions", committedLLM["wire_api"])
	}
}

func TestPresetEditorWireAPICleanupOnProviderSwitch(t *testing.T) {
	p := testOpenAIPresetEditorPreset()
	llm := p.Manifest["llm"].(map[string]interface{})
	llm["wire_api"] = "responses"
	llm["responses_transport"] = "websocket"
	m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)

	m.cursor = editorFieldOrderIndex(t, feProvider)
	m.cycleFocused(+1) // openai -> anthropic
	for _, key := range []string{"wire_api", "responses_transport"} {
		if _, ok := m.llmMap()[key]; ok {
			t.Fatalf("leaving the openai family should remove %s immediately", key)
		}
	}

	_, cmd := m.commit()
	committedLLM := cmd().(PresetEditorCommitMsg).Preset.Manifest["llm"].(map[string]interface{})
	if _, ok := committedLLM["wire_api"]; ok {
		t.Fatalf("commit after provider switch should remove wire_api; got %#v", committedLLM["wire_api"])
	}
}

func TestPresetEditorWireAPIEnterCyclesAndPreservesLegacyFlags(t *testing.T) {
	p := testOpenAIPresetEditorPreset()
	llm := p.Manifest["llm"].(map[string]interface{})
	llm["use_responses_api"] = true
	llm["force_responses"] = true
	m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)
	m.cursor = editorFieldOrderIndex(t, feWireAPI)

	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := m.fieldString(feWireAPI); got != "responses" {
		t.Fatalf("Enter on wire_api = %q, want responses", got)
	}
	m.cycleFocused(+1) // responses -> chat_completions
	for _, key := range []string{"use_responses_api", "force_responses"} {
		if got := m.llmMap()[key]; got != true {
			t.Fatalf("wire selection must preserve unrelated %s=true, got %#v", key, got)
		}
	}
}

func TestPresetEditorResponsesTransportVisibleOnlyForOpenAIResponses(t *testing.T) {
	p := testOpenAIPresetEditorPreset()
	llm := p.Manifest["llm"].(map[string]interface{})
	llm["wire_api"] = "responses"
	m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 80})

	if !m.fieldVisible(feResponsesTransport) || !m.isCyclable(feResponsesTransport) {
		t.Fatal("Responses transport should be visible and cyclable for openai Responses")
	}
	if got := m.fieldString(feResponsesTransport); got != "http" {
		t.Fatalf("absent responses_transport displays %q, want http", got)
	}
	view := m.View()
	if !strings.Contains(view, "Transport") || !strings.Contains(view, "websocket") {
		t.Fatalf("openai Responses editor did not render the transport choices; view:\n%s", view)
	}

	delete(llm, "wire_api")
	m = NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)
	if m.fieldVisible(feResponsesTransport) || m.isCyclable(feResponsesTransport) {
		t.Fatal("Responses transport must be hidden when wire_api is not responses")
	}
}

func TestPresetEditorResponsesTransportCyclesAndCommits(t *testing.T) {
	p := testOpenAIPresetEditorPreset()
	llm := p.Manifest["llm"].(map[string]interface{})
	llm["wire_api"] = "responses"
	m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)
	m.cursor = editorFieldOrderIndex(t, feResponsesTransport)

	// Enter uses the same enum path as Right: HTTP (omitted) -> WebSocket.
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := m.fieldString(feResponsesTransport); got != "websocket" {
		t.Fatalf("transport cycle = %q, want websocket", got)
	}
	if got := m.llmMap()["responses_transport"]; got != "websocket" {
		t.Fatalf("responses_transport not persisted: %#v", got)
	}

	_, cmd := m.commit()
	committed := cmd().(PresetEditorCommitMsg).Preset.Manifest["llm"].(map[string]interface{})
	if got := committed["responses_transport"]; got != "websocket" {
		t.Fatalf("committed responses_transport = %#v, want websocket", got)
	}

	// WebSocket -> HTTP removes the key because HTTP is the default.
	m.cycleFocused(+1)
	if got := m.fieldString(feResponsesTransport); got != "http" {
		t.Fatalf("transport cycle back = %q, want http", got)
	}
	if _, ok := m.llmMap()["responses_transport"]; ok {
		t.Fatal("HTTP transport should be represented by an omitted manifest key")
	}
}

func TestPresetEditorResponsesTransportCleansWhenScopeEnds(t *testing.T) {
	for _, tc := range []struct {
		name  string
		field editorField
	}{
		{name: "provider", field: feProvider},
		{name: "wire api", field: feWireAPI},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := testOpenAIPresetEditorPreset()
			llm := p.Manifest["llm"].(map[string]interface{})
			llm["wire_api"] = "responses"
			llm["responses_transport"] = "websocket"
			m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)

			m.cursor = editorFieldOrderIndex(t, tc.field)
			m.cycleFocused(+1)
			if _, ok := m.llmMap()["responses_transport"]; ok {
				t.Fatal("leaving openai Responses scope must remove responses_transport")
			}
			if m.fieldVisible(feResponsesTransport) {
				t.Fatal("transport must be hidden after leaving openai Responses scope")
			}
		})
	}

	// Commit is a second fail-closed boundary for stale or explicit HTTP values.
	p := testOpenAIPresetEditorPreset()
	llm := p.Manifest["llm"].(map[string]interface{})
	llm["wire_api"] = "responses"
	llm["responses_transport"] = "http"
	m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)
	_, cmd := m.commit()
	committed := cmd().(PresetEditorCommitMsg).Preset.Manifest["llm"].(map[string]interface{})
	if _, ok := committed["responses_transport"]; ok {
		t.Fatal("commit must omit explicit HTTP responses_transport")
	}
}

func TestPresetEditorOpenAIResponsesThinkingShowsDefaultAndAllEfforts(t *testing.T) {
	p := testOpenAIPresetEditorPreset()
	llm := p.Manifest["llm"].(map[string]interface{})
	llm["wire_api"] = "responses"
	m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 80})

	if !m.fieldVisible(feThinking) || !m.isCyclable(feThinking) {
		t.Fatal("thinking should be visible and cyclable for openai Responses")
	}
	if got := m.fieldString(feThinking); got != "default" {
		t.Fatalf("absent custom thinking displays %q, want default", got)
	}
	if got := m.thinkingOptions(); !reflect.DeepEqual(got, levelThinkingOptions) {
		t.Fatalf("custom thinking options = %#v, want %#v", got, levelThinkingOptions)
	}
	view := m.View()
	for _, effort := range levelThinkingOptions {
		if !strings.Contains(view, effort) {
			t.Fatalf("custom thinking picker does not render %q; view:\n%s", effort, view)
		}
	}
}

func TestPresetEditorOpenAIResponsesThinkingCyclesAndOmitsDefault(t *testing.T) {
	p := testOpenAIPresetEditorPreset()
	llm := p.Manifest["llm"].(map[string]interface{})
	llm["wire_api"] = "responses"
	m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)
	m.cursor = editorFieldOrderIndex(t, feThinking)

	for _, want := range levelThinkingOptions[1:] {
		m.cycleFocused(+1)
		if got := m.fieldString(feThinking); got != want {
			t.Fatalf("custom thinking cycle = %q, want %q", got, want)
		}
		if got := m.llmMap()["thinking"]; got != want {
			t.Fatalf("manifest thinking = %#v, want %q", got, want)
		}
	}

	// xhigh wraps to default, represented by an omitted field.
	m.cycleFocused(+1)
	if got := m.fieldString(feThinking); got != "default" {
		t.Fatalf("custom thinking wrap = %q, want default", got)
	}
	if _, ok := m.llmMap()["thinking"]; ok {
		t.Fatal("custom thinking default must omit the manifest field")
	}

	_, cmd := m.commit()
	committed := cmd().(PresetEditorCommitMsg).Preset.Manifest["llm"].(map[string]interface{})
	if _, ok := committed["thinking"]; ok {
		t.Fatal("committed custom default must omit manifest.llm.thinking")
	}
}

func TestPresetEditorThinkingSurvivesWireAPIChange(t *testing.T) {
	p := testOpenAIPresetEditorPreset()
	llm := p.Manifest["llm"].(map[string]interface{})
	llm["wire_api"] = "responses"
	llm["thinking"] = "high"
	m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)

	m.cursor = editorFieldOrderIndex(t, feWireAPI)
	m.cycleFocused(+1) // responses -> chat_completions
	if got := m.fieldString(feWireAPI); got != "chat_completions" {
		t.Fatalf("wire_api after cycling = %q, want chat_completions", got)
	}
	if got := m.llmMap()["thinking"]; got != "high" {
		t.Fatalf("thinking after wire_api change = %#v, want high", got)
	}
	if !m.fieldVisible(feThinking) || !m.isCyclable(feThinking) {
		t.Fatal("thinking must stay visible and cyclable on either openai wire")
	}
}

// testLevelThinkingPreset builds a minimal preset for a provider. A nil
// thinking omits the field.
func testLevelThinkingPreset(provider string, thinking interface{}) preset.Preset {
	llm := map[string]interface{}{
		"provider":    provider,
		"model":       "test-model",
		"base_url":    "https://api.example.com/v1",
		"api_key_env": "TEST_API_KEY",
	}
	if thinking != nil {
		llm["thinking"] = thinking
	}
	return preset.Preset{
		Name:        provider + "-thinking-test",
		Description: preset.PresetDescription{Summary: "Thinking-capable editor test preset"},
		Manifest: map[string]interface{}{
			"llm":          llm,
			"capabilities": map[string]interface{}{},
		},
	}
}

func TestPresetEditorThinkingRowVisibleForThinkingCapableProviders(t *testing.T) {
	cases := []struct {
		name      string
		provider  string
		wantShown bool
	}{
		{name: "anthropic", provider: "anthropic", wantShown: true},
		// No wire_api in this manifest: the row must not depend on an
		// explicit Responses selection.
		{name: "openai", provider: "openai", wantShown: true},
		{name: "claude code", provider: "claude-code", wantShown: false},
		{name: "legacy custom", provider: "custom", wantShown: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewPresetEditorModelWithBuiltinFlag(testLevelThinkingPreset(tc.provider, nil), "en", nil, "", false)
			m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 80})

			if got := m.fieldVisible(feThinking); got != tc.wantShown {
				t.Fatalf("fieldVisible(feThinking) = %v, want %v", got, tc.wantShown)
			}
			if got := m.isCyclable(feThinking); got != tc.wantShown {
				t.Fatalf("isCyclable(feThinking) = %v, want %v", got, tc.wantShown)
			}
			view := m.View()
			if got := strings.Contains(view, "Reasoning effort"); got != tc.wantShown {
				t.Fatalf("view renders Reasoning effort = %v, want %v; view:\n%s", got, tc.wantShown, view)
			}
			if !tc.wantShown {
				if got := m.thinkingValue(); got != "" {
					t.Fatalf("thinkingValue() = %q, want empty for a non-thinking provider", got)
				}
				if got := m.thinkingOptions(); got != nil {
					t.Fatalf("thinkingOptions() = %#v, want nil", got)
				}
				return
			}
			if got := m.thinkingValue(); got != "default" {
				t.Fatalf("absent thinking displays %q, want default", got)
			}
			if got := m.thinkingOptions(); !reflect.DeepEqual(got, levelThinkingOptions) {
				t.Fatalf("thinking options = %#v, want %#v", got, levelThinkingOptions)
			}
			for _, effort := range levelThinkingOptions {
				if !strings.Contains(view, effort) {
					t.Fatalf("thinking picker does not render %q; view:\n%s", effort, view)
				}
			}
		})
	}
}

func TestPresetEditorLevelThinkingCyclesAndCommits(t *testing.T) {
	for _, provider := range []string{"anthropic", "openai"} {
		t.Run(provider, func(t *testing.T) {
			m := NewPresetEditorModelWithBuiltinFlag(testLevelThinkingPreset(provider, nil), "en", nil, "", false)
			m.cursor = editorFieldOrderIndex(t, feThinking)

			for _, want := range levelThinkingOptions[1:] {
				m.cycleFocused(+1)
				if got := m.fieldString(feThinking); got != want {
					t.Fatalf("thinking cycle = %q, want %q", got, want)
				}
				if got := m.llmMap()["thinking"]; got != want {
					t.Fatalf("manifest thinking = %#v, want %q", got, want)
				}
			}

			// xhigh wraps back to default, represented by an omitted field.
			m.cycleFocused(+1)
			if got := m.fieldString(feThinking); got != "default" {
				t.Fatalf("thinking wrap = %q, want default", got)
			}
			if _, ok := m.llmMap()["thinking"]; ok {
				t.Fatal("default must omit manifest.llm.thinking")
			}

			_, cmd := m.commit()
			committed := cmd().(PresetEditorCommitMsg).Preset.Manifest["llm"].(map[string]interface{})
			if _, ok := committed["thinking"]; ok {
				t.Fatal("committed default must omit manifest.llm.thinking")
			}
		})
	}
}

func TestPresetEditorLevelThinkingSetPersistsThroughCommit(t *testing.T) {
	cases := []struct {
		name      string
		provider  string
		set       string
		wantSaved bool
		wantValue string
	}{
		{name: "anthropic high", provider: "anthropic", set: "high", wantSaved: true, wantValue: "high"},
		{name: "anthropic none", provider: "anthropic", set: "none", wantSaved: true, wantValue: "none"},
		{name: "anthropic default", provider: "anthropic", set: "default"},
		{name: "anthropic invalid", provider: "anthropic", set: "turbo"},
		{name: "openai xhigh", provider: "openai", set: "xhigh", wantSaved: true, wantValue: "xhigh"},
		{name: "openai default", provider: "openai", set: "default"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewPresetEditorModelWithBuiltinFlag(testLevelThinkingPreset(tc.provider, "medium"), "en", nil, "", false)
			m.setThinking(tc.set)

			wantDisplay := tc.wantValue
			if !tc.wantSaved {
				wantDisplay = "default"
			}
			if got := m.fieldString(feThinking); got != wantDisplay {
				t.Fatalf("thinking display = %q, want %q", got, wantDisplay)
			}

			_, cmd := m.commit()
			committed := cmd().(PresetEditorCommitMsg).Preset.Manifest["llm"].(map[string]interface{})
			got, saved := committed["thinking"].(string)
			if saved != tc.wantSaved {
				t.Fatalf("committed thinking saved=%v, want %v; value=%#v", saved, tc.wantSaved, committed["thinking"])
			}
			if saved && got != tc.wantValue {
				t.Fatalf("committed thinking = %q, want %q", got, tc.wantValue)
			}
		})
	}
}

// An invalid stored value is normalized away for level providers (omitted ==
// the kernel default) while Codex keeps its explicit xhigh default.
func TestNormalizeThinkingByProviderScope(t *testing.T) {
	cases := []struct {
		name      string
		llm       map[string]interface{}
		wantSaved bool
		wantValue string
	}{
		{
			name:      "anthropic invalid dropped",
			llm:       map[string]interface{}{"provider": "anthropic", "thinking": "turbo"},
			wantSaved: false,
		},
		{
			name:      "anthropic wrong type dropped",
			llm:       map[string]interface{}{"provider": "anthropic", "thinking": 12},
			wantSaved: false,
		},
		{
			name:      "anthropic valid kept",
			llm:       map[string]interface{}{"provider": "anthropic", "thinking": "medium"},
			wantSaved: true,
			wantValue: "medium",
		},
		{
			name:      "anthropic absent stays absent",
			llm:       map[string]interface{}{"provider": "anthropic"},
			wantSaved: false,
		},
		{
			name:      "openai invalid dropped",
			llm:       map[string]interface{}{"provider": "openai", "thinking": "turbo"},
			wantSaved: false,
		},
		{
			name:      "openai valid kept without wire_api",
			llm:       map[string]interface{}{"provider": "openai", "thinking": "minimal"},
			wantSaved: true,
			wantValue: "minimal",
		},
		{
			name:      "claude-code dropped",
			llm:       map[string]interface{}{"provider": "claude-code", "thinking": "high"},
			wantSaved: false,
		},
		{
			name:      "legacy api_compat no longer grants scope",
			llm:       map[string]interface{}{"provider": "custom", "api_compat": "openai", "thinking": "high"},
			wantSaved: false,
		},
		{
			name:      "codex invalid falls back to xhigh",
			llm:       map[string]interface{}{"provider": "codex", "thinking": "turbo"},
			wantSaved: true,
			wantValue: "xhigh",
		},
		{
			name:      "codex absent gains xhigh",
			llm:       map[string]interface{}{"provider": "codex"},
			wantSaved: true,
			wantValue: "xhigh",
		},
		{
			name:      "codex valid kept",
			llm:       map[string]interface{}{"provider": "codex", "thinking": "low"},
			wantSaved: true,
			wantValue: "low",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manifest := map[string]interface{}{"llm": tc.llm}
			normalizeThinking(manifest)
			got, saved := tc.llm["thinking"].(string)
			if saved != tc.wantSaved {
				t.Fatalf("normalized thinking saved=%v, want %v; value=%#v", saved, tc.wantSaved, tc.llm["thinking"])
			}
			if saved && got != tc.wantValue {
				t.Fatalf("normalized thinking = %q, want %q", got, tc.wantValue)
			}
		})
	}
}

func TestPresetEditorCodexThinkingOptionsRemainUnchanged(t *testing.T) {
	m := NewPresetEditorModelWithBuiltinFlag(testCodexPresetEditorPreset(nil), "en", nil, "", false)

	if got := m.thinkingValue(); got != "xhigh" {
		t.Fatalf("Codex default thinking = %q, want xhigh", got)
	}
	if got := m.thinkingOptions(); !reflect.DeepEqual(got, codexThinkingOptions) {
		t.Fatalf("Codex thinking options = %#v, want %#v", got, codexThinkingOptions)
	}
}

func TestPresetEditorCodexSingleAPIKeyDisplayKeepsBoundAccount(t *testing.T) {
	globalDir := t.TempDir()
	writeStubCodexToken(t, legacyCodexAuthPath(globalDir), "bound@example.test")
	m := NewPresetEditorModel(testCodexPresetEditorPreset(nil), "en", nil, globalDir)
	if got := m.fieldString(feAPIKey); got != "✓ bound@example.test" {
		t.Fatalf("CodexSingle API-key row = %q, want bound-account display", got)
	}
}

func TestPresetEditorCredentialFamilyGates(t *testing.T) {
	tests := []struct {
		provider       string
		account        bool
		thinking       bool
		serviceTierKey bool
	}{
		{"codex", true, true, true},
		{"codex_oauth", true, true, true},
		{"codex-pool", false, false, true},
		{"codex_pool", false, false, true},
		{"claude-code", false, false, true},
		{"claude_code", false, false, true},
		{"claude-agent-sdk", false, false, true},
		{"claude_agent_sdk", false, false, true},
	}
	for _, tc := range tests {
		t.Run(tc.provider, func(t *testing.T) {
			p := testCodexPresetEditorPreset(nil)
			llm := p.Manifest["llm"].(map[string]interface{})
			llm["provider"] = tc.provider
			m := NewPresetEditorModel(p, "en", nil, t.TempDir())
			if got := m.isCodexProvider(); got != tc.account {
				t.Fatalf("isCodexProvider() = %v, want %v", got, tc.account)
			}
			if got := m.hasCodexThinking(); got != tc.thinking {
				t.Fatalf("hasCodexThinking() = %v, want %v", got, tc.thinking)
			}
			m.setServiceTier("fast")
			workingLLM := m.llmMap()
			_, hasTier := workingLLM["service_tier"]
			if hasTier != tc.serviceTierKey {
				t.Fatalf("service_tier presence = %v, want %v", hasTier, tc.serviceTierKey)
			}
			m.setCodexAuthRef("codex-auth/account.json")
			_, hasRef := workingLLM["codex_auth_path"]
			if hasRef != tc.account {
				t.Fatalf("codex_auth_path presence = %v, want %v", hasRef, tc.account)
			}
		})
	}
}

func TestPresetEditorCredentialFamilyAPIKeyRowsAreReadOnly(t *testing.T) {
	tests := []struct {
		name       string
		provider   string
		readOnly   bool
		messageKey string
	}{
		{name: "codex", provider: "codex", readOnly: true, messageKey: "preset_editor.api_key_codex_readonly"},
		{name: "codex oauth alias", provider: "codex_oauth", readOnly: true, messageKey: "preset_editor.api_key_codex_readonly"},
		{name: "claude cli", provider: "claude-code", readOnly: true, messageKey: "preset_editor.api_key_managed_externally"},
		{name: "claude cli alias", provider: "claude_code", readOnly: true, messageKey: "preset_editor.api_key_managed_externally"},
		{name: "claude agent sdk", provider: "claude-agent-sdk", readOnly: true, messageKey: "preset_editor.api_key_managed_externally"},
		{name: "claude agent sdk alias", provider: "claude_agent_sdk", readOnly: true, messageKey: "preset_editor.api_key_managed_externally"},
		{name: "openai", provider: "openai", readOnly: false},
		{name: "anthropic", provider: "anthropic", readOnly: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := testPresetEditorPreset()
			llm := p.Manifest["llm"].(map[string]interface{})
			llm["provider"] = tt.provider
			llm["api_key_env"] = "TEST_API_KEY"
			m := NewPresetEditorModelWithBuiltinFlag(p, "en", map[string]string{"TEST_API_KEY": "existing"}, "", false)
			if tt.readOnly {
				wantDisplay := i18n.T(tt.messageKey)
				if tt.provider == "codex" || tt.provider == "codex_oauth" {
					wantDisplay = i18n.T("codex.oauth_not_logged_in")
				}
				if got := m.fieldString(feAPIKey); got != wantDisplay {
					t.Fatalf("API-key display = %q, want managed/read-only label %q", got, wantDisplay)
				}
			} else if got := m.fieldString(feAPIKey); got == "" || got == "existing" {
				t.Fatalf("Other API-key display = %q, want a masked existing key", got)
			}
			m.cursor = int(feAPIKey)
			m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			if tt.readOnly {
				if m.mode != emBrowse {
					t.Fatalf("mode after Enter = %v, want browse", m.mode)
				}
				if m.apiKeySet {
					t.Fatal("read-only credential row set apiKeySet")
				}
				if got := m.saveErr; got != i18n.T(tt.messageKey) {
					t.Fatalf("hint = %q, want %q", got, i18n.T(tt.messageKey))
				}
				return
			}
			if m.mode != emInline {
				t.Fatalf("Other API-key row mode after Enter = %v, want inline", m.mode)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// four provider families: provider cycle, switching, base_url
// ─────────────────────────────────────────────────────────────────────────────

func TestPresetEditorProviderCycleIsFourFamilies(t *testing.T) {
	if want := []string{"openai", "anthropic", "codex", "claude-code"}; !reflect.DeepEqual(editorProviders, want) {
		t.Fatalf("editorProviders = %#v, want %#v", editorProviders, want)
	}
	m := NewPresetEditorModelWithBuiltinFlag(testOpenAIPresetEditorPreset(), "en", nil, "", false)
	m.cursor = editorFieldOrderIndex(t, feProvider)
	for _, want := range []string{"anthropic", "codex", "claude-code", "openai"} {
		m.cycleFocused(+1)
		if got := m.fieldString(feProvider); got != want {
			t.Fatalf("provider cycle = %q, want %q", got, want)
		}
	}
	for _, want := range []string{"claude-code", "codex", "anthropic", "openai"} {
		m.cycleFocused(-1)
		if got := m.fieldString(feProvider); got != want {
			t.Fatalf("reverse provider cycle = %q, want %q", got, want)
		}
	}

	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 80})
	line := findLineContaining(t, m.View(), i18n.T("preset_editor.field_provider"))
	for _, provider := range editorProviders {
		if !strings.Contains(line, provider) {
			t.Fatalf("provider row should list %q; got %q", provider, line)
		}
	}
}

// TestPresetEditorProviderSwitchResetsRouteState walks one preset through
// every family and checks that nothing route-specific leaks across.
func TestPresetEditorProviderSwitchResetsRouteState(t *testing.T) {
	p := testCodexPresetEditorPresetWithThinking("fast", "high")
	p.Manifest["llm"].(map[string]interface{})["codex_auth_path"] = "codex-auth/work.json"
	m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)
	llm := m.llmMap()

	// codex -> openai: official endpoint, default key slot, curated Codex
	// model cleared (Save then requires an explicit model), OAuth binding
	// dropped, explicit Chat Completions wire.
	m.switchProvider("codex", "openai")
	if v, ok := llm["base_url"]; !ok || v != nil {
		t.Fatalf("codex -> openai base_url = %#v (present=%v), want nil (official)", v, ok)
	}
	if got := llm["api_key_env"]; got != "OPENAI_API_KEY" {
		t.Fatalf("codex -> openai api_key_env = %#v, want OPENAI_API_KEY", got)
	}
	if got := llm["model"]; got != "" {
		t.Fatalf("codex -> openai model = %#v, want cleared", got)
	}
	if _, ok := llm["codex_auth_path"]; ok {
		t.Fatalf("codex_auth_path must not survive leaving codex")
	}
	if _, ok := llm["thinking"]; ok {
		t.Fatalf("thinking must restart from the new family's default")
	}
	if got := llm["wire_api"]; got != "chat_completions" {
		t.Fatalf("openai wire_api = %#v, want chat_completions", got)
	}
	if _, cmd := m.commit(); cmd != nil {
		t.Fatalf("an openai preset with no model must not save")
	}

	// openai -> anthropic keeps the user's endpoint and model; the previous
	// family's default key slot is swapped for the new family's default.
	llm["base_url"] = "https://gateway.example.com/anthropic"
	llm["model"] = "vendor-model"
	m.switchProvider("openai", "anthropic")
	if got := llm["base_url"]; got != "https://gateway.example.com/anthropic" {
		t.Fatalf("openai -> anthropic base_url = %#v, want the user's endpoint", got)
	}
	if got := llm["api_key_env"]; got != "ANTHROPIC_API_KEY" {
		t.Fatalf("openai -> anthropic api_key_env = %#v, want ANTHROPIC_API_KEY", got)
	}
	if got := llm["model"]; got != "vendor-model" {
		t.Fatalf("a typed model must survive openai -> anthropic, got %#v", got)
	}
	if _, ok := llm["wire_api"]; ok {
		t.Fatalf("anthropic must not carry wire_api")
	}

	// A user-specific key slot survives a switch between API-key families.
	llm["api_key_env"] = "GATEWAY_1_API_KEY"
	m.switchProvider("anthropic", "openai")
	if got := llm["api_key_env"]; got != "GATEWAY_1_API_KEY" {
		t.Fatalf("user key slot = %#v, want GATEWAY_1_API_KEY kept", got)
	}

	// -> codex adopts the /codex route and its catalog default.
	m.switchProvider("openai", "codex")
	if got := llm["base_url"]; got != "https://chatgpt.com/backend-api/codex" {
		t.Fatalf("-> codex base_url = %#v, want the Codex route", got)
	}
	if got := llm["api_key_env"]; got != "" {
		t.Fatalf("-> codex api_key_env = %#v, want empty", got)
	}
	if got := llm["model"]; got != "gpt-5.6-sol" {
		t.Fatalf("-> codex model = %#v, want gpt-5.6-sol", got)
	}
	if got := llm["thinking"]; got != "xhigh" {
		t.Fatalf("-> codex thinking = %#v, want xhigh default", got)
	}

	// -> claude-code drops base_url and adopts its alias catalog.
	m.switchProvider("codex", "claude-code")
	if _, ok := llm["base_url"]; ok {
		t.Fatalf("claude-code must not carry base_url, got %#v", llm["base_url"])
	}
	if got := llm["model"]; got != "opus" {
		t.Fatalf("-> claude-code model = %#v, want opus", got)
	}
	if _, ok := llm["service_tier"]; ok {
		t.Fatalf("claude-code must not carry service_tier")
	}
}

// A saved preset from before the four-family model (e.g. provider "custom"
// with api_compat) converts to openai with → while keeping its endpoint,
// model, and key slot; the retired api_compat is dropped on commit.
func TestPresetEditorLegacyProviderConvertsToOpenAI(t *testing.T) {
	p := preset.Preset{
		Name:        "my-relay",
		Description: preset.PresetDescription{Summary: "legacy relay"},
		Source:      preset.SourceSaved,
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{
				"provider":    "custom",
				"api_compat":  "openai",
				"wire_api":    "auto",
				"model":       "relay-model",
				"base_url":    "https://relay.example.com/v1",
				"api_key_env": "LLM_API_KEY",
			},
		},
	}
	m := NewPresetEditorModel(p, "en", nil, "")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 80})
	if line := findLineContaining(t, m.View(), i18n.T("preset_editor.field_provider")); !strings.Contains(line, "custom") {
		t.Fatalf("a legacy provider must stay visible on the provider row; got %q", line)
	}
	m.cursor = editorFieldOrderIndex(t, feProvider)
	m.cycleFocused(+1)
	llm := m.llmMap()
	if got := llm["provider"]; got != "openai" {
		t.Fatalf("legacy provider + → = %#v, want openai", got)
	}
	for key, want := range map[string]string{
		"base_url":    "https://relay.example.com/v1",
		"model":       "relay-model",
		"api_key_env": "LLM_API_KEY",
		"wire_api":    "chat_completions",
	} {
		if got := llm[key]; got != want {
			t.Fatalf("converted %s = %#v, want %q", key, got, want)
		}
	}

	_, cmd := m.commit()
	committed := cmd().(PresetEditorCommitMsg).Preset.Manifest["llm"].(map[string]interface{})
	if _, ok := committed["api_compat"]; ok {
		t.Fatalf("commit must drop the retired api_compat field: %#v", committed)
	}
}

func TestPresetEditorCommitNeverWritesAPICompat(t *testing.T) {
	p := testOpenAIPresetEditorPreset()
	p.Manifest["llm"].(map[string]interface{})["api_compat"] = "openai"
	m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 80})
	if strings.Contains(m.View(), "api_compat") {
		t.Fatalf("editor must not render an api_compat row")
	}
	_, cmd := m.commit()
	committed := cmd().(PresetEditorCommitMsg).Preset.Manifest["llm"].(map[string]interface{})
	if _, ok := committed["api_compat"]; ok {
		t.Fatalf("commit wrote api_compat: %#v", committed)
	}
}

func TestPresetEditorBaseURLIsFreeTextForEveryFamily(t *testing.T) {
	for _, provider := range editorProviders {
		t.Run(provider, func(t *testing.T) {
			p := testOpenAIPresetEditorPreset()
			p.Manifest["llm"].(map[string]interface{})["provider"] = provider
			m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)
			m.cursor = editorFieldOrderIndex(t, feBaseURL)
			if m.isCyclable(feBaseURL) {
				t.Fatalf("base_url must not be an enum for %s", provider)
			}
			before := m.llmMap()["base_url"]
			m.cycleFocused(+1)
			if got := m.llmMap()["base_url"]; got != before {
				t.Fatalf("←/→ on base_url changed it for %s: %#v -> %#v", provider, before, got)
			}
			m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			if m.mode != emInline {
				t.Fatalf("Enter on base_url must open inline edit for %s", provider)
			}
			m.input.SetValue("http://127.0.0.1:8080/v1")
			m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			if got := m.llmMap()["base_url"]; got != "http://127.0.0.1:8080/v1" {
				t.Fatalf("typed base_url = %#v for %s", got, provider)
			}
		})
	}
}

func TestPresetEditorEmptyBaseURLShowsOfficialDefault(t *testing.T) {
	for provider, want := range map[string]string{
		"openai":    preset.OpenAIDefaultBaseURL,
		"anthropic": preset.AnthropicDefaultBaseURL,
	} {
		t.Run(provider, func(t *testing.T) {
			p := testOpenAIPresetEditorPreset()
			llm := p.Manifest["llm"].(map[string]interface{})
			llm["provider"] = provider
			llm["base_url"] = nil
			m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)
			m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 80})
			line := findLineContaining(t, m.View(), i18n.T("preset_editor.field_base_url"))
			if !strings.Contains(line, want) {
				t.Fatalf("empty %s base_url row should name the official endpoint %q; got %q", provider, want, line)
			}
		})
	}
}

func TestPresetEditorCodexCreditsOptInRoundTrip(t *testing.T) {
	t.Setenv("LINGTAI_TUI_DIR", t.TempDir())
	p := testCodexPresetEditorPreset(nil)
	p.Source = preset.SourceSaved
	m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)
	if !m.fieldVisible(feCodexCredits) || !m.isCyclable(feCodexCredits) || m.codexAllowCredits() {
		t.Fatal("Codex credits must be reachable and default off")
	}
	for i, f := range editorFieldOrder {
		if f == feCodexCredits {
			m.cursor = i
		}
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.codexAllowCredits() {
		t.Fatal("Enter must enable credits")
	}
	_, cmd := m.commit()
	if cmd == nil {
		t.Fatal("opt-in preset must commit")
	}
	committed := cmd().(PresetEditorCommitMsg).Preset
	if allow, ok := committed.Manifest["llm"].(map[string]interface{})["codex_allow_credits"].(bool); !ok || !allow {
		t.Fatal("opt-in must be a JSON boolean")
	}
	if err := preset.Save(committed); err != nil {
		t.Fatal(err)
	}
	loaded, err := preset.Load(committed.Name)
	if err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	if err := preset.GenerateInitJSON(loaded, "credit-agent", "credit-agent", project, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(project, "credit-agent", "init.json"))
	if err != nil {
		t.Fatal(err)
	}
	var init map[string]interface{}
	if err := json.Unmarshal(data, &init); err != nil {
		t.Fatal(err)
	}
	if init["manifest"].(map[string]interface{})["llm"].(map[string]interface{})["codex_allow_credits"] != true {
		t.Fatal("credit choice must reach the generated Agent init")
	}
	m = NewPresetEditorModelWithBuiltinFlag(loaded, "en", nil, "", false)
	if !m.codexAllowCredits() {
		t.Fatal("saved opt-in must reload")
	}
	for i, f := range editorFieldOrder {
		if f == feCodexCredits {
			m.cursor = i
		}
	}
	m.cycleFocused(-1)
	_, cmd = m.commit()
	if _, present := cmd().(PresetEditorCommitMsg).Preset.Manifest["llm"].(map[string]interface{})["codex_allow_credits"]; present {
		t.Fatal("off must omit the field")
	}
	m.setCodexAllowCredits(true)
	m.switchProvider(preset.ProviderCodex, preset.ProviderOpenAI)
	if m.fieldVisible(feCodexCredits) || m.isCyclable(feCodexCredits) {
		t.Fatal("other providers must hide credits")
	}
	if _, present := m.llmMap()["codex_allow_credits"]; present {
		t.Fatal("provider switch must clear opt-in")
	}
	m.switchProvider(preset.ProviderOpenAI, preset.ProviderCodex)
	if m.codexAllowCredits() {
		t.Fatal("switching back must default off")
	}
}

func TestPresetEditorCodexCreditsMalformedValuesDefaultOff(t *testing.T) {
	for _, value := range []interface{}{nil, false, "true", "false", 1} {
		p := testCodexPresetEditorPreset(nil)
		p.Manifest["llm"].(map[string]interface{})["codex_allow_credits"] = value
		m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)
		if m.codexAllowCredits() {
			t.Fatalf("%v must not enable credits", value)
		}
		normalizeLLMForCommit(m.working.Manifest)
		if _, present := m.llmMap()["codex_allow_credits"]; present {
			t.Fatalf("%v must be removed on commit", value)
		}
	}
}
