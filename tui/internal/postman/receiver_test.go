package postman

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDeliverLocal(t *testing.T) {
	base := t.TempDir()
	agentDir := filepath.Join(base, "agent_a")
	os.MkdirAll(filepath.Join(agentDir, "mailbox", "inbox"), 0o755)

	msg := map[string]interface{}{
		"_mailbox_id": "test-uuid-001",
		"from":        "[2001:db8::1]:/remote/.lingtai/human",
		"to":          []string{"[::1]:" + agentDir},
		"message":     "hello from remote",
	}
	data, _ := json.Marshal(msg)

	err := DeliverLocal(agentDir, "test-uuid-001", data)
	if err != nil {
		t.Fatalf("DeliverLocal: %v", err)
	}

	msgPath := filepath.Join(agentDir, "mailbox", "inbox", "test-uuid-001", "message.json")
	got, err := os.ReadFile(msgPath)
	if err != nil {
		t.Fatalf("read delivered message: %v", err)
	}

	var parsed map[string]interface{}
	json.Unmarshal(got, &parsed)
	if parsed["message"] != "hello from remote" {
		t.Errorf("message = %q, want %q", parsed["message"], "hello from remote")
	}
}

func TestDeliverLocal_NoInbox(t *testing.T) {
	base := t.TempDir()
	nonexistent := filepath.Join(base, "no_such_agent")

	err := DeliverLocal(nonexistent, "test-uuid", []byte(`{}`))
	if err != nil {
		t.Fatalf("DeliverLocal to nonexistent path should not error, got: %v", err)
	}
}

func TestExtractRecipientPath(t *testing.T) {
	tests := []struct {
		to   string
		want string
		ok   bool
	}{
		{"[2001:db8::1]:/home/user/.lingtai/agent_b", "/home/user/.lingtai/agent_b", true},
		{"[::1]:/tmp/test/.lingtai/agent_a", "/tmp/test/.lingtai/agent_a", true},
		{"localhost:/home/user/.lingtai/agent_c", "/home/user/.lingtai/agent_c", true},
		{"agent_b", "", false},
		{"/absolute/path", "", false},
	}

	for _, tt := range tests {
		got, ok := ExtractRecipientPath(tt.to)
		if ok != tt.ok || got != tt.want {
			t.Errorf("ExtractRecipientPath(%q) = (%q, %v), want (%q, %v)",
				tt.to, got, ok, tt.want, tt.ok)
		}
	}
}
