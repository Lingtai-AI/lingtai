package postman

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestScanOutbox(t *testing.T) {
	agentDir := t.TempDir()
	outboxDir := filepath.Join(agentDir, "mailbox", "outbox")

	msgDir := filepath.Join(outboxDir, "uuid-001")
	os.MkdirAll(msgDir, 0o755)
	msg := map[string]interface{}{
		"_mailbox_id": "uuid-001",
		"from":        "human",
		"to":          []string{"[2001:db8::1]:/home/user/.lingtai/agent_b"},
		"message":     "hello remote",
	}
	data, _ := json.MarshalIndent(msg, "", "  ")
	os.WriteFile(filepath.Join(msgDir, "message.json"), data, 0o644)

	items := ScanOutbox(agentDir)
	if len(items) != 1 {
		t.Fatalf("ScanOutbox: got %d items, want 1", len(items))
	}
	if items[0].ID != "uuid-001" {
		t.Errorf("item ID = %q, want %q", items[0].ID, "uuid-001")
	}
	if items[0].PeerAddr != "2001:db8::1" {
		t.Errorf("peer addr = %q, want %q", items[0].PeerAddr, "2001:db8::1")
	}
}

func TestScanOutbox_Empty(t *testing.T) {
	agentDir := t.TempDir()
	items := ScanOutbox(agentDir)
	if len(items) != 0 {
		t.Errorf("ScanOutbox on empty dir: got %d items, want 0", len(items))
	}
}

func TestCleanOutboxItem(t *testing.T) {
	agentDir := t.TempDir()
	msgDir := filepath.Join(agentDir, "mailbox", "outbox", "uuid-001")
	os.MkdirAll(msgDir, 0o755)
	os.WriteFile(filepath.Join(msgDir, "message.json"), []byte(`{}`), 0o644)

	CleanOutboxItem(agentDir, "uuid-001")

	if _, err := os.Stat(msgDir); !os.IsNotExist(err) {
		t.Error("outbox item should be removed after cleanup")
	}
}
