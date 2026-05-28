package tui

import (
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
				"provider":    "minimax",
				"model":       "MiniMax-M2.7",
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

// TestPresetEditorAPIKeyLockedWhenAlreadyStored verifies that opening the
// editor on a preset whose api_key_env slot already holds a value blocks
// inline edit of the api_key row. Users were confused when the masked row
// silently overwrote the stored key on commit. New presets (empty
// existingKeys) must still be editable — covered by the next test.
func TestPresetEditorAPIKeyLockedWhenAlreadyStored(t *testing.T) {
	keys := map[string]string{"MINIMAX_API_KEY": "sk-existing-value"}
	m := NewPresetEditorModelWithBuiltinFlag(testPresetEditorPreset(), "en", keys, "", false)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	if !m.apiKeyLocked {
		t.Fatalf("expected apiKeyLocked=true when preset opens with stored key")
	}

	// Walk cursor to feAPIKey (index 9 in editorFieldOrder).
	for i := 0; i < 9; i++ {
		m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if editorFieldOrder[m.cursor] != feAPIKey {
		t.Fatalf("expected cursor on feAPIKey, got %v", editorFieldOrder[m.cursor])
	}

	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if m.mode != emBrowse {
		t.Fatalf("expected to stay in browse mode after Enter on locked api_key, got mode=%v", m.mode)
	}
	if m.saveErr == "" {
		t.Fatalf("expected a saveErr message explaining the lock; got empty")
	}
	want := i18n.T("preset_editor.api_key_locked")
	if m.saveErr != want {
		t.Fatalf("expected lock message %q; got %q", want, m.saveErr)
	}
}

// TestPresetEditorAPIKeyEditableWhenNoStoredKey is the inverse: a preset
// with no stored key (typical for first-run flow on a fresh template)
// must still allow inline edit so initial setup works.
func TestPresetEditorAPIKeyEditableWhenNoStoredKey(t *testing.T) {
	m := NewPresetEditorModelWithBuiltinFlag(testPresetEditorPreset(), "en", nil, "", false)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	if m.apiKeyLocked {
		t.Fatalf("expected apiKeyLocked=false when no stored key")
	}

	for i := 0; i < 9; i++ {
		m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
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

func renderedLineCount(s string) int {
	if s == "" {
		return 0
	}
	return len(strings.Split(s, "\n"))
}
