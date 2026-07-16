package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

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
				"provider":    "minimax",
				"model":       "MiniMax-M3",
				"api_compat":  "openai",
				"base_url":    "https://api.minimax.io/v1",
				"api_key_env": "MINIMAX_API_KEY",
			},
			"capabilities": map[string]interface{}{
				"file":       map[string]interface{}{},
				"bash":       map[string]interface{}{"yolo": true},
				"avatar":     map[string]interface{}{},
				"daemon":     map[string]interface{}{},
				"web_search": map[string]interface{}{"provider": "duckduckgo"},
				"vision":     map[string]interface{}{"provider": "inherit"},
			},
		},
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

func TestPresetEditorProviderModelLineupsPinRequestedDefaults(t *testing.T) {
	if got := providerModels["zhipu"][0]; got != "GLM-5.2" {
		t.Fatalf("zhipu default picker model = %q, want GLM-5.2", got)
	}
	if got := providerModels["deepseek"][0]; got != "deepseek-v4-pro" {
		t.Fatalf("deepseek default picker model = %q, want deepseek-v4-pro", got)
	}
	wantMiMoModels := []string{"mimo-v2.5", "mimo-v2.5-pro"}
	if got := providerModels["mimo"]; !reflect.DeepEqual(got, wantMiMoModels) {
		t.Fatalf("mimo provider models = %#v, want %#v", got, wantMiMoModels)
	}
	if !modelHasVision["mimo-v2.5"] || modelHasVision["mimo-v2.5-pro"] {
		t.Fatal("MiMo picker must keep native vision only for mimo-v2.5")
	}
	for _, provider := range capabilityProviderOptions["vision"] {
		if provider == "zhipu" {
			t.Fatal("vision provider picker must not expose the removed legacy Zhipu route")
		}
	}
	for _, model := range providerModels["zhipu"] {
		if modelHasVision[model] {
			t.Fatalf("Zhipu coding-plan model %s must remain text-only; GLM-4.6V uses the optional manual MCP path", model)
		}
	}

	wantCodexModels := []string{
		"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna",
		"gpt-5.5", "gpt-5.4", "gpt-5.4-mini", "gpt-5.3-codex", "gpt-5.2",
	}
	for _, provider := range []string{"codex", "codex-pool"} {
		models := providerModels[provider]
		if !reflect.DeepEqual(models, wantCodexModels) {
			t.Fatalf("%s provider models = %#v, want %#v", provider, models, wantCodexModels)
		}
	}
	for _, model := range wantCodexModels[:4] {
		if !modelHasVision[model] {
			t.Fatalf("%s should be treated as vision-capable like the rest of the GPT-5.x Codex lineup", model)
		}
	}
}

func TestPresetEditorVisionProviderCyclePreservesOrResetsExactIdentity(t *testing.T) {
	wantOptions := []string{"inherit", "minimax", "mimo", "gemini", "codex", "codex-pool"}
	if got := capabilityProviderOptions["vision"]; !reflect.DeepEqual(got, wantOptions) {
		t.Fatalf("vision provider options = %#v, want %#v", got, wantOptions)
	}

	tests := []struct {
		name          string
		current       string
		next          string
		currentKeyEnv string
	}{
		{name: "gemini", current: "gemini", next: "codex", currentKeyEnv: "GEMINI_API_KEY"},
		{name: "codex-pool", current: "codex-pool", next: "inherit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := builtinPresetForEditorTest(t, tt.name)
			m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", true)
			if strip := m.capProviderStrip("vision", true); !strings.Contains(strip, "● "+tt.current) {
				t.Fatalf("vision strip does not show current provider %q: %q", tt.current, strip)
			}

			_, unchangedCmd := m.commit()
			unchanged := unchangedCmd().(PresetEditorCommitMsg)
			unchangedCaps := unchanged.Preset.Manifest["capabilities"].(map[string]interface{})
			unchangedVision := unchangedCaps["vision"].(map[string]interface{})
			if got := unchangedVision["provider"]; got != tt.current {
				t.Fatalf("ordinary clone changed vision provider: got %#v, want %q", got, tt.current)
			}
			if tt.currentKeyEnv != "" {
				if got := unchangedVision["api_key_env"]; got != tt.currentKeyEnv {
					t.Fatalf("ordinary clone changed current credential source: got %#v, want %q", got, tt.currentKeyEnv)
				}
			}

			m.cycleCapProvider("vision", +1)
			_, changedCmd := m.commit()
			changed := changedCmd().(PresetEditorCommitMsg)
			changedCaps := changed.Preset.Manifest["capabilities"].(map[string]interface{})
			changedVision := changedCaps["vision"].(map[string]interface{})
			if got := changedVision["provider"]; got != tt.next {
				t.Fatalf("cycled vision provider = %#v, want %q", got, tt.next)
			}
			if len(changedVision) != 1 {
				t.Fatalf("provider cycle retained incompatible identity fields: %#v", changedVision)
			}
		})
	}
}

