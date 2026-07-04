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

func TestCountDaemons(t *testing.T) {
	agentDir := t.TempDir()

	writeDaemonState(t, agentDir, "running", map[string]interface{}{
		"state": "running",
	})
	writeDaemonState(t, agentDir, "active", map[string]interface{}{
		"state": "active",
	})
	writeDaemonState(t, agentDir, "terminal", map[string]interface{}{
		"state": "done",
	})
	writeDaemonState(t, agentDir, "finished", map[string]interface{}{
		"state":       "running",
		"finished_at": "2026-05-24T12:00:00Z",
	})
	writeMalformedDaemonState(t, agentDir, "malformed")

	counts := CountDaemons(agentDir)
	if counts.Running != 2 {
		t.Fatalf("running daemons = %d, want 2", counts.Running)
	}
	if counts.Total != 4 {
		t.Fatalf("total daemons = %d, want 4", counts.Total)
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

func writeMalformedDaemonState(t *testing.T, agentDir, runID string) {
	t.Helper()
	path := filepath.Join(agentDir, "daemons", runID, "daemon.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir daemon: %v", err)
	}
	if err := os.WriteFile(path, []byte("{bad json"), 0o644); err != nil {
		t.Fatalf("write malformed daemon: %v", err)
	}
}

func TestComputeNetworkActivity_StatusActiveTurnEvidence(t *testing.T) {
	cases := []struct {
		name   string
		status map[string]interface{}
		mtime  time.Time
		want   string
		active int
	}{
		{
			name: "pending active turn counts with elapsed_seconds",
			status: map[string]interface{}{
				"active_turn": map[string]interface{}{
					"kind":            "pending",
					"id":              "turn-1",
					"started_at":      time.Now().Add(-30 * time.Second).Unix(),
					"elapsed_seconds": 30,
				},
			},
			mtime:  time.Now(),
			want:   NetworkStatusActive,
			active: 1,
		},
		{
			name: "fresh mtime keeps old started_at active",
			status: map[string]interface{}{
				"active_turn": map[string]interface{}{
					"kind":            "tool",
					"id":              "turn-2",
					"started_at":      time.Now().Add(-2 * networkActiveTurnCap).Unix(),
					"elapsed_seconds": 1200,
				},
			},
			mtime:  time.Now(),
			want:   NetworkStatusActive,
			active: 1,
		},
		{
			name: "old mtime and old active_turn age out at cap",
			status: map[string]interface{}{
				"active_turn": map[string]interface{}{
					"kind":            "tool",
					"id":              "turn-3",
					"started_at":      time.Now().Add(-2 * networkActiveTurnCap).Unix(),
					"elapsed_seconds": 1200,
				},
			},
			mtime:  time.Now().Add(-2 * networkActiveTurnCap),
			want:   NetworkStatusIdle,
			active: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			agentDir := writeActivityAgent(t, base, "alice", "IDLE", true)
			writeStatus(t, agentDir, tc.status, tc.mtime)

			activity, err := ComputeNetworkActivity(base)
			if err != nil {
				t.Fatalf("compute activity: %v", err)
			}
			if activity.Status != tc.want {
				t.Fatalf("status = %q, want %q", activity.Status, tc.want)
			}
			if activity.ActiveAgents != tc.active {
				t.Fatalf("active agents = %d, want %d", activity.ActiveAgents, tc.active)
			}
		})
	}
}

func TestComputeNetworkActivity_StatusRuntimeLastProgressWindow(t *testing.T) {
	cases := []struct {
		name  string
		last  interface{}
		mtime time.Time
		want  string
	}{
		{
			name:  "fresh standalone last_progress_at counts briefly",
			last:  time.Now().Add(-30 * time.Second).Unix(),
			mtime: time.Now().Add(-2 * networkRecentProgressWindow),
			want:  NetworkStatusActive,
		},
		{
			name:  "standalone last_progress_at does not use active turn cap or mtime",
			last:  time.Now().Add(-2 * networkRecentProgressWindow).Unix(),
			mtime: time.Now(),
			want:  NetworkStatusIdle,
		},
		{
			name:  "scrubbed empty string ignored",
			last:  "",
			mtime: time.Now(),
			want:  NetworkStatusIdle,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			agentDir := writeActivityAgent(t, base, "alice", "IDLE", true)
			writeStatus(t, agentDir, map[string]interface{}{
				"active_turn": nil,
				"runtime": map[string]interface{}{
					"last_progress_at":    tc.last,
					"no_progress_seconds": "",
					"state":               "IDLE",
				},
			}, tc.mtime)

			activity, err := ComputeNetworkActivity(base)
			if err != nil {
				t.Fatalf("compute activity: %v", err)
			}
			if activity.Status != tc.want {
				t.Fatalf("status = %q, want %q", activity.Status, tc.want)
			}
		})
	}
}

