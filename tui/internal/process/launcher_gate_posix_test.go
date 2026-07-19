//go:build !windows

package process

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// The launch gate must refuse on the duplicate-launch check's Block verdict
// (a held kernel .agent.lock lease) before doing any launch work.
func TestLaunchAgentRefusesWhenKernelLockHeld(t *testing.T) {
	dir := t.TempDir()
	holder, err := os.OpenFile(filepath.Join(dir, ".agent.lock"), os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	if err := syscall.Flock(int(holder.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("acquire holder flock: %v", err)
	}
	if _, err := LaunchAgent("python3", dir); !errors.Is(err, ErrAgentAlreadyRunning) {
		t.Fatalf("err = %v, want ErrAgentAlreadyRunning", err)
	}
}
