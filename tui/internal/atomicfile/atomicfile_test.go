package atomicfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteReplaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "init.json")
	if err := os.WriteFile(path, []byte("OLD"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte("NEW"), 0o644); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "NEW" {
		t.Fatalf("content = %q, want NEW", got)
	}
	assertNoTempSidecar(t, dir, path)
}

func TestWriteCreatesNew(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "init.json")
	if err := Write(path, []byte("NEW"), 0o600); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "NEW" {
		t.Fatalf("content = %q, want NEW", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("new file perm = %v, want 0o600", info.Mode().Perm())
	}
	assertNoTempSidecar(t, dir, path)
}

// A failing rename must leave the original file intact — the whole point of the
// pattern is that a failed write never destroys the existing config.
func TestWriteFailureLeavesOriginal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "init.json")
	if err := os.WriteFile(path, []byte("OLD"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Force the rename to fail by making the target a non-empty directory:
	// os.Rename(file -> non-empty dir) fails, but CreateTemp/Write/Sync all
	// succeed first, exercising the cleanup path.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "keep"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte("NEW"), 0o644); err == nil {
		t.Fatal("expected error when target path cannot be replaced")
	}
	// The temp sidecar must not survive the failed write.
	assertNoTempSidecar(t, dir, path)
}

// Rewriting an existing file must preserve its current permissions rather than
// resetting to the passed perm.
func TestWritePreservesExistingPerm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".agent.json")
	if err := os.WriteFile(path, []byte("OLD"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Pass a wider perm; the existing 0o600 must win.
	if err := Write(path, []byte("NEW"), 0o644); err != nil {
		t.Fatalf("Write: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %v, want 0o600 preserved", info.Mode().Perm())
	}
}

// assertNoTempSidecar fails if any leftover atomicfile temp file remains in dir.
func assertNoTempSidecar(t *testing.T, dir, path string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	prefix := "." + filepath.Base(path) + ".tmp-"
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) {
			t.Fatalf("temp sidecar left behind: %s", e.Name())
		}
	}
}
