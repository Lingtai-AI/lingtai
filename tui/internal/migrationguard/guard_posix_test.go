//go:build !windows

package migrationguard

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestCheckAgentDirMissingLockFileAllows(t *testing.T) {
	d := CheckAgentDir(t.TempDir())
	if d.Verdict != Allow || !d.Permitted() {
		t.Fatalf("missing lock file: got %+v, want Allow", d)
	}
}

func TestCheckAgentDirFreeLockAllows(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, kernelLockFile), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	d := CheckAgentDir(dir)
	if d.Verdict != Allow || !d.Permitted() {
		t.Fatalf("free lock: got %+v, want Allow", d)
	}
}

func TestCheckAgentDirHeldLockBlocks(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, kernelLockFile)
	// Hold the lock through a separate open file description, as the kernel
	// process would; flock conflicts are per description, so this contends.
	holder, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	if err := syscall.Flock(int(holder.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	d := CheckAgentDir(dir)
	if d.Verdict != Block || d.Permitted() {
		t.Fatalf("held lock: got %+v, want Block", d)
	}
}

func TestCheckAgentDirUnreadableLockIsUnknownAndBlocks(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission probes do not fail as root")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, kernelLockFile), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	d := CheckAgentDir(dir)
	if d.Verdict != Unknown || d.Permitted() || d.Reason == "" {
		t.Fatalf("unreadable lock: got %+v, want Unknown with reason", d)
	}
}

func TestZeroValueVerdictDoesNotPermit(t *testing.T) {
	var d Decision
	if d.Verdict != Unknown || d.Permitted() {
		t.Fatalf("zero Decision: got %+v, want Unknown and not permitted", d)
	}
}
