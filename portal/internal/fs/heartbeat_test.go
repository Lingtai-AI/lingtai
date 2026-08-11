package fs

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestAgentAliveThresholdSec_MatchesKernelContract pins the Portal's liveness
// window to the kernel contract: one-second heartbeat tick and five-second
// liveness (src/lingtai/kernel/config.py:132-133), applied with a strict
// age < threshold comparison (src/lingtai/kernel/agent_presence/__init__.py).
// Drift back to 2s (issue #845) fails here so the disagreement stays visible.
func TestAgentAliveThresholdSec_MatchesKernelContract(t *testing.T) {
	if AgentAliveThresholdSec != 5.0 {
		t.Fatalf("AgentAliveThresholdSec = %v, want 5.0 (kernel heartbeat liveness, issue #845)", AgentAliveThresholdSec)
	}
}

func TestIsAlive_ThreeSecondOldHeartbeat_Alive(t *testing.T) {
	// Regression for issue #845: a heartbeat ~3s old is alive to the kernel
	// (3 < 5) and must be alive to the Portal too, never reported dead.
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

// TestBuildNetwork_HeartbeatAgeDrivesAliveState is the Portal topology-level
// regression for issue #845: a 3s-old heartbeat (alive to the kernel) must not
// be marked dead or SUSPENDED, while an 8s-old heartbeat (stale even to the
// kernel) is not alive and is mapped to SUSPENDED.
func TestBuildNetwork_HeartbeatAgeDrivesAliveState(t *testing.T) {
	base := t.TempDir()

	bobDir := filepath.Join(base, "bob")
	writeAgentManifest(t, bobDir, "bob", false) // manifest state "ACTIVE"
	ts := fmt.Sprintf("%f", float64(time.Now().Add(-3*time.Second).Unix()))
	os.WriteFile(filepath.Join(bobDir, ".agent.heartbeat"), []byte(ts), 0o644)

	net, err := BuildNetwork(base)
	if err != nil {
		t.Fatalf("build network: %v", err)
	}
	var bob *AgentNode
	for i := range net.Nodes {
		if net.Nodes[i].Address == "bob" {
			bob = &net.Nodes[i]
		}
	}
	if bob == nil {
		t.Fatal("bob node not found")
	}
	if !bob.Alive {
		t.Errorf("bob with 3s-old heartbeat should be alive under the kernel 5s contract, got Alive=false")
	}
	if bob.State == "SUSPENDED" {
		t.Errorf("bob with 3s-old heartbeat must not be SUSPENDED, got State=%q", bob.State)
	}

	// An 8s-old heartbeat is stale to the kernel too: not alive and, having a
	// state, mapped to SUSPENDED by the topology build.
	staleTs := fmt.Sprintf("%f", float64(time.Now().Add(-8*time.Second).Unix()))
	os.WriteFile(filepath.Join(bobDir, ".agent.heartbeat"), []byte(staleTs), 0o644)

	net, err = BuildNetwork(base)
	if err != nil {
		t.Fatalf("rebuild network: %v", err)
	}
	bob = nil
	for i := range net.Nodes {
		if net.Nodes[i].Address == "bob" {
			bob = &net.Nodes[i]
		}
	}
	if bob == nil {
		t.Fatal("bob node not found after stale heartbeat")
	}
	if bob.Alive {
		t.Error("bob with 8s-old heartbeat should not be alive")
	}
	if bob.State != "SUSPENDED" {
		t.Errorf("bob with 8s-old heartbeat and a state should be SUSPENDED, got State=%q", bob.State)
	}
}