func TestPresetEditorCodexPoolThinkingIsEditableAndPreserved(t *testing.T) {
	p := builtinPresetForEditorTest(t, "codex-pool")
	m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", true)

	if !m.fieldVisible(feThinking) {
		t.Fatal("codex-pool thinking field must be visible")
	}
	if !m.isCyclable(feThinking) {
		t.Fatal("codex-pool thinking field must be cyclable")
	}

	_, unchangedCmd := m.commit()
	unchanged := unchangedCmd().(PresetEditorCommitMsg)
	unchangedLLM := unchanged.Preset.Manifest["llm"].(map[string]interface{})
	if got := unchangedLLM["thinking"]; got != "xhigh" {
		t.Fatalf("ordinary codex-pool commit changed thinking: got %#v, want xhigh", got)
	}

	m.setCodexThinking("high")
	_, changedCmd := m.commit()
	changed := changedCmd().(PresetEditorCommitMsg)
	changedLLM := changed.Preset.Manifest["llm"].(map[string]interface{})
	if got := changedLLM["thinking"]; got != "high" {
		t.Fatalf("codex-pool thinking edit was not preserved: got %#v, want high", got)
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
	keys := map[string]string{"MINIMAX_API_KEY": "sk-existing-value"}
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
	keys := map[string]string{"MINIMAX_API_KEY": "sk-existing-value"}
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
	keys := map[string]string{"MINIMAX_API_KEY": "sk-existing-value"}
	p := testPresetEditorPreset()
	p.Source = preset.SourceSaved
	m := NewPresetEditorModel(p, "en", keys, "")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	for i := 0; i < 9; i++ {
		m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
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
	keys := map[string]string{"MINIMAX_API_KEY": "sk-existing-value"}
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

func TestPresetEditorCapabilitiesAreOptionalOnly(t *testing.T) {
	wantCaps := []string{"web_search", "vision"}
	if strings.Join(editorCapabilities, ",") != strings.Join(wantCaps, ",") {
		t.Fatalf("editorCapabilities = %#v, want %#v", editorCapabilities, wantCaps)
	}

	for _, field := range []editorField{feCapFile, feCapBash, feCapAvatar, feCapDaemon} {
		for _, got := range editorFieldOrder {
			if got == field {
				t.Fatalf("core capability field %v should not be in editorFieldOrder %#v", field, editorFieldOrder)
			}
		}
	}
}

func TestDefaultCapsForDoesNotSerializeCoreFloor(t *testing.T) {
	tests := []struct {
		model      string
		wantVision bool
	}{
		{model: "mimo-v2.5", wantVision: true},
		{model: "mimo-v2.5-pro", wantVision: false},
	}
	coreCaps := []string{"file", "bash", "avatar", "daemon", "knowledge", "library", "skills", "mcp"}

	for _, tt := range tests {
		caps := defaultCapsFor(tt.model)
		if _, ok := caps["web_search"]; !ok {
			t.Fatalf("defaultCapsFor(%q) missing web_search: %#v", tt.model, caps)
		}
		_, hasVision := caps["vision"]
		if hasVision != tt.wantVision {
			t.Fatalf("defaultCapsFor(%q) vision presence = %v, want %v; caps=%#v", tt.model, hasVision, tt.wantVision, caps)
		}
		for _, capName := range coreCaps {
			if _, ok := caps[capName]; ok {
				t.Fatalf("defaultCapsFor(%q) serialized core capability %q: %#v", tt.model, capName, caps)
			}
		}
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
	for _, capName := range []string{"library", "skills", "file", "bash", "avatar", "daemon"} {
		if _, ok := caps[capName]; ok {
			t.Fatalf("commit injected core/legacy capability %q: %#v", capName, caps)
		}
	}
	if _, ok := caps["web_search"]; !ok {
		t.Fatalf("commit lost optional web_search capability: %#v", caps)
	}
}

// TestSyncCapsToModelPreservesNonOptionalCapabilities is the regression
// test for issue #311: switching models must not delete capability
// entries that are not model-conditional (skills.paths overrides, bash
// policy, etc.). Only the optionalCapabilities (web_search, vision) may
// be reset to the target model's defaults.
func TestSyncCapsToModelPreservesNonOptionalCapabilities(t *testing.T) {
	skillsPaths := []interface{}{"../.library_shared", "~/.lingtai-tui/utilities"}
	p := testPresetEditorPreset()
	p.Manifest["capabilities"] = map[string]interface{}{
		"web_search": map[string]interface{}{"provider": "zhipu"},
		"vision":     map[string]interface{}{"provider": "inherit"},
		"skills":     map[string]interface{}{"paths": skillsPaths},
		"bash":       map[string]interface{}{"yolo": true},
	}
	m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)

	// mimo-v2.5-pro is a cataloged text-only model: vision must drop,
	// web_search must reset to the default backend.
	m.syncCapsToModel("mimo-v2.5-pro")

	caps, ok := m.working.Manifest["capabilities"].(map[string]interface{})
	if !ok {
		t.Fatalf("capabilities missing/wrong type after model switch: %T", m.working.Manifest["capabilities"])
	}
	skills, ok := caps["skills"].(map[string]interface{})
	if !ok {
		t.Fatalf("model switch dropped skills capability: %#v", caps)
	}
	if !reflect.DeepEqual(skills["paths"], skillsPaths) {
		t.Fatalf("model switch mangled skills.paths: got %#v, want %#v", skills["paths"], skillsPaths)
	}
	bash, ok := caps["bash"].(map[string]interface{})
	if !ok {
		t.Fatalf("model switch dropped bash capability override: %#v", caps)
	}
	if yolo, _ := bash["yolo"].(bool); !yolo {
		t.Fatalf("model switch lost bash yolo override: %#v", bash)
	}
	ws, ok := caps["web_search"].(map[string]interface{})
	if !ok {
		t.Fatalf("model switch lost web_search: %#v", caps)
	}
	if got := ws["provider"]; got != "duckduckgo" {
		t.Fatalf("web_search should reset to target model default duckduckgo, got %#v", got)
	}
	if _, hasVision := caps["vision"]; hasVision {
		t.Fatalf("vision should be dropped for text-only model: %#v", caps)
	}
}

func TestSyncCapsToModelAddsVisionAndKeepsSkillsOnVisionModel(t *testing.T) {
	skillsPaths := []interface{}{"../.library_shared"}
	p := testPresetEditorPreset()
	p.Manifest["capabilities"] = map[string]interface{}{
		"web_search": map[string]interface{}{"provider": "duckduckgo"},
		"skills":     map[string]interface{}{"paths": skillsPaths},
	}
	m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)

	// mimo-v2.5 is vision-capable: vision must appear with its default.
	m.syncCapsToModel("mimo-v2.5")

	caps, ok := m.working.Manifest["capabilities"].(map[string]interface{})
	if !ok {
		t.Fatalf("capabilities missing/wrong type after model switch: %T", m.working.Manifest["capabilities"])
	}
	skills, ok := caps["skills"].(map[string]interface{})
	if !ok {
		t.Fatalf("model switch dropped skills capability: %#v", caps)
	}
	if !reflect.DeepEqual(skills["paths"], skillsPaths) {
		t.Fatalf("model switch mangled skills.paths: got %#v, want %#v", skills["paths"], skillsPaths)
	}
	vision, ok := caps["vision"].(map[string]interface{})
	if !ok {
		t.Fatalf("vision-capable model should gain vision default: %#v", caps)
	}
	if got := vision["provider"]; got != "inherit" {
		t.Fatalf("vision default provider = %#v, want \"inherit\"", got)
	}
}

func TestSyncCapsToModelLeavesCapsAloneForUnknownModel(t *testing.T) {
	p := testPresetEditorPreset()
	p.Manifest["capabilities"] = map[string]interface{}{
		"web_search": map[string]interface{}{"provider": "zhipu"},
		"skills":     map[string]interface{}{"paths": []interface{}{"../.library_shared"}},
	}
	m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)
	before := m.working.Manifest["capabilities"]

	m.syncCapsToModel("some-free-text-model")

	if !reflect.DeepEqual(m.working.Manifest["capabilities"], before) {
		t.Fatalf("unknown model id must not touch capabilities: got %#v, want %#v",
			m.working.Manifest["capabilities"], before)
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

func TestPresetEditorThinkingHiddenAndRemovedForNonCodex(t *testing.T) {
	m := NewPresetEditorModelWithBuiltinFlag(testPresetEditorPreset(), "en", nil, "", false)
	llm := m.working.Manifest["llm"].(map[string]interface{})
	llm["thinking"] = "low"
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 80})
	view := m.View()

	if m.fieldVisible(feThinking) {
		t.Fatalf("reasoning effort row should be hidden for non-codex provider")
	}
	if strings.Contains(view, "Reasoning effort") || strings.Contains(view, "llm.thinking") {
		t.Fatalf("non-codex editor should not render thinking row; view:\n%s", view)
	}

	m.cursor = editorFieldOrderIndex(t, feServiceTier)
	m.normalizeCursor()
	if editorFieldOrder[m.cursor] == feThinking {
		t.Fatalf("cursor landed on hidden thinking field for non-codex preset")
	}

	_, cmd := m.commit()
	commit := cmd().(PresetEditorCommitMsg)
	committedLLM := commit.Preset.Manifest["llm"].(map[string]interface{})
	if _, ok := committedLLM["thinking"]; ok {
		t.Fatalf("non-codex commit should remove llm.thinking; got %#v", committedLLM["thinking"])
	}
}

func TestPresetEditorProviderSwitchClearsThinking(t *testing.T) {
	m := NewPresetEditorModelWithBuiltinFlag(testCodexPresetEditorPresetWithThinking(nil, "low"), "en", nil, "", false)
	m.cursor = editorFieldOrderIndex(t, feProvider)

	m.cycleFocused(+1) // codex -> custom in provider picker order.
	llm := m.working.Manifest["llm"].(map[string]interface{})
	if got := llm["provider"]; got != "custom" {
		t.Fatalf("provider after cycling from codex = %#v, want custom", got)
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

func TestPresetEditorServiceTierHiddenForNonCodexAndPreserved(t *testing.T) {
	m := NewPresetEditorModelWithBuiltinFlag(testPresetEditorPreset(), "en", nil, "", false)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 80})
	view := m.View()

	if m.fieldVisible(feServiceTier) {
		t.Fatalf("service tier row should be hidden for non-codex provider")
	}
	if strings.Contains(view, "service_tier") {
		t.Fatalf("non-codex editor should not render service_tier row; view:\n%s", view)
	}

	m.cursor = editorFieldOrderIndex(t, feModel)
	m.moveCursor(+1)
	if editorFieldOrder[m.cursor] == feServiceTier {
		t.Fatalf("cursor landed on hidden service tier field for non-codex preset")
	}
	if editorFieldOrder[m.cursor] != feAPICompat {
		t.Fatalf("cursor after model = %v, want feAPICompat", editorFieldOrder[m.cursor])
	}

	llm := m.working.Manifest["llm"].(map[string]interface{})
	llm["service_tier"] = "provider-specific"
	_, cmd := m.commit()
	commit := cmd().(PresetEditorCommitMsg)
	committedLLM := commit.Preset.Manifest["llm"].(map[string]interface{})
	if got, _ := committedLLM["service_tier"].(string); got != "provider-specific" {
		t.Fatalf("non-codex commit should preserve llm.service_tier; got %#v", committedLLM["service_tier"])
	}
}

func TestPresetEditorProviderSwitchPreservesServiceTier(t *testing.T) {
	m := NewPresetEditorModelWithBuiltinFlag(testCodexPresetEditorPreset("fast"), "en", nil, "", false)
	m.cursor = editorFieldOrderIndex(t, feProvider)

	m.cycleFocused(+1) // codex -> custom in provider picker order.
	llm := m.working.Manifest["llm"].(map[string]interface{})
	if got := llm["provider"]; got != "custom" {
		t.Fatalf("provider after cycling from codex = %#v, want custom", got)
	}
	if got, _ := llm["service_tier"].(string); got != "fast" {
		t.Fatalf("provider switch should preserve existing service_tier; got %#v", llm["service_tier"])
	}

	_, cmd := m.commit()
	commit := cmd().(PresetEditorCommitMsg)
	committedLLM := commit.Preset.Manifest["llm"].(map[string]interface{})
	if got, _ := committedLLM["service_tier"].(string); got != "fast" {
		t.Fatalf("non-codex commit after provider switch should preserve service_tier; got %#v", committedLLM["service_tier"])
	}
}

func TestPresetEditorViewShowsCoreAsInformational(t *testing.T) {
	m := NewPresetEditorModelWithBuiltinFlag(testPresetEditorPreset(), "en", nil, "", false)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 80})
	view := m.View()

	for _, capName := range []string{"knowledge", "skills", "bash", "avatar", "daemon", "mcp", "file"} {
		if !strings.Contains(view, capName) {
			t.Fatalf("view missing always-included capability %q; view:\n%s", capName, view)
		}
	}
	for _, capName := range []string{"web_search", "vision"} {
		if !strings.Contains(view, capName) {
			t.Fatalf("view missing optional capability %q; view:\n%s", capName, view)
		}
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
// wire_api (OpenAI wire-format selector for custom+openai presets)
// ─────────────────────────────────────────────────────────────────────────────

func testCustomOpenAIPresetEditorPreset() preset.Preset {
	return preset.Preset{
		Name:        "custom-openai-test",
		Description: preset.PresetDescription{Summary: "Custom OpenAI-compat test preset"},
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{
				"provider":    "custom",
				"model":       "gpt-oss-test",
				"api_compat":  "openai",
				"base_url":    "https://api.example.com/v1",
				"api_key_env": "CUSTOM_API_KEY",
			},
			"capabilities": map[string]interface{}{},
		},
	}
}

