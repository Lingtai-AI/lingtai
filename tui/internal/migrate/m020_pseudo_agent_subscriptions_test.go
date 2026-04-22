package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeInitJSON writes an init.json with the given manifest content into
// <lingtaiDir>/<agentName>/init.json.
func writeInitJSON(t *testing.T, lingtaiDir, agentName string, manifest map[string]interface{}) {
	t.Helper()
	agentDir := filepath.Join(lingtaiDir, agentName)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir agent: %v", err)
	}
	init := map[string]interface{}{
		"manifest": manifest,
	}
	data, _ := json.MarshalIndent(init, "", "  ")
	if err := os.WriteFile(filepath.Join(agentDir, "init.json"), data, 0o644); err != nil {
		t.Fatalf("write init.json: %v", err)
	}
}

// readManifestSubscriptions returns the value of
// manifest.pseudo_agent_subscriptions from an agent's init.json, or nil
// if missing.
func readManifestSubscriptions(t *testing.T, lingtaiDir, agentName string) interface{} {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(lingtaiDir, agentName, "init.json"))
	if err != nil {
		t.Fatalf("read init.json: %v", err)
	}
	var init map[string]interface{}
	if err := json.Unmarshal(data, &init); err != nil {
		t.Fatalf("parse init.json: %v", err)
	}
	manifest, _ := init["manifest"].(map[string]interface{})
	return manifest["pseudo_agent_subscriptions"]
}

func TestMigratePseudoAgentSubscriptions_AddsDefault(t *testing.T) {
	dir := t.TempDir()
	writeInitJSON(t, dir, "本我", map[string]interface{}{
		"agent_name": "本我",
		"llm":        map[string]interface{}{"provider": "minimax"},
	})

	if err := migratePseudoAgentSubscriptions(dir); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	got := readManifestSubscriptions(t, dir, "本我")
	gotList, ok := got.([]interface{})
	if !ok {
		t.Fatalf("pseudo_agent_subscriptions not a list, got %T: %v", got, got)
	}
	if len(gotList) != 1 || gotList[0] != "../human" {
		t.Errorf("got %v, want [../human]", gotList)
	}
}

func TestMigratePseudoAgentSubscriptions_PreservesExistingValue(t *testing.T) {
	dir := t.TempDir()
	// Agent already has a custom list — migration must not clobber it.
	writeInitJSON(t, dir, "本我", map[string]interface{}{
		"agent_name":                  "本我",
		"llm":                         map[string]interface{}{"provider": "minimax"},
		"pseudo_agent_subscriptions":  []interface{}{"../human", "../announcements"},
	})

	if err := migratePseudoAgentSubscriptions(dir); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	got := readManifestSubscriptions(t, dir, "本我")
	gotList, ok := got.([]interface{})
	if !ok {
		t.Fatalf("pseudo_agent_subscriptions not a list, got %T: %v", got, got)
	}
	if len(gotList) != 2 || gotList[0] != "../human" || gotList[1] != "../announcements" {
		t.Errorf("got %v, want [../human ../announcements]", gotList)
	}
}

func TestMigratePseudoAgentSubscriptions_SkipsHumanAndDotDirs(t *testing.T) {
	dir := t.TempDir()
	// `human` has no init.json normally — but even if one were dropped
	// there, the migration should not touch it.
	writeInitJSON(t, dir, "human", map[string]interface{}{
		"llm": map[string]interface{}{"provider": "minimax"},
	})
	// `.addons` is an infrastructure dir and must be skipped.
	writeInitJSON(t, dir, ".addons", map[string]interface{}{
		"llm": map[string]interface{}{"provider": "minimax"},
	})

	if err := migratePseudoAgentSubscriptions(dir); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if got := readManifestSubscriptions(t, dir, "human"); got != nil {
		t.Errorf("human should not be migrated, got %v", got)
	}
	if got := readManifestSubscriptions(t, dir, ".addons"); got != nil {
		t.Errorf(".addons should not be migrated, got %v", got)
	}
}

func TestMigratePseudoAgentSubscriptions_HandlesMissingInitJSON(t *testing.T) {
	dir := t.TempDir()
	// Agent dir with no init.json — migration must not fail.
	if err := os.MkdirAll(filepath.Join(dir, "本我"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := migratePseudoAgentSubscriptions(dir); err != nil {
		t.Errorf("migrate should be tolerant of missing init.json, got err: %v", err)
	}
}

func TestMigratePseudoAgentSubscriptions_HandlesEmptyLingtaiDir(t *testing.T) {
	dir := t.TempDir()
	// Empty .lingtai dir — no agents.
	if err := migratePseudoAgentSubscriptions(dir); err != nil {
		t.Errorf("migrate on empty dir should not fail, got err: %v", err)
	}
}
