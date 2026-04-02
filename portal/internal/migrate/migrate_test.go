package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRunFreshInstall(t *testing.T) {
	// Fresh .lingtai/ with no meta.json → should create meta.json at CurrentVersion
	dir := t.TempDir()
	lingtaiDir := filepath.Join(dir, ".lingtai")
	os.MkdirAll(lingtaiDir, 0o755)

	if err := Run(lingtaiDir); err != nil {
		t.Fatalf("Run() failed: %v", err)
	}

	meta := readMeta(t, lingtaiDir)
	if meta.Version != CurrentVersion {
		t.Errorf("expected version %d, got %d", CurrentVersion, meta.Version)
	}
}

func TestRunAlreadyCurrent(t *testing.T) {
	dir := t.TempDir()
	lingtaiDir := filepath.Join(dir, ".lingtai")
	os.MkdirAll(lingtaiDir, 0o755)

	// Write meta at current version
	writeMeta(t, lingtaiDir, CurrentVersion)

	if err := Run(lingtaiDir); err != nil {
		t.Fatalf("Run() failed: %v", err)
	}

	meta := readMeta(t, lingtaiDir)
	if meta.Version != CurrentVersion {
		t.Errorf("expected version %d, got %d", CurrentVersion, meta.Version)
	}
}

func TestRunRejectsTooNew(t *testing.T) {
	dir := t.TempDir()
	lingtaiDir := filepath.Join(dir, ".lingtai")
	os.MkdirAll(lingtaiDir, 0o755)

	writeMeta(t, lingtaiDir, CurrentVersion+1)

	err := Run(lingtaiDir)
	if err == nil {
		t.Fatal("expected error for too-new version, got nil")
	}
}

func TestMigrateTopologyToPortal(t *testing.T) {
	dir := t.TempDir()
	lingtaiDir := filepath.Join(dir, ".lingtai")

	// Set up old topology file
	oldDir := filepath.Join(lingtaiDir, ".tui-asset")
	os.MkdirAll(oldDir, 0o755)
	oldPath := filepath.Join(oldDir, "topology.jsonl")
	content := []byte("{\"t\":1000,\"net\":{}}\n")
	os.WriteFile(oldPath, content, 0o644)

	if err := Run(lingtaiDir); err != nil {
		t.Fatalf("Run() failed: %v", err)
	}

	// Old file should be gone
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Error("old topology.jsonl should have been moved")
	}

	// New file should exist with same content
	newPath := filepath.Join(lingtaiDir, ".portal", "topology.jsonl")
	got, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("new topology.jsonl not found: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("content mismatch: got %q, want %q", got, content)
	}

	meta := readMeta(t, lingtaiDir)
	if meta.Version != CurrentVersion {
		t.Errorf("expected version %d, got %d", CurrentVersion, meta.Version)
	}
}

func TestMigrateTopologyNoOldFile(t *testing.T) {
	// Fresh install — no .tui-asset/topology.jsonl → migration should no-op
	dir := t.TempDir()
	lingtaiDir := filepath.Join(dir, ".lingtai")
	os.MkdirAll(lingtaiDir, 0o755)

	if err := Run(lingtaiDir); err != nil {
		t.Fatalf("Run() failed: %v", err)
	}

	meta := readMeta(t, lingtaiDir)
	if meta.Version != CurrentVersion {
		t.Errorf("expected version %d, got %d", CurrentVersion, meta.Version)
	}
}

// helpers

func readMeta(t *testing.T, lingtaiDir string) metaFile {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(lingtaiDir, "meta.json"))
	if err != nil {
		t.Fatalf("read meta.json: %v", err)
	}
	var m metaFile
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse meta.json: %v", err)
	}
	return m
}

func writeMeta(t *testing.T, lingtaiDir string, version int) {
	t.Helper()
	data, _ := json.Marshal(metaFile{Version: version})
	if err := os.WriteFile(filepath.Join(lingtaiDir, "meta.json"), data, 0o644); err != nil {
		t.Fatalf("write meta.json: %v", err)
	}
}
