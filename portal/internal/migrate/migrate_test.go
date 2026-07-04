package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func TestRunPreservesMetaSiblingFields(t *testing.T) {
	dir := t.TempDir()
	lingtaiDir := filepath.Join(dir, ".lingtai")
	os.MkdirAll(lingtaiDir, 0o755)

	writeFile(t, filepath.Join(lingtaiDir, "meta.json"), `{
		"version": 38,
		"addon_comment_cleanup_notified": true,
		"custom_state": {"owner": "tui"},
		"list_state": [1, 2, 3]
	}`)

	if err := Run(lingtaiDir); err != nil {
		t.Fatalf("Run() failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(lingtaiDir, "meta.json"))
	if err != nil {
		t.Fatalf("read meta.json: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parse meta.json as raw map: %v", err)
	}

	var version int
	if err := json.Unmarshal(raw["version"], &version); err != nil {
		t.Fatalf("parse version: %v", err)
	}
	if version != CurrentVersion {
		t.Fatalf("version = %d, want %d", version, CurrentVersion)
	}

	var notified bool
	if err := json.Unmarshal(raw["addon_comment_cleanup_notified"], &notified); err != nil {
		t.Fatalf("parse addon_comment_cleanup_notified: %v", err)
	}
	if !notified {
		t.Fatal("addon_comment_cleanup_notified was not preserved")
	}

	var custom map[string]string
	if err := json.Unmarshal(raw["custom_state"], &custom); err != nil {
		t.Fatalf("parse custom_state: %v", err)
	}
	if custom["owner"] != "tui" {
		t.Fatalf("custom_state = %#v, want owner=tui", custom)
	}

	var list []int
	if err := json.Unmarshal(raw["list_state"], &list); err != nil {
		t.Fatalf("parse list_state: %v", err)
	}
	if len(list) != 3 || list[2] != 3 {
		t.Fatalf("list_state = %#v, want [1 2 3]", list)
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

func TestMigrateRelativeAddressing(t *testing.T) {
	dir := t.TempDir()
	lingtaiDir := filepath.Join(dir, ".lingtai")

	agentDir := filepath.Join(lingtaiDir, "alice")
	os.MkdirAll(filepath.Join(agentDir, "mailbox", "inbox", "msg1"), 0o755)
	os.MkdirAll(filepath.Join(agentDir, "delegates"), 0o755)
	os.MkdirAll(filepath.Join(agentDir, "logs"), 0o755)

	writeFile(t, filepath.Join(agentDir, ".agent.json"),
		`{"agent_name":"alice","address":"`+agentDir+`","state":"ACTIVE"}`)

	writeFile(t, filepath.Join(agentDir, "mailbox", "contacts.json"),
		`[{"address":"`+filepath.Join(lingtaiDir, "bob")+`","name":"bob"}]`)

	writeFile(t, filepath.Join(agentDir, "delegates", "ledger.jsonl"),
		`{"event":"avatar","name":"bob","working_dir":"`+filepath.Join(lingtaiDir, "bob")+`","ts":1000}`+"\n")

	writeFile(t, filepath.Join(agentDir, "mailbox", "inbox", "msg1", "message.json"),
		`{"id":"msg1","from":"`+filepath.Join(lingtaiDir, "bob")+`","to":"`+agentDir+`","message":"hi"}`)

	writeFile(t, filepath.Join(agentDir, "logs", "events.jsonl"),
		`{"type":"agent_state","address":"`+agentDir+`","old":"ASLEEP","new":"ACTIVE","ts":1000}`+"\n")

	// Topology tape to verify deletion
	os.MkdirAll(filepath.Join(lingtaiDir, ".portal", "replay"), 0o755)
	writeFile(t, filepath.Join(lingtaiDir, ".portal", "topology.jsonl"), `{"t":1000}`)

	if err := Run(lingtaiDir); err != nil {
		t.Fatalf("Run() failed: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(agentDir, ".agent.json"))
	if strings.Contains(string(data), lingtaiDir) {
		t.Errorf(".agent.json still contains absolute path: %s", data)
	}
	if !strings.Contains(string(data), `"address":"alice"`) {
		t.Errorf(".agent.json missing relative address: %s", data)
	}

	data, _ = os.ReadFile(filepath.Join(agentDir, "mailbox", "contacts.json"))
	if strings.Contains(string(data), lingtaiDir) {
		t.Errorf("contacts.json still contains absolute path: %s", data)
	}

	data, _ = os.ReadFile(filepath.Join(agentDir, "delegates", "ledger.jsonl"))
	if strings.Contains(string(data), lingtaiDir) {
		t.Errorf("ledger.jsonl still contains absolute path: %s", data)
	}

	data, _ = os.ReadFile(filepath.Join(agentDir, "logs", "events.jsonl"))
	if strings.Contains(string(data), lingtaiDir) {
		t.Errorf("events.jsonl still contains absolute path: %s", data)
	}

	if _, err := os.Stat(filepath.Join(lingtaiDir, ".portal", "topology.jsonl")); !os.IsNotExist(err) {
		t.Error("topology.jsonl should have been deleted")
	}

	meta := readMeta(t, lingtaiDir)
	if meta.Version != CurrentVersion {
		t.Errorf("expected version %d, got %d", CurrentVersion, meta.Version)
	}
}

// helpers

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

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
