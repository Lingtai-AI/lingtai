package tui

import (
	"strings"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/preset"
)

func claudeLibraryPreset() preset.Preset {
	return preset.Preset{
		Name:        "claude",
		Description: preset.PresetDescription{Summary: "Claude Code"},
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{
				"provider":    "claude-code",
				"api_key_env": "CLAUDE_CODE_OAUTH_TOKEN",
			},
		},
	}
}

// The Claude preview names the claude-p backend and which credential the
// preset runs on: a stored setup-token wins over the local login, and the
// local login (with its account) is used when no token is stored.
func TestPresetLibraryClaudePreviewShowsBackendAndAuthSource(t *testing.T) {
	cases := []struct {
		name       string
		keyPresent map[string]bool
		login      claudeCodeAuthInfo
		want       []string
		notWant    []string
	}{
		{
			name:  "local login",
			login: claudeCodeAuthInfo{LoggedIn: true, Email: "user@example.com"},
			want:  []string{"claude-p", "auth", "local Claude login", "user@example.com"},
		},
		{
			name:       "token takes precedence",
			keyPresent: map[string]bool{"CLAUDE_CODE_OAUTH_TOKEN": true},
			login:      claudeCodeAuthInfo{LoggedIn: true, Email: "user@example.com"},
			want:       []string{"claude-p", "setup-token stored", "CLAUDE_CODE_OAUTH_TOKEN"},
			notWant:    []string{"user@example.com"},
		},
		{
			name: "neither",
			want: []string{"claude-p", "no local Claude login and no setup-token"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := PresetLibraryModel{
				presets:          []preset.Preset{claudeLibraryPreset()},
				keyPresent:       tc.keyPresent,
				claudeLogin:      tc.login,
				claudeLoginKnown: true,
				lang:             "en",
			}
			view := m.renderPreview(100, 24)
			for _, want := range tc.want {
				if !strings.Contains(view, want) {
					t.Fatalf("preview missing %q:\n%s", want, view)
				}
			}
			for _, bad := range tc.notWant {
				if strings.Contains(view, bad) {
					t.Fatalf("preview must not show %q:\n%s", bad, view)
				}
			}
			// claude-code carries no model, so no empty model row either.
			if strings.Contains(view, "model") {
				t.Fatalf("claude preview rendered a model row:\n%s", view)
			}
			if strings.Contains(view, "claude-agent-sdk") {
				t.Fatalf("preview exposed retired provider label:\n%s", view)
			}
		})
	}
}