func TestPresetEditorWireAPIVisibleForCustomOpenAI(t *testing.T) {
	m := NewPresetEditorModelWithBuiltinFlag(testCustomOpenAIPresetEditorPreset(), "en", nil, "", false)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 80})
	view := m.View()

	if !m.fieldVisible(feWireAPI) {
		t.Fatalf("wire_api row should be visible for custom+openai provider")
	}
	if !m.isCyclable(feWireAPI) {
		t.Fatalf("wire_api should be cyclable for custom+openai provider")
	}
	if !strings.Contains(view, "wire_api") {
		t.Fatalf("custom+openai editor should render wire_api row; view:\n%s", view)
	}
}

func TestPresetEditorWireAPIHiddenOutsideCustomOpenAI(t *testing.T) {
	// Built-in provider (minimax) with api_compat=openai: must NOT surface.
	m := NewPresetEditorModelWithBuiltinFlag(testPresetEditorPreset(), "en", nil, "", false)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 80})
	view := m.View()
	if m.fieldVisible(feWireAPI) {
		t.Fatalf("wire_api row should be hidden for non-custom provider")
	}
	if strings.Contains(view, "wire_api") {
		t.Fatalf("minimax editor should not render wire_api row; view:\n%s", view)
	}

	// Custom with api_compat=anthropic: must NOT surface.
	p := testCustomOpenAIPresetEditorPreset()
	llm := p.Manifest["llm"].(map[string]interface{})
	llm["api_compat"] = "anthropic"
	m2 := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)
	if m2.fieldVisible(feWireAPI) {
		t.Fatalf("wire_api row should be hidden for custom+anthropic")
	}

	// Custom with api_compat unset: must NOT surface.
	p2 := testCustomOpenAIPresetEditorPreset()
	llm2 := p2.Manifest["llm"].(map[string]interface{})
	delete(llm2, "api_compat")
	m3 := NewPresetEditorModelWithBuiltinFlag(p2, "en", nil, "", false)
	if m3.fieldVisible(feWireAPI) {
		t.Fatalf("wire_api row should be hidden for custom with no api_compat")
	}
}

