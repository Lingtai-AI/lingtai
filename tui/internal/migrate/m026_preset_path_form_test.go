package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestM026RewritesStemToPathForm(t *testing.T) {
	tmp := t.TempDir()
	lingtaiDir := filepath.Join(tmp, ".lingtai")
	agentDir := filepath.Join(lingtaiDir, "alice")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}

	init := map[string]interface{}{
		"manifest": map[string]interface{}{
			"preset": map[string]interface{}{
				"active":  "minimax",
				"default": "minimax",
			},
		},
	}
	data, _ := json.Marshal(init)
	if err := os.WriteFile(filepath.Join(agentDir, "init.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := migratePresetPathForm(lingtaiDir); err != nil {
		t.Fatal(err)
	}

	updated, _ := os.ReadFile(filepath.Join(agentDir, "init.json"))
	var got map[string]interface{}
	json.Unmarshal(updated, &got)
	preset := got["manifest"].(map[string]interface{})["preset"].(map[string]interface{})

	want := "~/.lingtai-tui/presets/minimax.json"
	if preset["active"] != want {
		t.Errorf("active = %v, want %q", preset["active"], want)
	}
	if preset["default"] != want {
		t.Errorf("default = %v, want %q", preset["default"], want)
	}
}

func TestM026PreservesPathForm(t *testing.T) {
	// Already in path form — must not double-suffix.
	tmp := t.TempDir()
	lingtaiDir := filepath.Join(tmp, ".lingtai")
	agentDir := filepath.Join(lingtaiDir, "alice")
	os.MkdirAll(agentDir, 0o755)

	original := "~/.lingtai-tui/presets/minimax.json"
	init := map[string]interface{}{
		"manifest": map[string]interface{}{
			"preset": map[string]interface{}{
				"active":  original,
				"default": original,
			},
		},
	}
	data, _ := json.Marshal(init)
	os.WriteFile(filepath.Join(agentDir, "init.json"), data, 0o644)

	if err := migratePresetPathForm(lingtaiDir); err != nil {
		t.Fatal(err)
	}

	updated, _ := os.ReadFile(filepath.Join(agentDir, "init.json"))
	var got map[string]interface{}
	json.Unmarshal(updated, &got)
	preset := got["manifest"].(map[string]interface{})["preset"].(map[string]interface{})

	if preset["active"] != original {
		t.Errorf("active rewritten unexpectedly: %v", preset["active"])
	}
}

func TestM026UsesCustomLibPath(t *testing.T) {
	// When manifest.preset.path is a string, m026 uses it as the prefix
	// rather than the default ~/.lingtai-tui/presets.
	tmp := t.TempDir()
	lingtaiDir := filepath.Join(tmp, ".lingtai")
	agentDir := filepath.Join(lingtaiDir, "alice")
	os.MkdirAll(agentDir, 0o755)

	init := map[string]interface{}{
		"manifest": map[string]interface{}{
			"preset": map[string]interface{}{
				"path":    "/custom/lib",
				"active":  "thinker",
				"default": "thinker",
			},
		},
	}
	data, _ := json.Marshal(init)
	os.WriteFile(filepath.Join(agentDir, "init.json"), data, 0o644)

	if err := migratePresetPathForm(lingtaiDir); err != nil {
		t.Fatal(err)
	}

	updated, _ := os.ReadFile(filepath.Join(agentDir, "init.json"))
	var got map[string]interface{}
	json.Unmarshal(updated, &got)
	preset := got["manifest"].(map[string]interface{})["preset"].(map[string]interface{})

	want := "/custom/lib/thinker.json"
	if preset["active"] != want {
		t.Errorf("active = %v, want %q", preset["active"], want)
	}
}

func TestM026SkipsAgentsWithoutPresetBlock(t *testing.T) {
	// Custom-config agents (no manifest.preset) are left untouched.
	tmp := t.TempDir()
	lingtaiDir := filepath.Join(tmp, ".lingtai")
	agentDir := filepath.Join(lingtaiDir, "bob")
	os.MkdirAll(agentDir, 0o755)

	init := map[string]interface{}{
		"manifest": map[string]interface{}{
			"llm": map[string]interface{}{"provider": "x", "model": "y"},
		},
	}
	data, _ := json.Marshal(init)
	originalBytes := data
	os.WriteFile(filepath.Join(agentDir, "init.json"), data, 0o644)

	if err := migratePresetPathForm(lingtaiDir); err != nil {
		t.Fatal(err)
	}

	updated, _ := os.ReadFile(filepath.Join(agentDir, "init.json"))
	if string(updated) != string(originalBytes) {
		t.Errorf("unexpected modification of agent without preset block")
	}
}
