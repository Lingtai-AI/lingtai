package tui

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

// stubLaunchHeartbeat overrides the waitForLaunchHeartbeat dependencies so
// tests can simulate heartbeat freshness and process liveness without
// launching a real agent. The cap is shrunk so the alive-grace path is
// exercised in milliseconds.
func stubLaunchHeartbeat(t *testing.T, alive func(string, float64) bool, running func(string) bool) {
	t.Helper()
	origAlive := launchHeartbeatIsAlive
	origRunning := launchHeartbeatIsRunning
	origPoll := launchHeartbeatPoll
	origCap := launchHeartbeatCap
	launchHeartbeatIsAlive = alive
	launchHeartbeatIsRunning = running
	launchHeartbeatPoll = time.Millisecond
	launchHeartbeatCap = 60 * time.Millisecond
	t.Cleanup(func() {
		launchHeartbeatIsAlive = origAlive
		launchHeartbeatIsRunning = origRunning
		launchHeartbeatPoll = origPoll
		launchHeartbeatCap = origCap
	})
}

func TestWaitForLaunchHeartbeatFresh(t *testing.T) {
	stubLaunchHeartbeat(t,
		func(string, float64) bool { return true },
		func(string) bool { return true },
	)
	if err := waitForLaunchHeartbeat(&exec.Cmd{}, t.TempDir(), 10*time.Second); err != nil {
		t.Fatalf("fresh heartbeat should succeed immediately, got: %v", err)
	}
}

func TestWaitForLaunchHeartbeatExitedFailsFast(t *testing.T) {
	stubLaunchHeartbeat(t,
		func(string, float64) bool { return false },
		func(string) bool { return false },
	)
	start := time.Now()
	err := waitForLaunchHeartbeat(&exec.Cmd{}, t.TempDir(), 10*time.Second)
	if err == nil {
		t.Fatal("expected error when the launched process exits before a heartbeat")
	}
	if !strings.Contains(err.Error(), "exited before writing a fresh heartbeat") {
		t.Fatalf("unexpected error text: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("process-exit failure should be fast, took %v", elapsed)
	}
}

func TestWaitForLaunchHeartbeatSlowStartSucceeds(t *testing.T) {
	// Simulates an agent whose first heartbeat appears only after the initial
	// timeout (e.g. serial stdio MCP addon initialization): the process stays
	// alive and eventually heartbeats, so the launch must not be reported as
	// failed at the initial deadline.
	var calls int
	stubLaunchHeartbeat(t,
		func(string, float64) bool { calls++; return calls >= 5 },
		func(string) bool { return true },
	)
	start := time.Now()
	if err := waitForLaunchHeartbeat(&exec.Cmd{}, t.TempDir(), 2*time.Millisecond); err != nil {
		t.Fatalf("slow start should succeed once the heartbeat appears, got: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 2*time.Millisecond {
		t.Fatalf("success should happen after the initial deadline, took %v", elapsed)
	}
}

func TestWaitForLaunchHeartbeatAliveNoHeartbeatTimesOutAtCap(t *testing.T) {
	// The process stays alive but never writes a heartbeat: the verdict must
	// wait until launchHeartbeatCap (not the initial deadline) before failing.
	stubLaunchHeartbeat(t,
		func(string, float64) bool { return false },
		func(string) bool { return true },
	)
	start := time.Now()
	err := waitForLaunchHeartbeat(&exec.Cmd{}, t.TempDir(), 5*time.Millisecond)
	if err == nil {
		t.Fatal("expected a timeout error when no heartbeat ever appears")
	}
	if !strings.Contains(err.Error(), "did not write a fresh heartbeat within") {
		t.Fatalf("unexpected error text: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 50*time.Millisecond {
		t.Fatalf("should keep waiting while the process is alive up to the cap, returned after %v", elapsed)
	}
}
