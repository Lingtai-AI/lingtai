package fs

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestComputeNetworkActivity_ActiveAgent(t *testing.T) {
	base := t.TempDir()
	writeActivityAgent(t, base, "alice", "ACTIVE", true)

	activity, err := ComputeNetworkActivity(base)
	if err != nil {
		t.Fatalf("compute activity: %v", err)
	}
	if activity.Status != NetworkStatusActive {
		t.Fatalf("status = %q, want %q", activity.Status, NetworkStatusActive)
	}
	if activity.ActiveAgents != 1 {
		t.Fatalf("active agents = %d, want 1", activity.ActiveAgents)
	}
}

func TestComputeNetworkActivity_IdleLiveAgentWithRunningDaemon(t *testing.T) {
	for _, state := range []string{"running", "active"} {
		t.Run(state, func(t *testing.T) {
			base := t.TempDir()
			agentDir := writeActivityAgent(t, base, "alice", "IDLE", true)
			writeDaemonState(t, agentDir, "run-1", map[string]interface{}{
				"state": state,
			})

			activity, err := ComputeNetworkActivity(base)
			if err != nil {
				t.Fatalf("compute activity: %v", err)
			}
			if activity.Status != NetworkStatusDaemonActive {
				t.Fatalf("status = %q, want %q", activity.Status, NetworkStatusDaemonActive)
			}
			if activity.RunningDaemons != 1 {
				t.Fatalf("running daemons = %d, want 1", activity.RunningDaemons)
			}
		})
	}
}

func TestComputeNetworkActivity_ParentStaleRunningDaemonIgnored(t *testing.T) {
	base := t.TempDir()
	agentDir := writeActivityAgent(t, base, "alice", "IDLE", false)
	writeDaemonState(t, agentDir, "run-1", map[string]interface{}{
		"state": "running",
	})

	activity, err := ComputeNetworkActivity(base)
	if err != nil {
		t.Fatalf("compute activity: %v", err)
	}
	if activity.Status != NetworkStatusSuspend {
		t.Fatalf("status = %q, want %q", activity.Status, NetworkStatusSuspend)
	}
	if activity.RunningDaemons != 0 {
		t.Fatalf("running daemons = %d, want 0", activity.RunningDaemons)
	}
}

func TestComputeNetworkActivity_TerminalAndFinishedDaemonsIgnored(t *testing.T) {
	cases := []struct {
		name   string
		daemon map[string]interface{}
	}{
		{name: "done", daemon: map[string]interface{}{"state": "done"}},
		{name: "failed", daemon: map[string]interface{}{"state": "failed"}},
		{name: "cancelled", daemon: map[string]interface{}{"state": "cancelled"}},
		{name: "timeout", daemon: map[string]interface{}{"state": "timeout"}},
		{name: "running with finished_at", daemon: map[string]interface{}{"state": "running", "finished_at": "2026-05-24T12:00:00Z"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			agentDir := writeActivityAgent(t, base, "alice", "IDLE", true)
			writeDaemonState(t, agentDir, "run-1", tc.daemon)

			activity, err := ComputeNetworkActivity(base)
			if err != nil {
				t.Fatalf("compute activity: %v", err)
			}
			if activity.Status != NetworkStatusIdle {
				t.Fatalf("status = %q, want %q", activity.Status, NetworkStatusIdle)
			}
			if activity.RunningDaemons != 0 {
				t.Fatalf("running daemons = %d, want 0", activity.RunningDaemons)
			}
		})
	}
}

func TestComputeNetworkActivity_AllIdle(t *testing.T) {
	base := t.TempDir()
	writeActivityAgent(t, base, "alice", "IDLE", true)
	writeActivityAgent(t, base, "bob", "IDLE", true)

	activity, err := ComputeNetworkActivity(base)
	if err != nil {
		t.Fatalf("compute activity: %v", err)
	}
	if activity.Status != NetworkStatusIdle {
		t.Fatalf("status = %q, want %q", activity.Status, NetworkStatusIdle)
	}
}

func TestComputeNetworkActivity_AsleepAndSuspended(t *testing.T) {
	base := t.TempDir()
	writeActivityAgent(t, base, "alice", "ASLEEP", true)
	writeActivityAgent(t, base, "bob", "ACTIVE", false)

	activity, err := ComputeNetworkActivity(base)
	if err != nil {
		t.Fatalf("compute activity: %v", err)
	}
	if activity.Status != NetworkStatusAsleep {
		t.Fatalf("status = %q, want %q", activity.Status, NetworkStatusAsleep)
	}
}

func TestComputeNetworkActivity_AllSuspended(t *testing.T) {
	base := t.TempDir()
	writeActivityAgent(t, base, "alice", "ACTIVE", false)
	writeActivityAgent(t, base, "bob", "IDLE", false)

	activity, err := ComputeNetworkActivity(base)
	if err != nil {
		t.Fatalf("compute activity: %v", err)
	}
	if activity.Status != NetworkStatusSuspend {
		t.Fatalf("status = %q, want %q", activity.Status, NetworkStatusSuspend)
	}
}

func TestComputeNetworkActivity_HumanIgnored(t *testing.T) {
	base := t.TempDir()
	humanDir := filepath.Join(base, "human")
	if err := os.MkdirAll(humanDir, 0o755); err != nil {
		t.Fatalf("mkdir human: %v", err)
	}
	writeJSON(t, filepath.Join(humanDir, ".agent.json"), map[string]interface{}{
		"agent_name": "human",
		"address":    "human",
		"state":      "ACTIVE",
		"admin":      nil,
	})

	activity, err := ComputeNetworkActivity(base)
	if err != nil {
		t.Fatalf("compute activity: %v", err)
	}
	if activity.Status != NetworkStatusSuspend {
		t.Fatalf("status = %q, want %q", activity.Status, NetworkStatusSuspend)
	}
}

func TestComputeNetworkActivity_StuckLiveAgentFallsBackToIdle(t *testing.T) {
	base := t.TempDir()
	writeActivityAgent(t, base, "alice", "STUCK", true)

	activity, err := ComputeNetworkActivity(base)
	if err != nil {
		t.Fatalf("compute activity: %v", err)
	}
	if activity.Status != NetworkStatusIdle {
		t.Fatalf("status = %q, want %q", activity.Status, NetworkStatusIdle)
	}
}

func writeActivityAgent(t *testing.T, baseDir, name, state string, alive bool) string {
	t.Helper()

	agentDir := filepath.Join(baseDir, name)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir agent: %v", err)
	}
	writeJSON(t, filepath.Join(agentDir, ".agent.json"), map[string]interface{}{
		"agent_name": name,
		"address":    name,
		"state":      state,
		"admin":      map[string]interface{}{"karma": true},
	})
	if alive {
		writeHeartbeat(t, agentDir)
	} else {
		writeStaleHeartbeat(t, agentDir)
	}
	return agentDir
}

func writeStaleHeartbeat(t *testing.T, dir string) {
	t.Helper()
	content := fmt.Sprintf("%d", time.Now().Add(-10*time.Second).Unix())
	if err := os.WriteFile(filepath.Join(dir, ".agent.heartbeat"), []byte(content), 0o644); err != nil {
		t.Fatalf("write heartbeat: %v", err)
	}
}

func writeDaemonState(t *testing.T, agentDir, runID string, state map[string]interface{}) {
	t.Helper()
	writeJSON(t, filepath.Join(agentDir, "daemons", runID, "daemon.json"), state)
}
