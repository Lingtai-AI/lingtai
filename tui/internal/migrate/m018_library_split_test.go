package migrate

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMigrateLibrarySplit_RenamesWithSymlinks verifies the v18 migration:
// - real skill folders survive the rename from .library/ to .library_shared/
// - TUI-managed symlinks inside .library/ are stripped before the rename
func TestMigrateLibrarySplit_RenamesWithSymlinks(t *testing.T) {
	root := t.TempDir()
	lingtaiDir := filepath.Join(root, ".lingtai")
	oldLibrary := filepath.Join(root, ".library")
	newLibrary := filepath.Join(root, ".library_shared")

	if err := os.MkdirAll(filepath.Join(oldLibrary, "real-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	skillMD := filepath.Join(oldLibrary, "real-skill", "SKILL.md")
	if err := os.WriteFile(skillMD, []byte("---\nname: real-skill\ndescription: survives\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A TUI-managed symlink that must be stripped.
	if err := os.Symlink(filepath.Join(oldLibrary, "real-skill"), filepath.Join(oldLibrary, "alias")); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(lingtaiDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := migrateLibrarySplit(lingtaiDir); err != nil {
		t.Fatalf("migrateLibrarySplit failed: %v", err)
	}

	// Old path must be gone.
	if _, err := os.Stat(oldLibrary); !os.IsNotExist(err) {
		t.Errorf("expected %s to be renamed away, still exists", oldLibrary)
	}
	// New path must have the real skill.
	if _, err := os.Stat(filepath.Join(newLibrary, "real-skill", "SKILL.md")); err != nil {
		t.Errorf("expected real-skill/SKILL.md under %s: %v", newLibrary, err)
	}
	// Symlink must be gone.
	if _, err := os.Lstat(filepath.Join(newLibrary, "alias")); !os.IsNotExist(err) {
		t.Errorf("expected symlink alias to be stripped, still exists")
	}
}

// TestMigrateLibrarySplit_FreshNetwork verifies the migration creates
// .library_shared/ on a network that never had a .library/.
func TestMigrateLibrarySplit_FreshNetwork(t *testing.T) {
	root := t.TempDir()
	lingtaiDir := filepath.Join(root, ".lingtai")
	newLibrary := filepath.Join(root, ".library_shared")

	if err := os.MkdirAll(lingtaiDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := migrateLibrarySplit(lingtaiDir); err != nil {
		t.Fatalf("migrateLibrarySplit failed: %v", err)
	}

	if _, err := os.Stat(newLibrary); err != nil {
		t.Errorf("expected %s to be created on fresh network: %v", newLibrary, err)
	}
}

// TestMigrateLibrarySplit_BothExist verifies that if .library_shared/ already
// exists, we only strip symlinks from .library/ and do not clobber the new one.
func TestMigrateLibrarySplit_BothExist(t *testing.T) {
	root := t.TempDir()
	lingtaiDir := filepath.Join(root, ".lingtai")
	oldLibrary := filepath.Join(root, ".library")
	newLibrary := filepath.Join(root, ".library_shared")

	if err := os.MkdirAll(filepath.Join(oldLibrary, "real-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(newLibrary, "existing-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(oldLibrary, "real-skill"), filepath.Join(oldLibrary, "alias")); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(lingtaiDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := migrateLibrarySplit(lingtaiDir); err != nil {
		t.Fatalf("migrateLibrarySplit failed: %v", err)
	}

	// Old path should still exist (not renamed, to avoid clobbering).
	if _, err := os.Stat(oldLibrary); err != nil {
		t.Errorf("expected old .library/ to remain: %v", err)
	}
	// Existing new path should be untouched.
	if _, err := os.Stat(filepath.Join(newLibrary, "existing-skill")); err != nil {
		t.Errorf("expected existing-skill to remain under %s: %v", newLibrary, err)
	}
	// Symlink in old path should still be stripped.
	if _, err := os.Lstat(filepath.Join(oldLibrary, "alias")); !os.IsNotExist(err) {
		t.Errorf("expected symlink alias to be stripped even in both-exist case")
	}
}
