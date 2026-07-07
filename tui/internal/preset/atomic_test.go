package preset

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAtomicWriteFileReplaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "init.json")
	if err := os.WriteFile(path, []byte("OLD"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := atomicWriteFile(path, []byte("NEW"), 0o644); err != nil {
		t.Fatalf("atomicWriteFile: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "NEW" {
		t.Fatalf("content = %q, want NEW", got)
	}
	// No unique temp sidecar must survive a successful write.
	assertNoAtomicTemp(t, dir, path)
}

// If the write cannot complete, the original file must remain intact — the
// whole point of the pattern is that a failed write never destroys the existing
// config.
func TestAtomicWriteFileFailureLeavesOriginal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "init.json")
	if err := os.WriteFile(path, []byte("OLD"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Force the rename to fail by turning the target into a non-empty directory:
	// os.Rename(tmp -> non-empty dir) fails after the temp file was written, so
	// the original directory content is left intact and the temp is cleaned up.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "keep"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := atomicWriteFile(path, []byte("NEW"), 0o644); err == nil {
		t.Fatal("expected error when target path cannot be replaced")
	}
	if _, err := os.Stat(filepath.Join(path, "keep")); err != nil {
		t.Fatalf("original clobbered: %v", err)
	}
	assertNoAtomicTemp(t, dir, path)
}

func assertNoAtomicTemp(t *testing.T, dir, path string) {
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
