package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeLegacyPromptFieldsInit(t *testing.T, orchDir string) string {
	t.Helper()
	path := filepath.Join(orchDir, "init.json")
	init := map[string]interface{}{
		"manifest": map[string]interface{}{
			"agent_name": "old",
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
	data, err := json.Marshal(init)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertLegacyPromptFieldsStripped(t *testing.T, path string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("parse init.json: %v", err)
	}
	for _, key := range []string{"principle_file", "procedures_file"} {
		if _, ok := got[key]; ok {
			t.Errorf("settings rewrite still contains obsolete %q", key)
		}
	}
	if covenant, ok := got["covenant_file"].(string); !ok || covenant == "" {
		t.Errorf("supported covenant_file was not preserved: %#v", got["covenant_file"])
	}
	for key, want := range map[string]interface{}{
		"env_file":  "/project/.lingtai/.env",
		"venv_path": "/project/.lingtai/runtime/venv",
		"pad":       "keep this pad",
	} {
		if got[key] != want {
			t.Errorf("%s = %#v, want %#v", key, got[key], want)
		}
	}
	userConfig, ok := got["user_config"].(map[string]interface{})
	if !ok || userConfig["keep"] != "me" {
		t.Errorf("unrelated user_config was not preserved: %#v", got["user_config"])
	}
	return got
}

func TestSettingsSaveAgentNameStripsObsoletePromptFiles(t *testing.T) {
	orchDir := t.TempDir()
	path := writeLegacyPromptFieldsInit(t, orchDir)
	m := SettingsModel{orchDir: orchDir, agentName: "new-name"}

	m.saveAgentName()
	got := assertLegacyPromptFieldsStripped(t, path)
	if got["covenant_file"] != "/project/.recipe/covenant.md" {
		t.Errorf("saveAgentName changed supported covenant_file: %#v", got["covenant_file"])
	}
	manifest, ok := got["manifest"].(map[string]interface{})
	if !ok || manifest["agent_name"] != "new-name" || manifest["user_flag"] != true {
		t.Errorf("manifest rewrite lost supported/unrelated fields: %#v", got["manifest"])
	}
}

func TestSettingsSaveAgentLangStripsObsoletePromptFiles(t *testing.T) {
	orchDir := t.TempDir()
	path := writeLegacyPromptFieldsInit(t, orchDir)
	m := SettingsModel{orchDir: orchDir, globalDir: t.TempDir()}

	m.saveAgentLang("zh")
	got := assertLegacyPromptFieldsStripped(t, path)
	manifest, ok := got["manifest"].(map[string]interface{})
	if !ok || manifest["language"] != "zh" || manifest["user_flag"] != true {
		t.Errorf("manifest rewrite lost supported/unrelated fields: %#v", got["manifest"])
	}
}
