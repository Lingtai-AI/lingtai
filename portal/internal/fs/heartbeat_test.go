package fs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestAgentAliveThresholdSec_Default pins the fallback heartbeat-liveness
// window to 10s. This window is a Portal-side presentation/operational
// setting, intentionally independent of the kernel's own heartbeat liveness
// config.
func TestAgentAliveThresholdSec_Default(t *testing.T) {
	t.Setenv(AgentAliveThresholdEnvVar, "")
	if got := AgentAliveThresholdSec(); got != 10.0 {
		t.Fatalf("AgentAliveThresholdSec() = %v, want default 10.0", got)
	}
}

func TestAgentAliveThresholdSec_ValidOverride(t *testing.T) {
	t.Setenv(AgentAliveThresholdEnvVar, "7.5")
	if got := AgentAliveThresholdSec(); got != 7.5 {
		t.Fatalf("AgentAliveThresholdSec() = %v, want overridden 7.5", got)
	}
}

func TestAgentAliveThresholdSec_InvalidFallsBackToDefault(t *testing.T) {
	cases := []string{"", "   ", "not-a-number", "0", "-1", "NaN", "Inf", "-Inf"}
	for _, raw := range cases {
		t.Run(fmt.Sprintf("%q", raw), func(t *testing.T) {
			t.Setenv(AgentAliveThresholdEnvVar, raw)
			if got := AgentAliveThresholdSec(); got != DefaultAgentAliveThresholdSec {
				t.Errorf("AgentAliveThresholdSec() with env=%q = %v, want default %v", raw, got, DefaultAgentAliveThresholdSec)
			}
		})
	}
}

func TestIsAlive_ThreeSecondOldHeartbeat_Alive(t *testing.T) {
	dir := t.TempDir()
	ts := fmt.Sprintf("%f", float64(time.Now().Add(-3*time.Second).Unix()))
	os.WriteFile(filepath.Join(dir, ".agent.heartbeat"), []byte(ts), 0o644)
	if !IsAlive(dir, AgentAliveThresholdSec()) {
		t.Error("expected alive for 3s-old heartbeat under the default 10s threshold")
	}
}

func TestIsAlive_TenSecondOldHeartbeat_Stale(t *testing.T) {
	// Strict boundary: an age at or beyond the threshold is stale
	// (age < threshold, not age <= threshold).
	dir := t.TempDir()
	ts := fmt.Sprintf("%f", float64(time.Now().Add(-10*time.Second).Unix()))
	os.WriteFile(filepath.Join(dir, ".agent.heartbeat"), []byte(ts), 0o644)
	if IsAlive(dir, AgentAliveThresholdSec()) {
		t.Error("expected stale for 10s-old heartbeat (strict age < threshold boundary)")
	}
}

// TestBuildNetwork_HeartbeatAgeDrivesAliveState is the Portal topology-level
// regression for issue #845: a 3s-old heartbeat (alive under the default
// threshold) must not be marked dead, while an 11s-old heartbeat (stale
// under the default threshold) is not alive. Neither age rewrites the
// manifest ACTIVE state: heartbeat freshness is liveness evidence (Alive),
// not permission to fabricate a lifecycle state (false-suspended fix,
// 2026-08-18).
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
		t.Errorf("bob with 3s-old heartbeat should be alive under the default 10s threshold, got Alive=false")
	}
	if bob.State == "SUSPENDED" {
		t.Errorf("bob with 3s-old heartbeat must not be SUSPENDED, got State=%q", bob.State)
	}

	// An 11s-old heartbeat is stale under the default threshold too: not
	// alive, but the manifest's ACTIVE state must survive untouched —
	// liveness and lifecycle state are observed independently.
	staleTs := fmt.Sprintf("%f", float64(time.Now().Add(-11*time.Second).Unix()))
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
		t.Error("bob with 11s-old heartbeat should not be alive")
	}
	if bob.State != "ACTIVE" {
		t.Errorf("bob with 11s-old heartbeat must keep manifest state ACTIVE, got State=%q", bob.State)
	}
}

// TestBuildNetwork_MissingHeartbeatKeepsManifestState is the missing-heartbeat
// companion to TestBuildNetwork_HeartbeatAgeDrivesAliveState: an agent with no
// heartbeat file at all must behave the same as one with a stale heartbeat —
// Alive=false without rewriting the manifest ACTIVE state (false-suspended
// fix, 2026-08-18).
func TestBuildNetwork_MissingHeartbeatKeepsManifestState(t *testing.T) {
	base := t.TempDir()

	daveDir := filepath.Join(base, "dave")
	writeAgentManifest(t, daveDir, "dave", false) // manifest state "ACTIVE", no heartbeat file written

	net, err := BuildNetwork(base)
	if err != nil {
		t.Fatalf("build network: %v", err)
	}
	var dave *AgentNode
	for i := range net.Nodes {
		if net.Nodes[i].Address == "dave" {
			dave = &net.Nodes[i]
		}
	}
	if dave == nil {
		t.Fatal("dave node not found")
	}
	if dave.Alive {
		t.Error("dave with missing heartbeat should not be alive")
	}
	if dave.State != "ACTIVE" {
		t.Errorf("dave with missing heartbeat must keep manifest state ACTIVE, got State=%q", dave.State)
	}
}

// TestBuildNetwork_SuspendedManifestStaysSuspendedWhenDead proves the
// genuine kernel/manifest SUSPENDED lifecycle state remains visibly
// SUSPENDED when its heartbeat is stale — the fix removes state fabrication
// for other states without weakening a real SUSPENDED manifest.
func TestBuildNetwork_SuspendedManifestStaysSuspendedWhenDead(t *testing.T) {
	base := t.TempDir()

	carolDir := filepath.Join(base, "carol")
	writeAgentManifest(t, carolDir, "carol", false)
	manifestPath := filepath.Join(carolDir, ".agent.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	m["state"] = "SUSPENDED"
	out, _ := json.Marshal(m)
	if err := os.WriteFile(manifestPath, out, 0o644); err != nil {
		t.Fatal(err)
	}
	staleTs := fmt.Sprintf("%f", float64(time.Now().Add(-11*time.Second).Unix()))
	os.WriteFile(filepath.Join(carolDir, ".agent.heartbeat"), []byte(staleTs), 0o644)

	net, err := BuildNetwork(base)
	if err != nil {
		t.Fatalf("build network: %v", err)
	}
	var carol *AgentNode
	for i := range net.Nodes {
		if net.Nodes[i].Address == "carol" {
			carol = &net.Nodes[i]
		}
	}
	if carol == nil {
		t.Fatal("carol node not found")
	}
	if carol.Alive {
		t.Error("carol with 11s-old heartbeat should not be alive")
	}
	if carol.State != "SUSPENDED" {
		t.Errorf("carol with genuine manifest SUSPENDED state should stay SUSPENDED, got State=%q", carol.State)
	}
}
