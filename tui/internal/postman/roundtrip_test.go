package postman

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRoundtrip_SendReceive(t *testing.T) {
	// Set up a "remote" agent's inbox
	remoteBase := t.TempDir()
	remoteAgentDir := filepath.Join(remoteBase, "agent_b")
	os.MkdirAll(filepath.Join(remoteAgentDir, "mailbox", "inbox"), 0o755)

	// Start a receiver on a high port
	stop := make(chan struct{})
	port := 17777
	go func() {
		ListenUDP(port, stop)
	}()
	defer close(stop)
	time.Sleep(50 * time.Millisecond) // let listener bind

	// Create a message addressed to the remote agent via localhost
	msg := map[string]interface{}{
		"_mailbox_id": "roundtrip-001",
		"id":          "roundtrip-001",
		"from":        "human",
		"to":          []string{"localhost:" + remoteAgentDir},
		"message":     "roundtrip test",
		"type":        "normal",
		"received_at": "2026-04-17T10:00:00Z",
	}
	data, _ := json.Marshal(msg)

	// Send via UDP to localhost (::1 for IPv6 loopback)
	err := SendUDP("::1", port, data)
	if err != nil {
		// Fall back to IPv4 loopback if IPv6 isn't available
		err = SendUDP("127.0.0.1", port, data)
		if err != nil {
			t.Fatalf("SendUDP: %v", err)
		}
	}

	// Wait for async delivery
	time.Sleep(200 * time.Millisecond)

	// Verify message arrived in the remote agent's inbox
	msgPath := filepath.Join(remoteAgentDir, "mailbox", "inbox", "roundtrip-001", "message.json")
	got, err := os.ReadFile(msgPath)
	if err != nil {
		t.Fatalf("message not delivered: %v", err)
	}

	var parsed map[string]interface{}
	json.Unmarshal(got, &parsed)
	if parsed["message"] != "roundtrip test" {
		t.Errorf("message = %q, want %q", parsed["message"], "roundtrip test")
	}
}