func TestPresetEditorWireAPICursorSkipsHiddenField(t *testing.T) {
	// For a non-custom-openai preset, cursor navigation must skip the
	// hidden feWireAPI and advance from feAPICompat to feBaseURL.
	m := NewPresetEditorModelWithBuiltinFlag(testPresetEditorPreset(), "en", nil, "", false)
	m.cursor = editorFieldOrderIndex(t, feAPICompat)
	m.moveCursor(+1)
	if editorFieldOrder[m.cursor] == feWireAPI {
		t.Fatalf("cursor landed on hidden wire_api field for non-custom-openai preset")
	}
	if editorFieldOrder[m.cursor] != feBaseURL {
		t.Fatalf("cursor after api_compat = %v, want feBaseURL", editorFieldOrder[m.cursor])
	}
}

func TestPresetEditorWireAPIDefaultsToAuto(t *testing.T) {
	m := NewPresetEditorModelWithBuiltinFlag(testCustomOpenAIPresetEditorPreset(), "en", nil, "", false)
	if got := m.fieldString(feWireAPI); got != "auto" {
		t.Fatalf("absent wire_api displays %q, want auto", got)
	}
}

func TestPresetEditorWireAPICycling(t *testing.T) {
	m := NewPresetEditorModelWithBuiltinFlag(testCustomOpenAIPresetEditorPreset(), "en", nil, "", false)
	m.cursor = editorFieldOrderIndex(t, feWireAPI)

	// auto -> chat_completions
	m.cycleFocused(+1)
	if got := m.fieldString(feWireAPI); got != "chat_completions" {
		t.Fatalf("cycling auto -> +1 = %q, want chat_completions", got)
	}
	llm := m.working.Manifest["llm"].(map[string]interface{})
	if got, _ := llm["wire_api"].(string); got != "chat_completions" {
		t.Fatalf("wire_api should be persisted as chat_completions, got %#v", llm["wire_api"])
	}

	// chat_completions -> responses
	m.cycleFocused(+1)
	if got := m.fieldString(feWireAPI); got != "responses" {
		t.Fatalf("cycling chat_completions -> +1 = %q, want responses", got)
	}

	// responses -> auto (absent)
	m.cycleFocused(+1)
	if got := m.fieldString(feWireAPI); got != "auto" {
		t.Fatalf("cycling responses -> +1 = %q, want auto", got)
	}
	llm = m.working.Manifest["llm"].(map[string]interface{})
	if _, ok := llm["wire_api"]; ok {
		t.Fatalf("cycling to auto should delete wire_api key; got %#v", llm["wire_api"])
	}

	// Reverse: auto -> responses
	m.cycleFocused(-1)
	if got := m.fieldString(feWireAPI); got != "responses" {
		t.Fatalf("cycling auto -> -1 = %q, want responses", got)
	}
}

