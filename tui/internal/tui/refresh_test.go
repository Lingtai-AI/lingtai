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

// TestReadAllowedPresets_HappyPath verifies the helper extracts the
// allowed list from a well-formed init.json.
func TestReadAllowedPresets_HappyPath(t *testing.T) {
	dir := t.TempDir()
	initJSON := map[string]interface{}{
		"manifest": map[string]interface{}{
			"preset": map[string]interface{}{
				"active":  "~/.lingtai-tui/presets/saved/zhipu-1.json",
				"default": "~/.lingtai-tui/presets/templates/minimax.json",
				"allowed": []interface{}{
					"~/.lingtai-tui/presets/templates/minimax.json",
					"~/.lingtai-tui/presets/saved/zhipu-1.json",
				},
			},
		},
	}
	writeJSON(t, filepath.Join(dir, "init.json"), initJSON)

	got := readAllowedPresets(dir)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %v", len(got), got)
	}
	if got[0] != "~/.lingtai-tui/presets/templates/minimax.json" {
		t.Errorf("got[0] = %q", got[0])
	}
}

// TestReadAllowedPresets_Missing returns nil for missing files /
// missing preset blocks rather than panicking.
func TestReadAllowedPresets_Missing(t *testing.T) {
	dir := t.TempDir()
	if got := readAllowedPresets(dir); got != nil {
		t.Errorf("missing init.json → got %v, want nil", got)
	}

	// init.json with no preset block.
	writeJSON(t, filepath.Join(dir, "init.json"), map[string]interface{}{
		"manifest": map[string]interface{}{"agent_name": "test"},
	})
	if got := readAllowedPresets(dir); got != nil {
		t.Errorf("missing preset block → got %v, want nil", got)
	}
}

// TestResolvePresetInAllowed_StemMatch verifies that a bare preset
// name resolves to the matching full path.
func TestResolvePresetInAllowed_StemMatch(t *testing.T) {
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, "init.json"), map[string]interface{}{
		"manifest": map[string]interface{}{
			"preset": map[string]interface{}{
				"allowed": []interface{}{
					"~/.lingtai-tui/presets/templates/minimax.json",
					"~/.lingtai-tui/presets/saved/zhipu-1.json",
				},
			},
		},
	})

	got, err := resolvePresetInAllowed(dir, "zhipu-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "~/.lingtai-tui/presets/saved/zhipu-1.json" {
		t.Errorf("got %q", got)
	}
}

// TestResolvePresetInAllowed_ExactPath verifies that the full path
// form also resolves.
func TestResolvePresetInAllowed_ExactPath(t *testing.T) {
	dir := t.TempDir()
	ref := "~/.lingtai-tui/presets/templates/minimax.json"
	writeJSON(t, filepath.Join(dir, "init.json"), map[string]interface{}{
		"manifest": map[string]interface{}{
			"preset": map[string]interface{}{
				"allowed": []interface{}{ref},
			},
		},
	})

	got, err := resolvePresetInAllowed(dir, ref)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != ref {
		t.Errorf("got %q, want %q", got, ref)
	}
}

// TestResolvePresetInAllowed_NotInAllowed verifies the error message
// names the unknown preset and lists what's available.
func TestResolvePresetInAllowed_NotInAllowed(t *testing.T) {
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, "init.json"), map[string]interface{}{
		"manifest": map[string]interface{}{
			"preset": map[string]interface{}{
				"allowed": []interface{}{
					"~/.lingtai-tui/presets/templates/minimax.json",
				},
			},
		},
	})

	_, err := resolvePresetInAllowed(dir, "ghost-preset")
	if err == nil {
		t.Fatal("expected error for non-allowed preset")
	}
	msg := err.Error()
	if !contains(msg, "ghost-preset") {
		t.Errorf("error %q missing requested name", msg)
	}
	if !contains(msg, "minimax") {
		t.Errorf("error %q missing list of available stems", msg)
	}
}

// TestResolvePresetInAllowed_AmbiguousStem verifies that when two
// allowed entries share a basename, the resolver refuses rather than
// pick arbitrarily.
func TestResolvePresetInAllowed_AmbiguousStem(t *testing.T) {
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, "init.json"), map[string]interface{}{
		"manifest": map[string]interface{}{
			"preset": map[string]interface{}{
				"allowed": []interface{}{
					"~/.lingtai-tui/presets/templates/mimo.json",
					"~/.lingtai-tui/presets/saved/mimo.json",
				},
			},
		},
	})

	_, err := resolvePresetInAllowed(dir, "mimo")
	if err == nil {
		t.Fatal("expected ambiguity error")
	}
	if !contains(err.Error(), "ambiguous") {
		t.Errorf("error %q missing 'ambiguous'", err.Error())
	}
}

// TestSetActivePreset_RewritesActive verifies the writer updates only
// the `active` field, preserving default and allowed.
func TestSetActivePreset_RewritesActive(t *testing.T) {
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, "init.json"), map[string]interface{}{
		"manifest": map[string]interface{}{
			"preset": map[string]interface{}{
				"active":  "~/.lingtai-tui/presets/templates/minimax.json",
				"default": "~/.lingtai-tui/presets/templates/minimax.json",
				"allowed": []interface{}{
					"~/.lingtai-tui/presets/templates/minimax.json",
					"~/.lingtai-tui/presets/saved/zhipu-1.json",
				},
			},
		},
	})

	want := "~/.lingtai-tui/presets/saved/zhipu-1.json"
	if err := setActivePreset(dir, want); err != nil {
		t.Fatalf("setActivePreset: %v", err)
	}

	got := readJSON(t, filepath.Join(dir, "init.json"))
	pre := got["manifest"].(map[string]interface{})["preset"].(map[string]interface{})
	if active := pre["active"]; active != want {
		t.Errorf("active = %v, want %s", active, want)
	}
	if def := pre["default"]; def != "~/.lingtai-tui/presets/templates/minimax.json" {
		t.Errorf("default mutated: %v", def)
	}
	if allowed := pre["allowed"].([]interface{}); len(allowed) != 2 {
		t.Errorf("allowed length changed: %d", len(allowed))
	}
}

// TestReadActivePreset_HappyPath verifies the helper extracts the
// active preset path from a well-formed init.json.
func TestReadActivePreset_HappyPath(t *testing.T) {
	dir := t.TempDir()
	want := "~/.lingtai-tui/presets/saved/zhipu-1.json"
	writeJSON(t, filepath.Join(dir, "init.json"), map[string]interface{}{
		"manifest": map[string]interface{}{
			"preset": map[string]interface{}{
				"active":  want,
				"default": "~/.lingtai-tui/presets/templates/minimax.json",
				"allowed": []interface{}{want},
			},
		},
	})

	if got := readActivePreset(dir); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestReadActivePreset_Missing returns "" for missing files / blocks
// rather than panicking.
func TestReadActivePreset_Missing(t *testing.T) {
	dir := t.TempDir()
	if got := readActivePreset(dir); got != "" {
		t.Errorf("missing init.json → got %q, want \"\"", got)
	}
	writeJSON(t, filepath.Join(dir, "init.json"), map[string]interface{}{
		"manifest": map[string]interface{}{"agent_name": "test"},
	})
	if got := readActivePreset(dir); got != "" {
		t.Errorf("missing preset block → got %q, want \"\"", got)
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
