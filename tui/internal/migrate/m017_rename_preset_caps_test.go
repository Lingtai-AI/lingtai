package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writePresetFile writes a preset JSON with the given capability map into
// ~/.lingtai-tui/presets/<name>.json under the test's fake $HOME.
func writePresetFile(t *testing.T, globalDir, name string, caps map[string]interface{}) {
	t.Helper()
	presetsDir := filepath.Join(globalDir, "presets")
	os.MkdirAll(presetsDir, 0o755)
	preset := map[string]interface{}{
		"name":        name,
		"description": "test",
		"manifest": map[string]interface{}{
			"capabilities": caps,
		},
	}
	data, _ := json.MarshalIndent(preset, "", "  ")
	if err := os.WriteFile(filepath.Join(presetsDir, name+".json"), data, 0o644); err != nil {
		t.Fatalf("write preset: %v", err)
	}
}

// readPresetCaps returns the sorted capability map of a preset file.
func readPresetCaps(t *testing.T, globalDir, name string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(globalDir, "presets", name+".json"))
	if err != nil {
		t.Fatalf("read preset: %v", err)
	}
	var p map[string]interface{}
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatalf("parse preset: %v", err)
	}
	manifest, _ := p["manifest"].(map[string]interface{})
	caps, _ := manifest["capabilities"].(map[string]interface{})
	return caps
}

func TestMigrateRenamePresetCaps_RenamesStaleKeys(t *testing.T) {
	globalDir := withTempHome(t)
	writePresetFile(t, globalDir, "zhipu", map[string]interface{}{
		"file":    map[string]interface{}{},
		"library": map[string]interface{}{"library_limit": 50}, // old knowledge archive
		"skills":  map[string]interface{}{},                    // old skill library
	})

	if err := migrateRenamePresetCaps(t.TempDir()); err != nil {
		t.Fatalf("migrateRenamePresetCaps err = %v", err)
	}

	caps := readPresetCaps(t, globalDir, "zhipu")

	if _, ok := caps["library"].(map[string]interface{}); !ok {
		t.Errorf("expected library (skill library) in caps, got %v", caps)
	}
	if _, ok := caps["codex"]; !ok {
		t.Errorf("expected codex (knowledge archive) in caps, got %v", caps)
	}
	if _, ok := caps["skills"]; ok {
		t.Errorf("did not expect skills key to remain, got %v", caps)
	}

	// Confirm library_limit → codex_limit transform
	codexCfg, _ := caps["codex"].(map[string]interface{})
	if _, hasOld := codexCfg["library_limit"]; hasOld {
		t.Errorf("library_limit should have been renamed, got %v", codexCfg)
	}
	if _, hasNew := codexCfg["codex_limit"]; !hasNew {
		t.Errorf("expected codex_limit, got %v", codexCfg)
	}
}

func TestMigrateRenamePresetCaps_Idempotent(t *testing.T) {
	globalDir := withTempHome(t)
	// Already-migrated preset — has codex + library (new-world names).
	writePresetFile(t, globalDir, "zhipu", map[string]interface{}{
		"codex":   map[string]interface{}{},
		"library": map[string]interface{}{},
	})

	if err := migrateRenamePresetCaps(t.TempDir()); err != nil {
		t.Fatalf("err = %v", err)
	}

	caps := readPresetCaps(t, globalDir, "zhipu")
	if _, ok := caps["codex"]; !ok {
		t.Errorf("codex key should remain, got %v", caps)
	}
	if _, ok := caps["library"]; !ok {
		t.Errorf("library key should remain, got %v", caps)
	}
	if len(caps) != 2 {
		t.Errorf("expected exactly 2 caps, got %v", caps)
	}
}

func TestMigrateRenamePresetCaps_NoPresetsDir(t *testing.T) {
	withTempHome(t) // creates globalDir but no presets subdir
	if err := migrateRenamePresetCaps(t.TempDir()); err != nil {
		t.Fatalf("err = %v", err)
	}
}

func TestMigrateRenamePresetCaps_IgnoresCorruptAndNonJSON(t *testing.T) {
	globalDir := withTempHome(t)
	presetsDir := filepath.Join(globalDir, "presets")
	os.MkdirAll(presetsDir, 0o755)
	os.WriteFile(filepath.Join(presetsDir, "corrupt.json"), []byte("not json {"), 0o644)
	os.WriteFile(filepath.Join(presetsDir, "readme.txt"), []byte("ignored"), 0o644)
	writePresetFile(t, globalDir, "zhipu", map[string]interface{}{
		"library": map[string]interface{}{},
	})

	if err := migrateRenamePresetCaps(t.TempDir()); err != nil {
		t.Fatalf("err = %v", err)
	}

	caps := readPresetCaps(t, globalDir, "zhipu")
	if _, ok := caps["codex"]; !ok {
		t.Errorf("expected codex in zhipu caps after migration, got %v", caps)
	}
}
