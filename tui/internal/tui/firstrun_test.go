package tui

import (
	"testing"

	"github.com/anthropics/lingtai-tui/internal/preset"
)

func TestGetPresetProvider(t *testing.T) {
	m := FirstRunModel{}

	tests := []struct {
		name     string
		preset   preset.Preset
		wantProv string
	}{
		{
			name: "minimax preset",
			preset: preset.Preset{
				Name: "minimax",
				Manifest: map[string]interface{}{
					"llm": map[string]interface{}{"provider": "minimax"},
				},
			},
			wantProv: "minimax",
		},
		{
			name: "custom preset",
			preset: preset.Preset{
				Name: "custom",
				Manifest: map[string]interface{}{
					"llm": map[string]interface{}{"provider": "custom"},
				},
			},
			wantProv: "custom",
		},
		{
			name: "missing llm, defaults to minimax",
			preset: preset.Preset{
				Name:     "empty",
				Manifest: map[string]interface{}{},
			},
			wantProv: "minimax",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.getPresetProvider(tt.preset)
			if got != tt.wantProv {
				t.Errorf("getPresetProvider() = %q, want %q", got, tt.wantProv)
			}
		})
	}
}

func TestPresetNeedsKey(t *testing.T) {
	m := FirstRunModel{
		// Keys are env-var-keyed now (the field on the preset declares
		// which env var holds its key — see manifest.llm.api_key_env).
		existingKeys: map[string]string{
			"MINIMAX_API_KEY": "my-minimax-key",
		},
	}

	minimaxPreset := preset.Preset{
		Name: "minimax",
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{
				"provider":    "minimax",
				"api_key_env": "MINIMAX_API_KEY",
			},
		},
	}
	customPreset := preset.Preset{
		Name: "custom",
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{
				"provider":    "custom",
				"api_key_env": "LLM_API_KEY",
			},
		},
	}
	// A preset with no api_key_env (e.g. codex OAuth) doesn't need a key.
	codexOAuthPreset := preset.Preset{
		Name: "codex_oauth",
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{"provider": "codex"},
		},
	}

	if m.presetNeedsKey(minimaxPreset) {
		t.Error("minimax preset should not need key (MINIMAX_API_KEY is set)")
	}
	if !m.presetNeedsKey(customPreset) {
		t.Error("custom preset should need key (LLM_API_KEY is unset)")
	}
	if m.presetNeedsKey(codexOAuthPreset) {
		t.Error("codex OAuth preset has no api_key_env — should not need key")
	}
}

func TestPresetNeedsKey_distinctEnvVars(t *testing.T) {
	// Two minimax presets with different env vars: one configured,
	// one not. The provider doesn't determine the lookup — the preset's
	// own api_key_env field does.
	m := FirstRunModel{
		existingKeys: map[string]string{
			"MINIMAX_PERSONAL_KEY": "personal-key",
		},
	}
	personal := preset.Preset{
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{
				"provider":    "minimax",
				"api_key_env": "MINIMAX_PERSONAL_KEY",
			},
		},
	}
	work := preset.Preset{
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{
				"provider":    "minimax",
				"api_key_env": "MINIMAX_WORK_KEY",
			},
		},
	}
	if m.presetNeedsKey(personal) {
		t.Error("personal preset has key, should not need")
	}
	if !m.presetNeedsKey(work) {
		t.Error("work preset uses a distinct env var that's unset, should need")
	}
}
