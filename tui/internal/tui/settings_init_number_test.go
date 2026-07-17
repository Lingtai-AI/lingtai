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
