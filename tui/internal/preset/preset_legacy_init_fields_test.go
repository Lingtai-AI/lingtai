package preset

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateInitJSONFreshOmitsObsoletePromptFiles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tmp := t.TempDir()
	lingtaiDir := filepath.Join(tmp, ".lingtai")
	globalDir := filepath.Join(tmp, "global")
	const defaultCovenant = "default Covenant fixture\n"
	defaultPath := CovenantPath(globalDir, "en")
	if err := os.MkdirAll(filepath.Dir(defaultPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(defaultPath, []byte(defaultCovenant), 0600); err != nil {
		t.Fatal(err)
	}
	if err := fixtureGenerateInitJSON(t, codexPreset(), "fresh", "fresh", lingtaiDir, globalDir); err != nil {
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
	if got["covenant_file"] != nil || got["manifest"] != nil {
		t.Error("fresh canonical init must not copy legacy prompt owners")
	}
	// default_covenant_file was legacy metadata, not an active Psyche edit.
	// Exact-base and canonical controls both resolve an empty Covenant here;
	// a readable fallback file must not silently activate a new prompt owner.
	dir := filepath.Join(lingtaiDir, "fresh")
	if _, err := os.Lstat(filepath.Join(dir, "settings", "psyche.json")); !os.IsNotExist(err) {
		t.Errorf("default metadata unexpectedly authored a Psyche owner: %v", err)
	}
	observed := fixtureCoreConfiguration(t, dir)
	if covenant, _ := observed["covenant"].(string); covenant != "" {
		t.Error("legacy default metadata unexpectedly activated a Covenant")
	}
}

func TestGenerateInitJSONOmitsObsoletePromptFilesAndPreservesExistingConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
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
	oldData, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "init.json"), oldData, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := fixtureGenerateInitJSONWithOpts(t, codexPreset(), "alice", "alice", lingtaiDir, globalDir, DefaultAgentOpts()); err != nil {
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
	if !ok {
		t.Fatal("manifest missing after rewrite")
	}
	if manifest["user_flag"] != true {
		t.Errorf("unrelated manifest field was not preserved: %#v", manifest)
	}
}
