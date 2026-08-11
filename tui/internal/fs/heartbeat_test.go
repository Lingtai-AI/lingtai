package fs

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestAgentAliveThresholdSec_MatchesKernelContract pins the Go consumer's
// liveness window to the kernel contract: one-second heartbeat tick and
// five-second liveness (src/lingtai/kernel/config.py:132-133), applied with a
// strict age < threshold comparison (src/lingtai/kernel/agent_presence/__init__.py).
// Drift back to 2s (issue #845) fails here so the disagreement stays visible.
func TestAgentAliveThresholdSec_MatchesKernelContract(t *testing.T) {
	if AgentAliveThresholdSec != 5.0 {
		t.Fatalf("AgentAliveThresholdSec = %v, want 5.0 (kernel heartbeat liveness, issue #845)", AgentAliveThresholdSec)
	}
}

func TestIsAlive_FreshHeartbeat(t *testing.T) {
	dir := t.TempDir()
	ts := fmt.Sprintf("%f", float64(time.Now().Unix()))
	os.WriteFile(filepath.Join(dir, ".agent.heartbeat"), []byte(ts), 0o644)
	if !IsAlive(dir, AgentAliveThresholdSec) {
		t.Error("expected alive for fresh heartbeat")
	}
}

func TestIsAlive_ThreeSecondOldHeartbeat_Alive(t *testing.T) {
	// Regression for issue #845: a heartbeat ~3s old is alive to the kernel
	// (3 < 5) and must be alive to the TUI too, never dead or suspended.
	dir := t.TempDir()
	ts := fmt.Sprintf("%f", float64(time.Now().Add(-3*time.Second).Unix()))
	os.WriteFile(filepath.Join(dir, ".agent.heartbeat"), []byte(ts), 0o644)
	if !IsAlive(dir, AgentAliveThresholdSec) {
		t.Error("expected alive for 3s-old heartbeat under the kernel 5s contract")
	}
}

func TestIsAlive_FiveSecondOldHeartbeat_Stale(t *testing.T) {
	// Strict boundary: an age at or beyond the 5s threshold is stale
	// (kernel compares age < threshold, not age <= threshold).
	dir := t.TempDir()
	ts := fmt.Sprintf("%f", float64(time.Now().Add(-5*time.Second).Unix()))
	os.WriteFile(filepath.Join(dir, ".agent.heartbeat"), []byte(ts), 0o644)
	if IsAlive(dir, AgentAliveThresholdSec) {
		t.Error("expected stale for 5s-old heartbeat (strict age < threshold boundary)")
	}
}

func TestIsAlive_SixSecondOldHeartbeat_Stale(t *testing.T) {
	dir := t.TempDir()
	ts := fmt.Sprintf("%f", float64(time.Now().Add(-6*time.Second).Unix()))
	os.WriteFile(filepath.Join(dir, ".agent.heartbeat"), []byte(ts), 0o644)
	if IsAlive(dir, AgentAliveThresholdSec) {
		t.Error("expected dead for 6s-old heartbeat")
	}
}

func TestIsAlive_MissingFile(t *testing.T) {
	dir := t.TempDir()
	if IsAlive(dir, AgentAliveThresholdSec) {
		t.Error("expected dead when heartbeat file missing")
	}
}

func TestIsAlive_HumanAlwaysAlive(t *testing.T) {
	if !IsAliveHuman() {
		t.Error("human agents are always alive")
	}
}
