package fs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setupTestNetwork(t *testing.T) string {
	t.Helper()
	base := t.TempDir()

	aliceDir := filepath.Join(base, "alice")
	os.MkdirAll(filepath.Join(aliceDir, "mailbox", "inbox"), 0o755)
	os.MkdirAll(filepath.Join(aliceDir, "mailbox", "sent"), 0o755)
	os.MkdirAll(filepath.Join(aliceDir, "delegates"), 0o755)

	writeJSON(t, filepath.Join(aliceDir, ".agent.json"), map[string]interface{}{
		"agent_name": "alice", "address": "alice", "state": "ACTIVE",
		"admin": map[string]interface{}{"karma": true},
	})
	// Fresh heartbeat so IsAlive returns true and State is not overridden
	writeHeartbeat(t, aliceDir)

	// Ledger uses relative name
	ledger := `{"event":"avatar","name":"bob","working_dir":"bob","ts":1000}`
	os.WriteFile(filepath.Join(aliceDir, "delegates", "ledger.jsonl"), []byte(ledger+"\n"), 0o644)

	// Contacts use relative name
	contacts := []map[string]string{{"address": "bob", "name": "bob"}}
	data, _ := json.Marshal(contacts)
	os.WriteFile(filepath.Join(aliceDir, "mailbox", "contacts.json"), data, 0o644)

	bobDir := filepath.Join(base, "bob")
	os.MkdirAll(filepath.Join(bobDir, "mailbox", "inbox"), 0o755)
	writeJSON(t, filepath.Join(bobDir, ".agent.json"), map[string]interface{}{
		"agent_name": "bob", "address": "bob", "state": "IDLE",
		"admin": map[string]interface{}{"karma": false},
	})
	// Fresh heartbeat so IsAlive returns true and State is not overridden
	writeHeartbeat(t, bobDir)

	humanDir := filepath.Join(base, "human")
	os.MkdirAll(filepath.Join(humanDir, "mailbox", "inbox"), 0o755)
	writeJSON(t, filepath.Join(humanDir, ".agent.json"), map[string]interface{}{
		"agent_name": "human", "address": "human", "admin": nil,
	})

	return base
}

func writeJSON(t *testing.T, path string, v interface{}) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	data, _ := json.Marshal(v)
	os.WriteFile(path, data, 0o644)
}

func writeHeartbeat(t *testing.T, dir string) {
	t.Helper()
	content := fmt.Sprintf("%d", time.Now().Unix())
	os.WriteFile(filepath.Join(dir, ".agent.heartbeat"), []byte(content), 0o644)
}

func TestBuildNetwork(t *testing.T) {
	base := setupTestNetwork(t)

	net, err := BuildNetwork(base)
	if err != nil {
		t.Fatalf("build network: %v", err)
	}

	if len(net.Nodes) != 3 {
		t.Errorf("nodes = %d, want 3", len(net.Nodes))
	}
	if len(net.AvatarEdges) != 1 {
		t.Errorf("avatar edges = %d, want 1", len(net.AvatarEdges))
	}
	if len(net.ContactEdges) != 1 {
		t.Errorf("contact edges = %d, want 1", len(net.ContactEdges))
	}
	if net.Stats.Active != 1 {
		t.Errorf("active = %d, want 1", net.Stats.Active)
	}
	if net.Stats.Idle != 1 {
		t.Errorf("idle = %d, want 1", net.Stats.Idle)
	}
	if net.Activity.Status != NetworkStatusActive {
		t.Errorf("activity status = %q, want %q", net.Activity.Status, NetworkStatusActive)
	}
}

func TestBuildNetwork_AllAddressesRelative(t *testing.T) {
	base := setupTestNetwork(t)

	net, err := BuildNetwork(base)
	if err != nil {
		t.Fatalf("build network: %v", err)
	}

	for _, n := range net.Nodes {
		if len(n.Address) > 0 && n.Address[0] == '/' {
			t.Errorf("node %s has absolute address: %s", n.AgentName, n.Address)
		}
	}
}

func TestBuildNetwork_WorkingDirAlwaysAbsolute(t *testing.T) {
	base := setupTestNetwork(t)

	net, err := BuildNetwork(base)
	if err != nil {
		t.Fatalf("build network: %v", err)
	}

	for _, n := range net.Nodes {
		if !filepath.IsAbs(n.WorkingDir) {
			t.Errorf("node %s has relative WorkingDir: %s", n.AgentName, n.WorkingDir)
		}
	}
}

func writeNetworkTestMailMessage(t *testing.T, agentDir, folder, msgID string, msg MailMessage) {
	t.Helper()
	msg.ID = msgID
	if msg.ReceivedAt == "" {
		msg.ReceivedAt = time.Now().Format(time.RFC3339)
	}
	msgDir := filepath.Join(agentDir, "mailbox", folder, msgID)
	os.MkdirAll(msgDir, 0o755)
	data, _ := json.Marshal(msg)
	os.WriteFile(filepath.Join(msgDir, "message.json"), data, 0o644)
}

func TestBuildNetworkWithOptionsSkipsMailEdges(t *testing.T) {
	base := setupTestNetwork(t)
	writeNetworkTestMailMessage(t, filepath.Join(base, "bob"), "inbox", "msg-1", MailMessage{
		From:       "alice",
		To:         "bob",
		ReceivedAt: time.Now().Format(time.RFC3339),
	})

	full, err := BuildNetwork(base)
	if err != nil {
		t.Fatalf("build full network: %v", err)
	}
	if len(full.MailEdges) != 1 {
		t.Fatalf("full mail edges = %d, want 1", len(full.MailEdges))
	}
	if full.Stats.TotalMails != 1 {
		t.Fatalf("full total mails = %d, want 1", full.Stats.TotalMails)
	}

	fast, err := BuildNetworkWithOptions(base, NetworkOptions{SkipMailEdges: true})
	if err != nil {
		t.Fatalf("build fast network: %v", err)
	}
	if len(fast.MailEdges) != 0 {
		t.Fatalf("fast mail edges = %d, want 0", len(fast.MailEdges))
	}
	if fast.Stats.TotalMails != 0 {
		t.Fatalf("fast total mails = %d, want 0", fast.Stats.TotalMails)
	}
	if len(fast.Nodes) != len(full.Nodes) || len(fast.AvatarEdges) != len(full.AvatarEdges) || len(fast.ContactEdges) != len(full.ContactEdges) {
		t.Fatalf("fast topology changed nodes/avatar/contact counts: fast=%d/%d/%d full=%d/%d/%d",
			len(fast.Nodes), len(fast.AvatarEdges), len(fast.ContactEdges),
			len(full.Nodes), len(full.AvatarEdges), len(full.ContactEdges))
	}
}
