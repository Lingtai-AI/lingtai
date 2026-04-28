package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/preset"
)

func TestM024AddsActivePresetForKnownLLM(t *testing.T) {
	tmp := t.TempDir()
	lingtaiDir := filepath.Join(tmp, ".lingtai")
	agentDir := filepath.Join(lingtaiDir, "alice")
	os.MkdirAll(agentDir, 0o755)

	// Set HOME to the temp dir so PresetsDir() lands inside tmp
	t.Setenv("HOME", tmp)

	// Seed the global preset library by writing minimax.json directly.
	globalPresetsDir := filepath.Join(tmp, ".lingtai-tui", "presets")
	os.MkdirAll(globalPresetsDir, 0o755)
	if err := preset.Save(preset.Preset{
		Name:        "minimax",
		Description: "MiniMax M2.7",
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{
				"provider": "minimax", "model": "MiniMax-M2.7-highspeed",
			},
		},
	}); err != nil {
		t.Fatalf("seed preset: %v", err)
	}

	// Write an agent init.json whose llm matches minimax
	init := map[string]interface{}{
		"manifest": map[string]interface{}{
			"agent_name": "alice",
			"llm": map[string]interface{}{
				"provider": "minimax",
				"model":    "MiniMax-M2.7-highspeed",
			},
			"capabilities": map[string]interface{}{},
		},
	}
	data, _ := json.Marshal(init)
	os.WriteFile(filepath.Join(agentDir, "init.json"), data, 0o644)

	if err := migrateAddActivePreset(lingtaiDir); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// init.json now has active_preset = "minimax"
	updated, _ := os.ReadFile(filepath.Join(agentDir, "init.json"))
	var got map[string]interface{}
	json.Unmarshal(updated, &got)
	manifest := got["manifest"].(map[string]interface{})
	if manifest["active_preset"] != "minimax" {
		t.Errorf("active_preset = %v, want 'minimax'", manifest["active_preset"])
	}
}

func TestM024LeavesUnknownLLMAlone(t *testing.T) {
	tmp := t.TempDir()
	lingtaiDir := filepath.Join(tmp, ".lingtai")
	agentDir := filepath.Join(lingtaiDir, "bob")
	os.MkdirAll(agentDir, 0o755)
	t.Setenv("HOME", tmp)

	init := map[string]interface{}{
		"manifest": map[string]interface{}{
			"agent_name": "bob",
			"llm": map[string]interface{}{
				"provider": "exotic",
				"model":    "weird-1",
			},
		},
	}
	data, _ := json.Marshal(init)
	os.WriteFile(filepath.Join(agentDir, "init.json"), data, 0o644)

	if err := migrateAddActivePreset(lingtaiDir); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	updated, _ := os.ReadFile(filepath.Join(agentDir, "init.json"))
	var got map[string]interface{}
	json.Unmarshal(updated, &got)
	manifest := got["manifest"].(map[string]interface{})
	if _, ok := manifest["active_preset"]; ok {
		t.Errorf("expected no active_preset, got %v", manifest["active_preset"])
	}
}

func TestM024SkipsAgentsAlreadyMigrated(t *testing.T) {
	tmp := t.TempDir()
	lingtaiDir := filepath.Join(tmp, ".lingtai")
	agentDir := filepath.Join(lingtaiDir, "carol")
	os.MkdirAll(agentDir, 0o755)
	t.Setenv("HOME", tmp)

	init := map[string]interface{}{
		"manifest": map[string]interface{}{
			"agent_name":    "carol",
			"active_preset": "custom_preset", // already set
			"llm": map[string]interface{}{
				"provider": "minimax",
				"model":    "MiniMax-M2.7-highspeed",
			},
		},
	}
	data, _ := json.Marshal(init)
	os.WriteFile(filepath.Join(agentDir, "init.json"), data, 0o644)

	if err := migrateAddActivePreset(lingtaiDir); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	updated, _ := os.ReadFile(filepath.Join(agentDir, "init.json"))
	var got map[string]interface{}
	json.Unmarshal(updated, &got)
	manifest := got["manifest"].(map[string]interface{})
	if manifest["active_preset"] != "custom_preset" {
		t.Errorf("active_preset overwritten: %v", manifest["active_preset"])
	}
}
