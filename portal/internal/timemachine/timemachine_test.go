package timemachine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	lingtaiDir := filepath.Join(dir, ".lingtai")
	os.MkdirAll(lingtaiDir, 0o755)

	if err := initGit(lingtaiDir); err != nil {
		t.Fatalf("initGit failed: %v", err)
	}

	// .git should exist
	if _, err := os.Stat(filepath.Join(lingtaiDir, ".git")); err != nil {
		t.Fatal(".git not created")
	}

	// Should have initial commit
	out, err := exec.Command("git", "-C", lingtaiDir, "log", "--oneline", "-1").Output()
	if err != nil {
		t.Fatalf("git log failed: %v", err)
	}
	if !strings.Contains(string(out), "init") {
		t.Errorf("expected initial commit, got: %s", out)
	}
}

func TestInitGitIdempotent(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	lingtaiDir := filepath.Join(dir, ".lingtai")
	os.MkdirAll(lingtaiDir, 0o755)

	initGit(lingtaiDir)
	out1, _ := exec.Command("git", "-C", lingtaiDir, "rev-list", "--count", "HEAD").Output()

	initGit(lingtaiDir)
	out2, _ := exec.Command("git", "-C", lingtaiDir, "rev-list", "--count", "HEAD").Output()

	if string(out1) != string(out2) {
		t.Error("initGit should not create extra commits on re-run")
	}
}

func TestSnapshot(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	lingtaiDir := filepath.Join(dir, ".lingtai")
	os.MkdirAll(lingtaiDir, 0o755)
	initGit(lingtaiDir)

	// Write a file
	os.WriteFile(filepath.Join(lingtaiDir, "test.txt"), []byte("hello"), 0o644)

	committed, err := snapshot(lingtaiDir)
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	if !committed {
		t.Error("expected snapshot to commit changes")
	}

	// Verify commit message format
	out, _ := exec.Command("git", "-C", lingtaiDir, "log", "--oneline", "-1").Output()
	if !strings.Contains(string(out), "snapshot") {
		t.Errorf("expected 'snapshot' in commit message, got: %s", out)
	}
}

func TestSnapshotNoChanges(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	lingtaiDir := filepath.Join(dir, ".lingtai")
	os.MkdirAll(lingtaiDir, 0o755)
	initGit(lingtaiDir)

	committed, err := snapshot(lingtaiDir)
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	if committed {
		t.Error("expected no commit when nothing changed")
	}
}

func TestLargeFileIgnored(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	lingtaiDir := filepath.Join(dir, ".lingtai")
	os.MkdirAll(lingtaiDir, 0o755)

	// Write .gitignore (normally done by migration)
	os.WriteFile(filepath.Join(lingtaiDir, ".gitignore"), []byte("# test\n"), 0o644)
	initGit(lingtaiDir)

	// Create a file just over 10MB
	bigFile := filepath.Join(lingtaiDir, "big.bin")
	f, _ := os.Create(bigFile)
	f.Write(make([]byte, 10*1024*1024+1))
	f.Close()

	scanLargeFiles(lingtaiDir, 10*1024*1024)

	data, _ := os.ReadFile(filepath.Join(lingtaiDir, ".gitignore"))
	if !strings.Contains(string(data), "big.bin") {
		t.Error("large file not added to .gitignore")
	}
}
