//go:build !windows

package termobs

import (
	"errors"
	"os"
	"os/exec"
	"testing"
)

func TestObserveLiveThenExitedChild(t *testing.T) {
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	inc, err := Identify(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("Identify(child): %v", err)
	}
	if got := Observe(inc); got.Status != StatusRunning {
		t.Fatalf("Observe(live child) = %v (%v), want running", got.Status, got.Err)
	}
	// The test harness ends its own child; the interface only observes.
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	if got := Observe(inc); got.Status != StatusExited {
		t.Fatalf("Observe(reaped child) = %v (%v), want exited", got.Status, got.Err)
	}
	if _, err := Identify(cmd.Process.Pid); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Identify(gone PID) = %v, want ErrNotFound", err)
	}
}

func TestExactIncarnationAndInvalidInputs(t *testing.T) {
	inc, err := Identify(os.Getpid())
	if err != nil {
		t.Fatalf("Identify(self): %v", err)
	}
	forged := Incarnation{PID: inc.PID, token: inc.token + 1}
	if got := Observe(forged); got.Status != StatusExited {
		t.Fatalf("Observe(stale token) = %v, want exited", got.Status)
	}
	if _, err := Identify(0); err == nil {
		t.Fatal("Identify(0) succeeded, want error")
	}
	if got := Observe(Incarnation{}); got.Status != StatusError {
		t.Fatalf("Observe(zero) = %v, want error", got.Status)
	}
}
