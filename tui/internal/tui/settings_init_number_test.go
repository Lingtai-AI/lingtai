package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const exactCapabilityNumber = "9007199254740993"

func writeSettingsInit(t *testing.T, orchDir string) string {
	t.Helper()
	path := filepath.Join(orchDir, "init.json")
	content := `{"manifest":{"agent_name":"old","language":"en","capabilities":{"shell":{"tenant_id":9007199254740993}}},"covenant":"old","principle":"old"}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertSettingsNumberPreserved(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"tenant_id": `+exactCapabilityNumber) {
		t.Fatalf("init.json rounded capability number: %s", data)
	}
}

func TestSettingsSaveAgentNamePreservesExactCapabilityNumber(t *testing.T) {
	orchDir := t.TempDir()
	path := writeSettingsInit(t, orchDir)
	m := SettingsModel{orchDir: orchDir, agentName: "new-name"}

	m.saveAgentName()
	assertSettingsNumberPreserved(t, path)
}

func TestSettingsSaveAgentLangPreservesExactCapabilityNumber(t *testing.T) {
	orchDir := t.TempDir()
	path := writeSettingsInit(t, orchDir)
	m := SettingsModel{orchDir: orchDir, globalDir: t.TempDir()}

	m.saveAgentLang("zh")
	assertSettingsNumberPreserved(t, path)
}

func TestSettingsCanonicalAgentAndPsycheOwners(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "init.json")
	raw := `{"agent":{"name":"old","language":"en","permissions":{"karma":true}},"llm":{"preset":{"active":"a","default":"a","allowed":["a"]}},"tools":{"shell":{"tenant_id":9007199254740993}},"mcp":{},"plugins":[]}`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "settings"), 0755); err != nil {
		t.Fatal(err)
	}
	psyche := filepath.Join(dir, "settings", "psyche.json")
	if err := os.WriteFile(psyche, []byte(`{"schema_version":1,"base_prompt":"keep","covenant":"old"}`), 0600); err != nil {
		t.Fatal(err)
	}
	m := SettingsModel{orchDir: dir, globalDir: t.TempDir(), agentName: "new"}
	m.saveAgentName()
	m.saveAgentLang("zh")
	assertSettingsNumberPreserved(t, path)
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"name": "new"`) || !strings.Contains(string(data), `"language": "zh"`) || strings.Contains(string(data), `"manifest"`) || strings.Contains(string(data), `"covenant_file"`) {
		t.Fatalf("wrong canonical owners: %s", data)
	}
	owner, _ := os.ReadFile(psyche)
	if !strings.Contains(string(owner), `"base_prompt": "keep"`) || !strings.Contains(string(owner), `"covenant_file"`) || strings.Contains(string(owner), `"covenant":`) {
		t.Fatalf("Psyche owner lost or wrong covenant: %s", owner)
	}
}

func TestSettingsCanonicalMalformedPsycheRefusesWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "init.json")
	original := []byte(`{"agent":{"language":"en"},"llm":{"provider":"openai","model":"fixture"},"tools":{}}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "settings"), 0755); err != nil {
		t.Fatal(err)
	}
	psyche := filepath.Join(dir, "settings", "psyche.json")
	bad := []byte(`{"schema_version":1,"covenant":"first","covenant":"second"}`)
	if err := os.WriteFile(psyche, bad, 0600); err != nil {
		t.Fatal(err)
	}
	m := SettingsModel{orchDir: dir, globalDir: t.TempDir()}
	m.saveAgentLang("zh")
	for path, want := range map[string][]byte{path: original, psyche: bad} {
		got, _ := os.ReadFile(path)
		if string(got) != string(want) {
			t.Fatal("malformed Psyche owner caused partial replacement")
		}
	}
}
