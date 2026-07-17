package preset

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateInitJSONFreshOmitsObsoletePromptFiles(t *testing.T) {
	tmp := t.TempDir()
	lingtaiDir := filepath.Join(tmp, ".lingtai")
	globalDir := filepath.Join(tmp, "global")
	if err := GenerateInitJSON(minimaxPreset(), "fresh", "fresh", lingtaiDir, globalDir); err != nil {
		t.Fatalf("GenerateInitJSON: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(lingtaiDir, "fresh", "init.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("parse init.json: %v", err)
	}
	for _, key := range []string{"principle_file", "procedures_file"} {
		if _, ok := got[key]; ok {
			t.Errorf("fresh generated init.json contains obsolete %q", key)
		}
	}
	if covenant, ok := got["covenant_file"].(string); !ok || covenant == "" {
		t.Errorf("fresh generated init.json missing supported covenant_file: %#v", got["covenant_file"])
	}
}

func TestGenerateInitJSONOmitsObsoletePromptFilesAndPreservesExistingConfig(t *testing.T) {
	tmp := t.TempDir()
	lingtaiDir := filepath.Join(tmp, ".lingtai")
	agentDir := filepath.Join(lingtaiDir, "alice")
	globalDir := filepath.Join(tmp, "global")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// /setup rewrites an existing init.json. The two *_file keys are stale
	// kernel-facing fields, while the other top-level and manifest fields are
	// supported or user-owned and must survive the rewrite.
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
	oldData, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "init.json"), oldData, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := GenerateInitJSONWithOpts(minimaxPreset(), "alice", "alice", lingtaiDir, globalDir, DefaultAgentOpts()); err != nil {
		t.Fatalf("GenerateInitJSONWithOpts: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(agentDir, "init.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("parse init.json: %v", err)
	}
	for _, key := range []string{"principle_file", "procedures_file"} {
		if _, ok := got[key]; ok {
			t.Errorf("generated init.json still contains obsolete %q", key)
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
	if !ok {
		t.Fatal("manifest missing after rewrite")
	}
	if manifest["user_flag"] != true {
		t.Errorf("unrelated manifest field was not preserved: %#v", manifest)
	}
}