func TestHasStatusActivity_FutureTimestampFreshness(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)

	cases := []struct {
		name   string
		status map[string]interface{}
		mtime  time.Time
		want   bool
	}{
		{
			name: "standalone modest future last_progress_at clamps to now",
			status: map[string]interface{}{
				"runtime": map[string]interface{}{
					"last_progress_at": now.Add(30 * time.Second).Unix(),
				},
			},
			mtime: now.Add(-2 * networkRecentProgressWindow),
			want:  true,
		},
		{
			name: "standalone far future last_progress_at ignored",
			status: map[string]interface{}{
				"runtime": map[string]interface{}{
					"last_progress_at": now.Add(24 * time.Hour).Unix(),
				},
			},
			mtime: now,
			want:  false,
		},
		{
			name: "active turn modest future started_at clamps to now",
			status: map[string]interface{}{
				"active_turn": map[string]interface{}{
					"kind":       "tool",
					"id":         "turn-1",
					"started_at": now.Add(30 * time.Second).Unix(),
				},
			},
			mtime: now.Add(-2 * networkActiveTurnCap),
			want:  true,
		},
		{
			name: "active turn far future started_at does not hide fresh mtime",
			status: map[string]interface{}{
				"active_turn": map[string]interface{}{
					"kind":       "tool",
					"id":         "turn-2",
					"started_at": now.Add(24 * time.Hour).Unix(),
				},
			},
			mtime: now,
			want:  true,
		},
		{
			name: "active turn far future started_at ignored without fresh evidence",
			status: map[string]interface{}{
				"active_turn": map[string]interface{}{
					"kind":       "tool",
					"id":         "turn-3",
					"started_at": now.Add(24 * time.Hour).Unix(),
				},
			},
			mtime: now.Add(-2 * networkActiveTurnCap),
			want:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agentDir := t.TempDir()
			writeStatus(t, agentDir, tc.status, tc.mtime)

			if got := hasStatusActivity(agentDir, now); got != tc.want {
				t.Fatalf("hasStatusActivity = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestComputeNetworkActivity_StatusEvidenceRequiresFreshHeartbeat(t *testing.T) {
	base := t.TempDir()
	agentDir := writeActivityAgent(t, base, "alice", "IDLE", false)
	writeStatus(t, agentDir, map[string]interface{}{
		"active_turn": map[string]interface{}{
			"kind":            "tool",
			"id":              "turn-1",
			"started_at":      time.Now().Unix(),
			"elapsed_seconds": 1,
		},
	}, time.Now())

	activity, err := ComputeNetworkActivity(base)
	if err != nil {
		t.Fatalf("compute activity: %v", err)
	}
	if activity.Status != NetworkStatusSuspend {
		t.Fatalf("status = %q, want %q", activity.Status, NetworkStatusSuspend)
	}
}

func TestComputeNetworkActivity_MalformedStatusFallsBackSafely(t *testing.T) {
	base := t.TempDir()
	agentDir := writeActivityAgent(t, base, "alice", "IDLE", true)
	path := filepath.Join(agentDir, ".status.json")
	if err := os.WriteFile(path, []byte(`{"active_turn":`), 0o644); err != nil {
		t.Fatalf("write malformed status: %v", err)
	}

	activity, err := ComputeNetworkActivity(base)
	if err != nil {
		t.Fatalf("compute activity: %v", err)
	}
	if activity.Status != NetworkStatusIdle {
		t.Fatalf("status = %q, want %q", activity.Status, NetworkStatusIdle)
	}
}

func TestBuildNetwork_ActivityMatchesComputeNetworkActivityForStatusEvidence(t *testing.T) {
	base := t.TempDir()
	agentDir := writeActivityAgent(t, base, "alice", "IDLE", true)
	writeStatus(t, agentDir, map[string]interface{}{
		"active_turn": map[string]interface{}{
			"kind":            "pending",
			"id":              "turn-1",
			"started_at":      time.Now().Unix(),
			"elapsed_seconds": 0,
		},
	}, time.Now())

	activity, err := ComputeNetworkActivity(base)
	if err != nil {
		t.Fatalf("compute activity: %v", err)
	}
	net, err := BuildNetwork(base)
	if err != nil {
		t.Fatalf("build network: %v", err)
	}
	if net.Activity != activity {
		t.Fatalf("BuildNetwork activity = %+v, ComputeNetworkActivity = %+v", net.Activity, activity)
	}
}

func TestComputeNetworkActivity_DaemonActiveOnlyWhenSoleSignal(t *testing.T) {
	base := t.TempDir()
	agentDir := writeActivityAgent(t, base, "alice", "IDLE", true)
	writeDaemonState(t, agentDir, "run-1", map[string]interface{}{"state": "running"})
	writeStatus(t, agentDir, map[string]interface{}{
		"runtime": map[string]interface{}{
			"last_progress_at": time.Now().Unix(),
		},
	}, time.Now())

	activity, err := ComputeNetworkActivity(base)
	if err != nil {
		t.Fatalf("compute activity: %v", err)
	}
	if activity.Status != NetworkStatusActive {
		t.Fatalf("status = %q, want %q", activity.Status, NetworkStatusActive)
	}
	if activity.RunningDaemons != 1 {
		t.Fatalf("running daemons = %d, want 1", activity.RunningDaemons)
	}
}

func writeStatus(t *testing.T, agentDir string, status map[string]interface{}, mtime time.Time) {
	t.Helper()
	path := filepath.Join(agentDir, ".status.json")
	writeJSON(t, path, status)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("chtimes status: %v", err)
	}
}
