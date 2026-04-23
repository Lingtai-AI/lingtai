package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// loadInit reads an init.json written during a test and returns the library
// capability entry from manifest.capabilities.
func loadInit(t *testing.T, path string) interface{} {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var d map[string]interface{}
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	m, ok := d["manifest"].(map[string]interface{})
	if !ok {
		return nil
	}
	c, ok := m["capabilities"].(map[string]interface{})
	if !ok {
		return nil
	}
	return c["library"]
}

// writeInit writes an init.json with the given library capability value
// into lingtaiDir/<agent>/init.json. `library` may be nil (key absent),
// an empty map, or a populated map. Other fields are minimal so the
// migration focus stays on library.paths handling.
func writeInit(t *testing.T, dir, agent string, library interface{}, omitKey bool) string {
	t.Helper()
	caps := map[string]interface{}{
		"bash": map[string]interface{}{},
	}
	if !omitKey {
		caps["library"] = library
	}
	d := map[string]interface{}{
		"manifest": map[string]interface{}{
			"agent_name":   agent,
			"capabilities": caps,
		},
	}
	agentDir := filepath.Join(dir, agent)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", agentDir, err)
	}
	path := filepath.Join(agentDir, "init.json")
	data, _ := json.MarshalIndent(d, "", "  ")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestMigrateLibraryPaths_EmptyMap(t *testing.T) {
	dir := t.TempDir()
	path := writeInit(t, dir, "alice", map[string]interface{}{}, false)

	t.Setenv("HOME", t.TempDir()) // isolate from real ~/.lingtai-tui/presets
	if err := migrateLibraryPaths(dir); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	lib, _ := loadInit(t, path).(map[string]interface{})
	if lib == nil {
		t.Fatalf("library should be a map, got %#v", loadInit(t, path))
	}
	paths, _ := lib["paths"].([]interface{})
	if len(paths) != 2 || paths[0] != "../.library_shared" || paths[1] != "~/.lingtai-tui/utilities" {
		t.Fatalf("expected defaults merged, got %#v", paths)
	}
}

func TestMigrateLibraryPaths_Null(t *testing.T) {
	dir := t.TempDir()
	path := writeInit(t, dir, "bob", nil, false)

	t.Setenv("HOME", t.TempDir()) // isolate from real ~/.lingtai-tui/presets
	if err := migrateLibraryPaths(dir); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	lib, _ := loadInit(t, path).(map[string]interface{})
	if lib == nil {
		t.Fatalf("library should become a map after migration, got %#v", loadInit(t, path))
	}
	paths, _ := lib["paths"].([]interface{})
	if len(paths) != 2 {
		t.Fatalf("expected 2 defaults, got %#v", paths)
	}
}

func TestMigrateLibraryPaths_MissingKey(t *testing.T) {
	dir := t.TempDir()
	path := writeInit(t, dir, "carol", nil, true)

	t.Setenv("HOME", t.TempDir()) // isolate from real ~/.lingtai-tui/presets
	if err := migrateLibraryPaths(dir); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Library key should remain absent — migration only touches agents
	// that have already opted in.
	if loadInit(t, path) != nil {
		t.Fatalf("expected library key absent, got %#v", loadInit(t, path))
	}
}

func TestMigrateLibraryPaths_MergesUserPaths(t *testing.T) {
	dir := t.TempDir()
	path := writeInit(t, dir, "dave", map[string]interface{}{
		"paths": []interface{}{"/Users/dave/my-skills", "../.library_shared"},
	}, false)

	t.Setenv("HOME", t.TempDir()) // isolate from real ~/.lingtai-tui/presets
	if err := migrateLibraryPaths(dir); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	lib, _ := loadInit(t, path).(map[string]interface{})
	paths, _ := lib["paths"].([]interface{})
	if len(paths) != 3 {
		t.Fatalf("expected 3 paths (2 existing + 1 new default), got %d: %#v", len(paths), paths)
	}
	// User path first, existing default second, missing default appended.
	expected := []interface{}{"/Users/dave/my-skills", "../.library_shared", "~/.lingtai-tui/utilities"}
	for i, p := range expected {
		if paths[i] != p {
			t.Fatalf("paths[%d] = %v, want %v", i, paths[i], p)
		}
	}
}

