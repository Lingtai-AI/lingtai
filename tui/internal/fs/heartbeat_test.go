package fs

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestAgentAliveThresholdSec_Default pins the fallback heartbeat-liveness
// window to 10s. This window is a TUI-side presentation/operational setting,
// intentionally independent of the kernel's own heartbeat liveness config.
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

func TestIsAlive_FreshHeartbeat(t *testing.T) {
	dir := t.TempDir()
	ts := fmt.Sprintf("%f", float64(time.Now().Unix()))
	os.WriteFile(filepath.Join(dir, ".agent.heartbeat"), []byte(ts), 0o644)
	if !IsAlive(dir, AgentAliveThresholdSec()) {
		t.Error("expected alive for fresh heartbeat")
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

func TestIsAlive_ElevenSecondOldHeartbeat_Stale(t *testing.T) {
	dir := t.TempDir()
	ts := fmt.Sprintf("%f", float64(time.Now().Add(-11*time.Second).Unix()))
	os.WriteFile(filepath.Join(dir, ".agent.heartbeat"), []byte(ts), 0o644)
	if IsAlive(dir, AgentAliveThresholdSec()) {
		t.Error("expected dead for 11s-old heartbeat")
	}
}

func TestIsAlive_MissingFile(t *testing.T) {
	dir := t.TempDir()
	if IsAlive(dir, AgentAliveThresholdSec()) {
		t.Error("expected dead when heartbeat file missing")
	}
}

func TestIsAlive_HumanAlwaysAlive(t *testing.T) {
	if !IsAliveHuman() {
		t.Error("human agents are always alive")
	}
}
