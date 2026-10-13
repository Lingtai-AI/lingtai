package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/anthropics/lingtai-tui/internal/preset"
)

// The actual Project CLI comes only from the parent's cold-installed package.
// The local interpreter handle is deliberately non-executable: Project commands
// use its sibling CLI, whereas an accidental agent launch must fail before any
// provider or runtime starts. This is fixture wiring, not a fake Project merger.
func fixtureUIKernel(t *testing.T, globalDir string) string {
	t.Helper()
	installed := fixtureUIProjectPython(t)
	python := config.VenvPython(filepath.Join(globalDir, "runtime", "venv"))
	if err := os.MkdirAll(filepath.Dir(python), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(python, []byte("test-only provider launch guard"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(filepath.Dir(installed), "lingtai-agent"), filepath.Join(filepath.Dir(python), "lingtai-agent")); err != nil {
		t.Fatal(err)
	}
	return python
}

func fixtureUIProjectPython(t *testing.T) string {
	t.Helper()
	python := os.Getenv("LINGTAI_PROJECT_CONFIGURATION_TEST_PYTHON")
	if python == "" || !filepath.IsAbs(python) {
		t.Fatal("explicit parent-verified installed test interpreter required; no global runtime fallback")
	}
	if _, err := os.Stat(python); err != nil {
		t.Fatal(err)
	}
	return python
}

func fixtureUISetupPreset(t *testing.T) preset.Preset {
	t.Helper()
	p := minimaxPresetForSetupTest()
	// The current kernel routes MiniMax via its Anthropic-compatible provider;
	// the old standalone "minimax" provider was independently retired.
	p.Manifest["llm"].(map[string]interface{})["provider"] = "anthropic"
	p.Source = preset.SourceSaved
	p.Description = preset.PresetDescription{Summary: "setup fixture"}
	if err := preset.Save(p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSetupSaveStripsObsoletePromptFilesAndPreservesExistingConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tmp := t.TempDir()
	baseDir := filepath.Join(tmp, ".lingtai")
	agentDir := filepath.Join(baseDir, "alice")
	globalDir := filepath.Join(tmp, "global")
	fixtureUIKernel(t, globalDir)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	envFile := filepath.Join(tmp, "custom.env")
	if err := os.WriteFile(envFile, nil, 0600); err != nil {
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
		"env_file":        envFile,
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
		presets:          []preset.Preset{fixtureUISetupPreset(t)},
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
		"env_file":      envFile,
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