func TestMigrateLibraryPaths_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := writeInit(t, dir, "eve", map[string]interface{}{}, false)

	t.Setenv("HOME", t.TempDir()) // isolate from real ~/.lingtai-tui/presets
	if err := migrateLibraryPaths(dir); err != nil {
		t.Fatalf("first run: %v", err)
	}
	firstPass, _ := os.ReadFile(path)

	t.Setenv("HOME", t.TempDir()) // isolate from real ~/.lingtai-tui/presets
	if err := migrateLibraryPaths(dir); err != nil {
		t.Fatalf("second run: %v", err)
	}
	secondPass, _ := os.ReadFile(path)

	if string(firstPass) != string(secondPass) {
		t.Fatalf("migration not idempotent:\nfirst:\n%s\nsecond:\n%s", firstPass, secondPass)
	}
}

func TestMigrateLibraryPaths_RewritesPresets(t *testing.T) {
	// Fake $HOME so the migration's presets walk targets a controlled dir.
	home := t.TempDir()
	t.Setenv("HOME", home)

	presetsDir := filepath.Join(home, ".lingtai-tui", "presets")
	if err := os.MkdirAll(presetsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Preset with bare library: {} — classic stale cache case.
	presetPath := filepath.Join(presetsDir, "minimax.json")
	presetData := `{
  "name": "minimax",
  "manifest": {
    "capabilities": {
      "bash": {},
      "library": {}
    }
  }
}`
	if err := os.WriteFile(presetPath, []byte(presetData), 0o644); err != nil {
		t.Fatal(err)
	}

	// Empty lingtaiDir — forces the migration to focus on the preset scope.
	if err := migrateLibraryPaths(t.TempDir()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	data, _ := os.ReadFile(presetPath)
	var p map[string]interface{}
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatalf("unmarshal preset: %v", err)
	}
	caps := p["manifest"].(map[string]interface{})["capabilities"].(map[string]interface{})
	lib, _ := caps["library"].(map[string]interface{})
	if lib == nil {
		t.Fatalf("library missing after migration: %#v", caps["library"])
	}
	paths, _ := lib["paths"].([]interface{})
	if len(paths) != 2 || paths[0] != "../.library_shared" || paths[1] != "~/.lingtai-tui/utilities" {
		t.Fatalf("expected defaults merged into preset, got %#v", paths)
	}
}

func TestMigrateLibraryPaths_SkipsPresetWithoutLibraryKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	presetsDir := filepath.Join(home, ".lingtai-tui", "presets")
	if err := os.MkdirAll(presetsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	presetPath := filepath.Join(presetsDir, "custom.json")
	presetData := `{"name":"custom","manifest":{"capabilities":{"bash":{}}}}`
	if err := os.WriteFile(presetPath, []byte(presetData), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := migrateLibraryPaths(t.TempDir()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	data, _ := os.ReadFile(presetPath)
	if string(data) != presetData {
		t.Fatalf("preset without library key was modified: %s", data)
	}
}

func TestMigrateLibraryPaths_SkipsHiddenAndHuman(t *testing.T) {
	dir := t.TempDir()
	// human folder — no init.json anyway, but make sure walker doesn't crash.
	if err := os.MkdirAll(filepath.Join(dir, "human"), 0o755); err != nil {
		t.Fatal(err)
	}
	// hidden dir with init.json — should be skipped
	hidden := filepath.Join(dir, ".library")
	if err := os.MkdirAll(hidden, 0o755); err != nil {
		t.Fatal(err)
	}
	hiddenInit := filepath.Join(hidden, "init.json")
	if err := os.WriteFile(hiddenInit, []byte(`{"manifest":{"capabilities":{"library":{}}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", t.TempDir()) // isolate from real ~/.lingtai-tui/presets
	if err := migrateLibraryPaths(dir); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Hidden dir's init.json should be untouched (no paths injected).
	data, _ := os.ReadFile(hiddenInit)
	if string(data) != `{"manifest":{"capabilities":{"library":{}}}}` {
		t.Fatalf("hidden dir init.json was modified: %s", data)
	}
}