func TestPresetEditorWireAPICommitPersistsAndOmitsAuto(t *testing.T) {
	m := NewPresetEditorModelWithBuiltinFlag(testCustomOpenAIPresetEditorPreset(), "en", nil, "", false)
	m.cursor = editorFieldOrderIndex(t, feWireAPI)

	// Select responses and commit — should persist.
	m.cycleFocused(+1) // auto -> chat_completions
	m.cycleFocused(+1) // chat_completions -> responses
	_, cmd := m.commit()
	commit := cmd().(PresetEditorCommitMsg)
	committedLLM := commit.Preset.Manifest["llm"].(map[string]interface{})
	if got, _ := committedLLM["wire_api"].(string); got != "responses" {
		t.Fatalf("committed wire_api=%#v, want responses", committedLLM["wire_api"])
	}

	// Cycle back to auto and commit — should omit the key entirely.
	m.cycleFocused(+1) // responses -> auto
	_, cmd = m.commit()
	commit = cmd().(PresetEditorCommitMsg)
	committedLLM = commit.Preset.Manifest["llm"].(map[string]interface{})
	if _, ok := committedLLM["wire_api"]; ok {
		t.Fatalf("committing auto wire_api should omit the key; got %#v", committedLLM["wire_api"])
	}
}

func TestPresetEditorWireAPICleanupOnScopeExit(t *testing.T) {
	p := testCustomOpenAIPresetEditorPreset()
	llm := p.Manifest["llm"].(map[string]interface{})
	llm["wire_api"] = "responses"
	m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)

	// Switch api_compat away from openai while keeping provider=custom.
	m.cursor = editorFieldOrderIndex(t, feAPICompat)
	m.cycleFocused(+1) // openai -> anthropic
	if _, ok := m.llmMap()["wire_api"]; ok {
		t.Fatalf("leaving openai compat should remove wire_api immediately")
	}
	if m.fieldVisible(feWireAPI) {
		t.Fatalf("wire_api should be hidden after api_compat leaves openai")
	}

	// Commit must strip the stale wire_api.
	_, cmd := m.commit()
	commit := cmd().(PresetEditorCommitMsg)
	committedLLM := commit.Preset.Manifest["llm"].(map[string]interface{})
	if _, ok := committedLLM["wire_api"]; ok {
		t.Fatalf("commit after scope exit should remove wire_api; got %#v", committedLLM["wire_api"])
	}
}

