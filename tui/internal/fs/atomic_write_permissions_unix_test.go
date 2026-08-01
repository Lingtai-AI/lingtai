//go:build !windows

package fs

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

const atomicWriteUmaskChildEnv = "LINGTAI_ATOMIC_WRITE_UMASK_CHILD_DIR"

func TestAtomicReplacementPreservesRestrictiveExistingMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
		t.Fatalf("seed canonical file: %v", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("enforce restrictive canonical mode: %v", err)
	}

	if err := writeAtomicBytes(path, []byte("new\n"), 0o644); err != nil {
		t.Fatalf("atomic replacement: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read replaced canonical file: %v", err)
	}
	if string(got) != "new\n" {
		t.Fatalf("canonical bytes = %q, want %q", got, "new\\n")
	}
	assertNoAtomicWriteTemps(t, path)
	assertAtomicWriteMode(t, path, 0o600)
}

func TestAtomicReplacementRespectsCreationUmask(t *testing.T) {
	dir := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	cmd := exec.Command(executable, "-test.run=^TestAtomicReplacementUmaskChild$", "-test.v")
	cmd.Env = append(os.Environ(), atomicWriteUmaskChildEnv+"="+dir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("umask helper failed: %v\n%s", err, output)
	}
}

func TestAtomicReplacementUmaskChild(t *testing.T) {
	dir := os.Getenv(atomicWriteUmaskChildEnv)
	if dir == "" {
		t.Skip("helper subprocess only")
	}

	oldUmask := syscall.Umask(0o077)
	defer syscall.Umask(oldUmask)

	path := filepath.Join(dir, "new-direct-unread.json")
	if err := writeAtomicBytes(path, []byte("private\n"), 0o644); err != nil {
		t.Fatalf("atomic create under umask 077: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read created canonical file: %v", err)
	}
	if string(got) != "private\n" {
		t.Fatalf("canonical bytes = %q, want %q", got, "private\\n")
	}
	assertNoAtomicWriteTemps(t, path)
	assertAtomicWriteMode(t, path, 0o600)
}

func assertAtomicWriteMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("canonical mode = %04o, want %04o", got, want)
	}
}

func assertNoAtomicWriteTemps(t *testing.T, path string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*"))
	if err != nil {
		t.Fatalf("glob generated atomic temps: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("generated atomic temps remain: %v", matches)
	}
}
