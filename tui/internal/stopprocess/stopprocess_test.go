package stopprocess_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/lingtai-tui/internal/stopprocess"
)

func TestMain(m *testing.M) {
	if os.Getenv("STOPPROCESS_TEST_HELPER") == "1" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// spawnHelper re-executes this test binary as a sleeping child of known identity.
func spawnHelper(t *testing.T) (*exec.Cmd, string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^$")
	cmd.Env = append(os.Environ(), "STOPPROCESS_TEST_HELPER=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	return cmd, strings.TrimSuffix(filepath.Base(exe), ".exe")
}

func nativeKind() stopprocess.Kind {
	if runtime.GOOS == "windows" {
		return stopprocess.Forceful
	}
	return stopprocess.Graceful
}

func TestDeliverStopsVerifiedTarget(t *testing.T) {
	cmd, name := spawnHelper(t)
	res := stopprocess.Deliver(stopprocess.Target{PID: cmd.Process.Pid, Executable: name}, nativeKind())
	if res.Outcome != stopprocess.Delivered {
		t.Fatalf("outcome = %s (err %v), want delivered", res.Outcome, res.Err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("target still running after delivered stop request")
	}
}

func TestDeliverRefusesStaleIdentity(t *testing.T) {
	cmd, name := spawnHelper(t)
	res := stopprocess.Deliver(stopprocess.Target{PID: cmd.Process.Pid, Executable: "not-this-process"}, nativeKind())
	if res.Outcome != stopprocess.StaleIdentity {
		t.Fatalf("outcome = %s (err %v), want stale-identity", res.Outcome, res.Err)
	}
	// The refused target must be untouched: a correctly named retry delivers.
	if res := stopprocess.Deliver(stopprocess.Target{PID: cmd.Process.Pid, Executable: name}, nativeKind()); res.Outcome != stopprocess.Delivered {
		t.Fatalf("retry outcome = %s (err %v), want delivered", res.Outcome, res.Err)
	}
}

func TestDeliverReportsAlreadyAbsent(t *testing.T) {
	cmd, name := spawnHelper(t)
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	res := stopprocess.Deliver(stopprocess.Target{PID: cmd.Process.Pid, Executable: name}, nativeKind())
	if res.Outcome != stopprocess.AlreadyAbsent {
		t.Fatalf("outcome = %s (err %v), want already-absent", res.Outcome, res.Err)
	}
}

func TestEdgeOutcomes(t *testing.T) {
	if res := stopprocess.Deliver(stopprocess.Target{PID: 0, Executable: "x"}, stopprocess.Forceful); res.Outcome != stopprocess.Unknown || res.Err == nil {
		t.Fatalf("invalid target: outcome = %s (err %v), want unknown with error", res.Outcome, res.Err)
	}
	if runtime.GOOS == "windows" {
		if res := stopprocess.Deliver(stopprocess.Target{PID: os.Getpid(), Executable: "x"}, stopprocess.Graceful); res.Outcome != stopprocess.Unsupported {
			t.Fatalf("graceful on windows: outcome = %s, want unsupported", res.Outcome)
		}
	}
}
