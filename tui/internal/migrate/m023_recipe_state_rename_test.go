package migrate

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMigrateRecipeStateRename_LegacyFileRenamed ensures a legacy
// .tui-asset/.recipe file is renamed to recipe-state.json.
func TestMigrateRecipeStateRename_LegacyFileRenamed(t *testing.T) {
	lingtaiDir := t.TempDir()
	tuiAsset := filepath.Join(lingtaiDir, ".tui-asset")
	if err := os.MkdirAll(tuiAsset, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	legacy := filepath.Join(tuiAsset, ".recipe")
	payload := []byte(`{"recipe":"greeter"}`)
	if err := os.WriteFile(legacy, payload, 0o644); err != nil {
		t.Fatalf("write legacy: %v", err)
	}

	if err := migrateRecipeStateRename(lingtaiDir); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Errorf("legacy .recipe still exists: err=%v", err)
	}
	newPath := filepath.Join(tuiAsset, "recipe-state.json")
	got, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("read new path: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("content = %s, want %s", got, payload)
	}
}

// TestMigrateRecipeStateRename_NoLegacy is a no-op when the legacy file
// doesn't exist.
func TestMigrateRecipeStateRename_NoLegacy(t *testing.T) {
	lingtaiDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(lingtaiDir, ".tui-asset"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := migrateRecipeStateRename(lingtaiDir); err != nil {
		t.Errorf("migrate returned error on no-op: %v", err)
	}
}

// TestMigrateRecipeStateRename_DirectorySkipped leaves a pre-existing
// directory at .tui-asset/.recipe alone (that's a valid applied-recipe
// snapshot under the new naming scheme).
func TestMigrateRecipeStateRename_DirectorySkipped(t *testing.T) {
	lingtaiDir := t.TempDir()
	snapshot := filepath.Join(lingtaiDir, ".tui-asset", ".recipe")
	if err := os.MkdirAll(snapshot, 0o755); err != nil {
		t.Fatalf("mkdir snapshot: %v", err)
	}
	marker := filepath.Join(snapshot, "marker.txt")
	if err := os.WriteFile(marker, []byte("snapshot"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	if err := migrateRecipeStateRename(lingtaiDir); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if _, err := os.Stat(snapshot); err != nil {
		t.Errorf("snapshot directory was removed: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("marker inside snapshot was lost: %v", err)
	}
}

// TestMigrateRecipeStateRename_BothExistPrefersNew prefers recipe-state.json
// if both legacy and new exist (removes the legacy one).
func TestMigrateRecipeStateRename_BothExistPrefersNew(t *testing.T) {
	lingtaiDir := t.TempDir()
	tuiAsset := filepath.Join(lingtaiDir, ".tui-asset")
	if err := os.MkdirAll(tuiAsset, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	legacy := filepath.Join(tuiAsset, ".recipe")
	newPath := filepath.Join(tuiAsset, "recipe-state.json")
	os.WriteFile(legacy, []byte(`{"recipe":"legacy"}`), 0o644)
	os.WriteFile(newPath, []byte(`{"recipe":"new"}`), 0o644)

	if err := migrateRecipeStateRename(lingtaiDir); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Errorf("legacy should have been removed: err=%v", err)
	}
	got, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("read new: %v", err)
	}
	if string(got) != `{"recipe":"new"}` {
		t.Errorf("new path content was overwritten: %s", got)
	}
}