func TestPresetEditorWireAPICleanupOnProviderSwitch(t *testing.T) {
	p := testCustomOpenAIPresetEditorPreset()
	llm := p.Manifest["llm"].(map[string]interface{})
	llm["wire_api"] = "chat_completions"
	m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)

	// Switch provider away from custom (custom is last in picker order,
	// so cycling wraps to the first provider, minimax).
	m.cursor = editorFieldOrderIndex(t, feProvider)
	m.cycleFocused(+1)
	if _, ok := m.llmMap()["wire_api"]; ok {
		t.Fatalf("leaving the custom provider should remove wire_api immediately")
	}

	// Commit must strip the stale wire_api.
	_, cmd := m.commit()
	commit := cmd().(PresetEditorCommitMsg)
	committedLLM := commit.Preset.Manifest["llm"].(map[string]interface{})
	if _, ok := committedLLM["wire_api"]; ok {
		t.Fatalf("commit after provider switch should remove wire_api; got %#v", committedLLM["wire_api"])
	}
}

func TestPresetEditorWireAPIEnterCyclesAndPreservesLegacyFlags(t *testing.T) {
	p := testCustomOpenAIPresetEditorPreset()
	llm := p.Manifest["llm"].(map[string]interface{})
	llm["use_responses_api"] = true
	llm["force_responses"] = true
	m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)
	m.cursor = editorFieldOrderIndex(t, feWireAPI)

	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := m.fieldString(feWireAPI); got != "chat_completions" {
		t.Fatalf("Enter on wire_api = %q, want chat_completions", got)
	}
	for _, key := range []string{"use_responses_api", "force_responses"} {
		if got := m.llmMap()[key]; got != true {
			t.Fatalf("wire selection must preserve legacy %s=true, got %#v", key, got)
		}
	}

	m.cycleFocused(+1) // chat_completions -> responses
	m.cycleFocused(+1) // responses -> auto (canonical key omitted)
	if _, ok := m.llmMap()["wire_api"]; ok {
		t.Fatalf("auto should omit only canonical wire_api")
	}
	for _, key := range []string{"use_responses_api", "force_responses"} {
		if got := m.llmMap()[key]; got != true {
			t.Fatalf("auto must keep legacy delegation flag %s=true, got %#v", key, got)
		}
	}
}
