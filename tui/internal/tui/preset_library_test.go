package tui

import (
	"strings"
	"testing"

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
