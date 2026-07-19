//go:build !windows

package duplaunch

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestCheckAllowsWhenNoLeaseAndNeverCreatesIt(t *testing.T) {
	dir := t.TempDir()
	if d := Check(dir); d.Verdict != Allow {
		t.Fatalf("verdict = %s (%s), want allow", d.Verdict, d.Reason)
	}
	if _, err := os.Stat(filepath.Join(dir, kernelLockFile)); !os.IsNotExist(err) {
		t.Fatalf("probe must not create the lock file (stat err=%v)", err)
	}
}

func TestCheckAllowsWhenLeaseStale(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, kernelLockFile), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if d := Check(dir); d.Verdict != Allow {
		t.Fatalf("verdict = %s (%s), want allow for unheld lease", d.Verdict, d.Reason)
	}
}

func TestCheckBlocksWhenLeaseHeld(t *testing.T) {
	dir := t.TempDir()
	holder, err := os.OpenFile(filepath.Join(dir, kernelLockFile), os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	if err := syscall.Flock(int(holder.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("acquire holder flock: %v", err)
	}
	defer syscall.Flock(int(holder.Fd()), syscall.LOCK_UN)
	if d := Check(dir); d.Verdict != Block {
		t.Fatalf("verdict = %s (%s), want block", d.Verdict, d.Reason)
	}
}

func TestCheckUnknownWhenLeaseUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, kernelLockFile), nil, 0o000); err != nil {
		t.Fatal(err)
	}
	if d := Check(dir); d.Verdict != Unknown {
		t.Fatalf("verdict = %s (%s), want unknown", d.Verdict, d.Reason)
	}
}
