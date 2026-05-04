package preset

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestSetupPreservesAgentJSONIdentity verifies that re-running /setup
// against an existing agent preserves kernel-owned identity fields
// (agent_id, created_at, molt_count, language, etc.). Regression: prior
// code unconditionally rebuilt .agent.json with only {agent_name,
// address, state, admin}, dropping molt_count to 0 — which made psyche
// overwrite past snapshots and broke soul-flow continuity.
func TestSetupPreservesAgentJSONIdentity(t *testing.T) {
	tmp := t.TempDir()
	globalDir := filepath.Join(tmp, "global")
	lingtaiDir := filepath.Join(tmp, "project", ".lingtai")
	agentDir := filepath.Join(lingtaiDir, "alice")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Seed a long-lived .agent.json — the agent has molted twice, has a
	// stable agent_id, language preference, etc.
	prev := map[string]interface{}{
		"agent_id":    "20260501-123456-abc1",
		"agent_name":  "alice",
		"address":     "alice",
		"created_at":  "2026-05-01T12:34:56Z",
		"started_at":  "2026-05-04T07:00:00Z",
		"molt_count":  float64(3), // JSON numbers unmarshal as float64
		"stamina":     float64(36000),
		"language":    "wen",
		"nickname":    "ah-li",
		"soul_delay":  float64(120),
		"soul_voice":  "inner",
		"state":       "active",
		"capabilities": []interface{}{
			[]interface{}{"bash", map[string]interface{}{"yolo": true}},
		},
		"admin": map[string]interface{}{"karma": true, "nirvana": false},
	}
	prevData, _ := json.MarshalIndent(prev, "", "  ")
	if err := os.WriteFile(filepath.Join(agentDir, ".agent.json"), prevData, 0o644); err != nil {
		t.Fatal(err)
	}

	// Re-run wizard with new opts — turn karma off, but keep otherwise.
	opts := DefaultAgentOpts()
	opts.Karma = false
	opts.PreserveActivePreset = true

	if err := GenerateInitJSONWithOpts(minimaxPreset(), "alice", "alice", lingtaiDir, globalDir, opts); err != nil {
		t.Fatalf("GenerateInitJSONWithOpts: %v", err)
	}

	// Verify the merge.
	data, err := os.ReadFile(filepath.Join(agentDir, ".agent.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}

	// Wizard-controlled fields take effect.
	if name := got["agent_name"]; name != "alice" {
		t.Errorf("agent_name = %v, want alice", name)
	}
	admin := got["admin"].(map[string]interface{})
	if admin["karma"] != false {
		t.Errorf("admin.karma = %v, want false (wizard override)", admin["karma"])
	}

	// Kernel-owned identity must be preserved verbatim.
	for _, key := range []string{"agent_id", "created_at", "started_at", "language", "nickname", "soul_voice"} {
		if got[key] != prev[key] {
			t.Errorf("%s clobbered: got %v, want %v", key, got[key], prev[key])
		}
	}
	for _, key := range []string{"molt_count", "stamina", "soul_delay"} {
		if got[key] != prev[key] {
			t.Errorf("%s clobbered: got %v, want %v", key, got[key], prev[key])
		}
	}
	// capabilities preserved (kernel materializes from preset on next boot;
	// the file's prior cached value should still be there until then).
	if _, ok := got["capabilities"]; !ok {
		t.Errorf("capabilities key dropped from .agent.json")
	}
}

// TestSetupFreshAgentInitializesAgentJSON verifies that a brand new agent
// (no prior .agent.json) gets a clean baseline with state="" so the
// kernel can populate the rest at boot.
func TestSetupFreshAgentInitializesAgentJSON(t *testing.T) {
	tmp := t.TempDir()
	globalDir := filepath.Join(tmp, "global")
	lingtaiDir := filepath.Join(tmp, "project", ".lingtai")
	agentDir := filepath.Join(lingtaiDir, "newbie")
	// Note: we do NOT pre-create agentDir — GenerateInitJSONWithOpts must.

	opts := DefaultAgentOpts()
	if err := GenerateInitJSONWithOpts(minimaxPreset(), "newbie", "newbie", lingtaiDir, globalDir, opts); err != nil {
		t.Fatalf("GenerateInitJSONWithOpts: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(agentDir, ".agent.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}

	if got["agent_name"] != "newbie" {
		t.Errorf("agent_name = %v", got["agent_name"])
	}
	if got["state"] != "" {
		t.Errorf("state = %v, want \"\" for fresh agent", got["state"])
	}
	// No molt_count yet — the kernel will populate at first boot.
	if _, present := got["molt_count"]; present {
		t.Errorf("molt_count should not be initialized by TUI; kernel owns it")
	}
}
