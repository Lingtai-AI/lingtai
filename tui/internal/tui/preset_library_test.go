package tui

import (
	"strings"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/anthropics/lingtai-tui/internal/preset"
)

func TestPresetLibraryClaudePreviewShowsBackendAndCurrentAccount(t *testing.T) {
	m := PresetLibraryModel{
		presets: []preset.Preset{{
			Name:        "claude",
			Description: preset.PresetDescription{Summary: "Claude Code"},
			Manifest: map[string]interface{}{
				"llm": map[string]interface{}{
					"provider": "claude-code",
					"model":    "fable",
				},
			},
		}},
		claudeAccount: "user@example.com",
		lang:          "en",
	}

	view := m.renderPreview(80, 24)
	for _, want := range []string{"claude", "claude-p", "fable", "account", "user@example.com"} {
		if !strings.Contains(view, want) {
			t.Fatalf("preview missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "claude-agent-sdk") {
		t.Fatalf("preview exposed retired provider label:\n%s", view)
	}
}

// TestPresetLibraryForwardsCodexPublicModelsMsgToEmbeddedEditor is the real
// embedded-editor message/command path test for the library host. Unlike
// FirstRunModel, PresetLibraryModel forwards an unrecognized message to the
// embedded editor through its existing generic default: branch (no new
// per-message case needed here) — this test drives the actual
// PresetLibraryModel.Update to prove that generic forwarding really does
// carry codexPublicModelsMsg into the editor, both while the editor is
// focused and — as a no-op — while it is not.
func TestPresetLibraryForwardsCodexPublicModelsMsgToEmbeddedEditor(t *testing.T) {
	p := preset.Preset{
		Name:        "codex-test",
		Description: preset.PresetDescription{Summary: "Codex library test preset"},
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{"provider": "codex", "model": "gpt-5.6-sol"},
		},
	}
	msg := codexPublicModelsMsg{options: []config.CodexModelOption{
		{Slug: "gpt-6-astra", Label: "GPT-6 Astra"},
	}}

	editing := PresetLibraryModel{focus: presetLibFocusEditor, editor: NewPresetEditorModel(p, "en", nil, "")}
	updated, cmd := editing.Update(msg)
	if cmd != nil {
		t.Fatal("forwarding the refresh result must not itself emit a follow-up command")
	}
	if len(updated.editor.codexModels) != 1 || updated.editor.codexModels[0].Slug != "gpt-6-astra" {
		t.Fatalf("editor.codexModels after forwarding = %#v, want the refreshed options applied", updated.editor.codexModels)
	}

	notEditing := PresetLibraryModel{focus: presetLibFocusList, editor: NewPresetEditorModel(p, "en", nil, "")}
	updated, cmd = notEditing.Update(msg)
	if cmd != nil {
		t.Fatal("a stray refresh result while the editor isn't focused must not emit a command")
	}
	if len(updated.editor.codexModels) != len(notEditing.editor.codexModels) {
		t.Fatalf("editor.codexModels while unfocused = %#v, want untouched", updated.editor.codexModels)
	}
}
