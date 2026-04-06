package fs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// --- helpers ---

// writeAgentManifest writes a minimal .agent.json for testing.
func writeAgentManifest(t *testing.T, agentDir, name string, isHuman bool) {
	t.Helper()
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	m := map[string]interface{}{
		"agent_name": name,
		"address":    name, // relative name, not absolute path
		"state":      "ACTIVE",
	}
	if !isHuman {
		m["admin"] = agentDir // non-nil admin → is_human=false
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, ".agent.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeEvent appends a JSON line to <agentDir>/logs/events.jsonl.
func writeEvent(t *testing.T, agentDir string, event map[string]interface{}) {
	t.Helper()
	logsDir := filepath.Join(agentDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	line, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	line = append(line, '\n')
	f, err := os.OpenFile(filepath.Join(logsDir, "events.jsonl"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(line); err != nil {
		t.Fatal(err)
	}
}

// writeMailMessage writes a message.json into <agentDir>/mailbox/<folder>/<msgID>/message.json.
func writeMailMessage(t *testing.T, agentDir, folder, msgID string, msg MailMessage) {
	t.Helper()
	dir := filepath.Join(agentDir, "mailbox", folder, msgID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "message.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// --- tests ---

func TestReconstructTape_Basic(t *testing.T) {
	base := t.TempDir()

	// Create human
	humanDir := filepath.Join(base, "human")
	writeAgentManifest(t, humanDir, "human", true)

	// Create agent
	agentDir := filepath.Join(base, "agent-a")
	writeAgentManifest(t, agentDir, "agent-a", false)

	// t0: agent becomes active
	t0 := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	writeEvent(t, agentDir, map[string]interface{}{
		"type":       "agent_state",
		"ts":         float64(t0.Unix()),
		"address":    "agent-a",
		"agent_name": "agent-a",
		"old":        "asleep",
		"new":        "active",
	})

	// t0+5s: heartbeat
	t1 := t0.Add(5 * time.Second)
	writeEvent(t, agentDir, map[string]interface{}{
		"type":       "heartbeat_start",
		"ts":         float64(t1.Unix()),
		"address":    "agent-a",
		"agent_name": "agent-a",
	})

	// Write one email from agent-a to human, received at t0+2s
	emailTime := t0.Add(2 * time.Second)
	writeMailMessage(t, humanDir, "inbox", "msg-001", MailMessage{
		ID:         "msg-001",
		From:       "agent-a",
		To:         "human",
		ReceivedAt: emailTime.Format(time.RFC3339),
	})

	frames, err := ReconstructTape(base)
	if err != nil {
		t.Fatalf("ReconstructTape() error: %v", err)
	}

	// Must produce at least 1 frame
	if len(frames) == 0 {
		t.Fatal("expected frames, got 0")
	}

	// First frame t should be >= t0 (within 3s bucket)
	firstT := time.UnixMilli(frames[0].T)
	if firstT.Before(t0.Add(-3 * time.Second)) {
		t.Errorf("first frame T=%v is too early (t0=%v)", firstT, t0)
	}

	// Last frame t should be >= t1
	lastT := time.UnixMilli(frames[len(frames)-1].T)
	if lastT.Before(t1.Add(-3 * time.Second)) {
		t.Errorf("last frame T=%v is too early (t1=%v)", lastT, t1)
	}

	// All frames must have non-nil arrays
	for i, f := range frames {
		if f.Net.Nodes == nil {
			t.Errorf("frame[%d]: Nodes is nil", i)
		}
		if f.Net.AvatarEdges == nil {
			t.Errorf("frame[%d]: AvatarEdges is nil", i)
		}
		if f.Net.ContactEdges == nil {
			t.Errorf("frame[%d]: ContactEdges is nil", i)
		}
		if f.Net.MailEdges == nil {
			t.Errorf("frame[%d]: MailEdges is nil", i)
		}
	}

	// Check that the last frame has a mail edge with Direct=1
	lastFrame := frames[len(frames)-1]
	foundMail := false
	for _, e := range lastFrame.Net.MailEdges {
		if e.Sender == agentDir && e.Recipient == humanDir && e.Direct == 1 {
			foundMail = true
		}
	}
	if !foundMail {
		t.Errorf("expected mail edge with direct=1 in last frame, got: %+v", lastFrame.Net.MailEdges)
	}
}

func TestReconstructTape_Empty(t *testing.T) {
	base := t.TempDir()

	// Create human only, no events
	humanDir := filepath.Join(base, "human")
	writeAgentManifest(t, humanDir, "human", true)

	frames, err := ReconstructTape(base)
	if err != nil {
		t.Fatalf("ReconstructTape() error: %v", err)
	}

	// No events, no emails → should return nil or empty without error
	if len(frames) != 0 {
		t.Errorf("expected 0 frames for empty base, got %d", len(frames))
	}
}

func TestReconstructTape_StateTransitions(t *testing.T) {
	base := t.TempDir()

	// Human always present
	humanDir := filepath.Join(base, "human")
	writeAgentManifest(t, humanDir, "human", true)

	// Agent
	agentDir := filepath.Join(base, "agent-b")
	writeAgentManifest(t, agentDir, "agent-b", false)

	// t0: agent becomes active
	t0 := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	writeEvent(t, agentDir, map[string]interface{}{
		"type":       "agent_state",
		"ts":         float64(t0.Unix()),
		"address":    "agent-b",
		"agent_name": "agent-b",
		"old":        "asleep",
		"new":        "active",
	})

	// t0+12s: agent becomes suspended
	t1 := t0.Add(12 * time.Second)
	writeEvent(t, agentDir, map[string]interface{}{
		"type":       "agent_state",
		"ts":         float64(t1.Unix()),
		"address":    "agent-b",
		"agent_name": "agent-b",
		"old":        "active",
		"new":        "suspended",
	})

	// t0+24s: agent revived (back to active)
	t2 := t0.Add(24 * time.Second)
	writeEvent(t, agentDir, map[string]interface{}{
		"type":       "agent_state",
		"ts":         float64(t2.Unix()),
		"address":    "agent-b",
		"agent_name": "agent-b",
		"old":        "suspended",
		"new":        "active",
	})

	frames, err := ReconstructTape(base)
	if err != nil {
		t.Fatalf("ReconstructTape() error: %v", err)
	}

	if len(frames) == 0 {
		t.Fatal("expected frames, got 0")
	}

	// Find agent state at various timestamps by looking at frames
	agentStateAt := func(ts time.Time) string {
		var state string
		for _, f := range frames {
			if f.T > ts.UnixMilli() {
				break
			}
			for _, n := range f.Net.Nodes {
				if n.WorkingDir == agentDir {
					state = n.State
				}
			}
		}
		return state
	}

	// At t0+6s (between active start and suspend): should be ACTIVE
	s1 := agentStateAt(t0.Add(6 * time.Second))
	if s1 != "ACTIVE" {
		t.Errorf("expected ACTIVE at t0+6s, got %q", s1)
	}

	// At t0+15s (during suspended period): should be SUSPENDED
	s2 := agentStateAt(t0.Add(15 * time.Second))
	if s2 != "SUSPENDED" {
		t.Errorf("expected SUSPENDED at t0+15s, got %q", s2)
	}

	// At t0+27s (after revival): should be ACTIVE
	s3 := agentStateAt(t0.Add(27 * time.Second))
	if s3 != "ACTIVE" {
		t.Errorf("expected ACTIVE at t0+27s, got %q", s3)
	}
}
