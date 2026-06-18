package headless

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/preset"
)

// withTempHome redirects home env vars to a temp dir for isolated preset tests.
// Follows the same pattern as preset_test.go:withTempPresets.
func withTempHome(t *testing.T) {
	t.Helper()
	origHome := os.Getenv("HOME")
	origUserProfile := os.Getenv("USERPROFILE")
	tmp := t.TempDir()
	os.Setenv("HOME", tmp)
	os.Setenv("USERPROFILE", tmp)
	t.Cleanup(func() {
		os.Setenv("HOME", origHome)
		os.Setenv("USERPROFILE", origUserProfile)
	})
}

func TestRunPresets_EmptyDir_ReturnsEmptyList(t *testing.T) {
	withTempHome(t)
	var buf bytes.Buffer
	RunPresets(&buf, &bytes.Buffer{}, false, false)
	var result PresetsOutput
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result.Presets) != 0 {
		t.Errorf("expected 0 presets, got %d", len(result.Presets))
	}
}

func TestRunPresets_WithTemplates_ListsAll(t *testing.T) {
	withTempHome(t)
	if err := preset.RefreshTemplates(); err != nil {
		t.Fatalf("RefreshTemplates: %v", err)
	}
	var buf bytes.Buffer
	RunPresets(&buf, &bytes.Buffer{}, false, false)
	var result PresetsOutput
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result.Presets) < 8 {
		t.Errorf("expected >= 8 presets, got %d", len(result.Presets))
	}
	for _, p := range result.Presets {
		if p.Source != "template" {
			t.Errorf("preset %q: source = %q, want %q", p.Name, p.Source, "template")
		}
	}
}

func TestRunPresets_SavedOnly_ExcludesTemplates(t *testing.T) {
	withTempHome(t)
	if err := preset.RefreshTemplates(); err != nil {
		t.Fatalf("RefreshTemplates: %v", err)
	}
	var buf bytes.Buffer
	RunPresets(&buf, &bytes.Buffer{}, true, false)
	var result PresetsOutput
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result.Presets) != 0 {
		t.Errorf("expected 0 saved presets, got %d", len(result.Presets))
	}
}

func TestRunPresets_TemplatesOnly_ExcludesSaved(t *testing.T) {
	withTempHome(t)
	if err := preset.RefreshTemplates(); err != nil {
		t.Fatalf("RefreshTemplates: %v", err)
	}
	p := preset.Preset{
		Name:        "test-saved",
		Description: preset.PresetDescription{Summary: "A test saved preset"},
		Manifest:    map[string]interface{}{"llm": map[string]interface{}{"provider": "custom", "model": "test"}},
	}
	if err := preset.Save(p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	var buf bytes.Buffer
	RunPresets(&buf, &bytes.Buffer{}, false, true)
	var result PresetsOutput
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, p := range result.Presets {
		if p.Source == "saved" {
			t.Errorf("found saved preset %q in templates-only output", p.Name)
		}
	}
}

func TestRunPresets_HasRequiredFields(t *testing.T) {
	withTempHome(t)
	if err := preset.RefreshTemplates(); err != nil {
		t.Fatalf("RefreshTemplates: %v", err)
	}
	var buf bytes.Buffer
	RunPresets(&buf, &bytes.Buffer{}, false, false)
	var result PresetsOutput
	json.Unmarshal(buf.Bytes(), &result)
	if len(result.Presets) == 0 {
		t.Fatal("no presets to check")
	}
	first := result.Presets[0]
	if first.Name == "" {
		t.Error("name is empty")
	}
	if first.Description == "" {
		t.Error("description is empty")
	}
	if first.Source == "" {
		t.Error("source is empty")
	}
	if first.Path == "" {
		t.Error("path is empty")
	}
}
