package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/preset"
)

func TestSetupSaveStripsObsoletePromptFilesAndPreservesExistingConfig(t *testing.T) {
	tmp := t.TempDir()
	baseDir := filepath.Join(tmp, ".lingtai")
	agentDir := filepath.Join(baseDir, "alice")
	globalDir := filepath.Join(tmp, "global")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := map[string]interface{}{
		"manifest": map[string]interface{}{
			"agent_name": "alice",
			"language":   "en",
			"user_flag":  true,
		},
		"covenant_file":   "/project/.recipe/covenant.md",
		"principle_file":  "/project/.lingtai/principle.md",
		"procedures_file": "/project/.lingtai/procedures.md",
		"env_file":        "/project/.lingtai/.env",
		"venv_path":       "/project/.lingtai/runtime/venv",
		"pad":             "keep this pad",
		"user_config":     map[string]interface{}{"keep": "me"},
	}
	data, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	initPath := filepath.Join(agentDir, "init.json")
	if err := os.WriteFile(initPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	m := FirstRunModel{
		baseDir:          baseDir,
		globalDir:        globalDir,
		setupMode:        true,
		setupOrchDir:     agentDir,
		agentName:        "alice",
		cursor:           0,
		presets:          []preset.Preset{minimaxPresetForSetupTest()},
		pendingAgentOpts: preset.DefaultAgentOpts(),
	}
	_, cmd := m.performSetupSaveOnly()
	if cmd == nil {
		t.Fatal("performSetupSaveOnly returned nil completion command")
	}
	msg := cmd()
	if _, ok := msg.(SetupSavedMsg); !ok {
		t.Fatalf("performSetupSaveOnly completion command returned %T, want SetupSavedMsg", msg)
	}

	data, err = os.ReadFile(initPath)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("parse init.json: %v", err)
	}
	for _, key := range []string{"principle_file", "procedures_file"} {
		if _, ok := got[key]; ok {
			t.Errorf("setup rewrite still contains obsolete %q", key)
		}
	}
	for key, want := range map[string]interface{}{
		"covenant_file": "/project/.recipe/covenant.md",
		"env_file":      "/project/.lingtai/.env",
		"venv_path":     "/project/.lingtai/runtime/venv",
		"pad":           "keep this pad",
	} {
		if got[key] != want {
			t.Errorf("%s = %#v, want %#v", key, got[key], want)
		}
	}
	userConfig, ok := got["user_config"].(map[string]interface{})
	if !ok || userConfig["keep"] != "me" {
		t.Errorf("unrelated user_config was not preserved: %#v", got["user_config"])
	}
	manifest, ok := got["manifest"].(map[string]interface{})
	if !ok || manifest["user_flag"] != true {
		t.Errorf("unrelated manifest field was not preserved: %#v", got["manifest"])
	}
}

func minimaxPresetForSetupTest() preset.Preset {
	return preset.Preset{
		Name: "minimax",
		Manifest: map[string]interface{}{
			"language": "en",
			"llm": map[string]interface{}{
				"provider": "minimax",
				"model":    "MiniMax-M2.5",
			},
			"capabilities": map[string]interface{}{},
		},
	}
}
