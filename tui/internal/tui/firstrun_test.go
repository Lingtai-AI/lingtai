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

func TestNeedsKey(t *testing.T) {
	m := FirstRunModel{
		existingKeys: map[string]string{
			"minimax": "my-minimax-key",
		},
	}

	if m.needsKey("minimax") {
		t.Error("minimax has key, should not need")
	}
	if !m.needsKey("custom") {
		t.Error("custom missing key, should need")
	}
}

func TestPresetNeedsKey(t *testing.T) {
	m := FirstRunModel{
		existingKeys: map[string]string{
			"minimax": "my-minimax-key",
		},
	}

	minimaxPreset := preset.Preset{
		Name: "minimax",
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{"provider": "minimax"},
		},
	}
	customPreset := preset.Preset{
		Name: "custom",
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{"provider": "custom"},
		},
	}

	if m.presetNeedsKey(minimaxPreset) {
		t.Error("minimax preset should not need key")
	}
	if !m.presetNeedsKey(customPreset) {
		t.Error("custom preset should need key (not configured)")
	}
}
