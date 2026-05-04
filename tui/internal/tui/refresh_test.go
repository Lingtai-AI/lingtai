package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestResetActivePresetToDefault_RewritesActive verifies that when
// active != default, the helper rewrites active to match default.
func TestResetActivePresetToDefault_RewritesActive(t *testing.T) {
	dir := t.TempDir()
	initJSON := map[string]interface{}{
		"manifest": map[string]interface{}{
			"agent_name": "test",
			"preset": map[string]interface{}{
				"active":  "~/.lingtai-tui/presets/saved/zhipu-1.json",
				"default": "~/.lingtai-tui/presets/templates/minimax.json",
				"allowed": []interface{}{
					"~/.lingtai-tui/presets/saved/zhipu-1.json",
					"~/.lingtai-tui/presets/templates/minimax.json",
				},
			},
		},
	}
	writeJSON(t, filepath.Join(dir, "init.json"), initJSON)

	resetActivePresetToDefault(dir)

	got := readJSON(t, filepath.Join(dir, "init.json"))
	pre := got["manifest"].(map[string]interface{})["preset"].(map[string]interface{})
	if active := pre["active"]; active != "~/.lingtai-tui/presets/templates/minimax.json" {
		t.Errorf("active = %v, want minimax (the default)", active)
	}
	// default and allowed must be untouched
	if def := pre["default"]; def != "~/.lingtai-tui/presets/templates/minimax.json" {
		t.Errorf("default mutated: %v", def)
	}
	allowed := pre["allowed"].([]interface{})
	if len(allowed) != 2 {
		t.Errorf("allowed length changed: %d", len(allowed))
	}
}

// TestResetActivePresetToDefault_NoOpWhenAlreadyDefault verifies that
// when active == default, the helper is a no-op (no spurious rewrite).
func TestResetActivePresetToDefault_NoOpWhenAlreadyDefault(t *testing.T) {
	dir := t.TempDir()
	ref := "~/.lingtai-tui/presets/templates/minimax.json"
	initJSON := map[string]interface{}{
		"manifest": map[string]interface{}{
			"preset": map[string]interface{}{
				"active":  ref,
				"default": ref,
				"allowed": []interface{}{ref},
			},
		},
	}
	path := filepath.Join(dir, "init.json")
	writeJSON(t, path, initJSON)

	beforeStat, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	resetActivePresetToDefault(dir)

	got := readJSON(t, path)
	pre := got["manifest"].(map[string]interface{})["preset"].(map[string]interface{})
	if active := pre["active"]; active != ref {
		t.Errorf("active = %v, want %s", active, ref)
	}
	// File should not have been rewritten — modtime should be unchanged.
	afterStat, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !beforeStat.ModTime().Equal(afterStat.ModTime()) {
		t.Errorf("file rewritten on no-op path; modtime changed")
	}
}

// TestResetActivePresetToDefault_MissingPresetBlock verifies the helper
// silently skips when the preset block is absent (older agents, partial
// init.json). This must not panic and must not corrupt the file.
func TestResetActivePresetToDefault_MissingPresetBlock(t *testing.T) {
	dir := t.TempDir()
	initJSON := map[string]interface{}{
		"manifest": map[string]interface{}{
			"agent_name": "test",
		},
	}
	path := filepath.Join(dir, "init.json")
	writeJSON(t, path, initJSON)

	resetActivePresetToDefault(dir) // must not panic

	got := readJSON(t, path)
	if name := got["manifest"].(map[string]interface{})["agent_name"]; name != "test" {
		t.Errorf("agent_name corrupted: %v", name)
	}
}

// TestResetActivePresetToDefault_MalformedJSON verifies the helper
// silently skips when init.json is unparseable (rather than panic or
// truncate the file).
func TestResetActivePresetToDefault_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "init.json")
	if err := os.WriteFile(path, []byte("not valid json {"), 0o644); err != nil {
		t.Fatal(err)
	}

	resetActivePresetToDefault(dir) // must not panic

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "not valid json {" {
		t.Errorf("file mutated despite parse failure: %q", string(data))
	}
}

func writeJSON(t *testing.T, path string, v interface{}) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readJSON(t *testing.T, path string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}
