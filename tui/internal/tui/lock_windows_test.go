//go:build windows

package tui

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestTryLockWindowsReportsHeldAndReleasedLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agent.lock")
	holder, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()

	var overlapped windows.Overlapped
	if err := windows.LockFileEx(
		windows.Handle(holder.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		^uint32(0),
		^uint32(0),
		&overlapped,
	); err != nil {
		t.Fatalf("lock held file: %v", err)
	}

	if tryLock(path) {
		t.Fatalf("tryLock(%q) = true while another handle holds the lock", path)
	}

	if err := windows.UnlockFileEx(windows.Handle(holder.Fd()), 0, ^uint32(0), ^uint32(0), &overlapped); err != nil {
		t.Fatalf("unlock held file: %v", err)
	}
	if !tryLock(path) {
		t.Fatalf("tryLock(%q) = false after releasing the lock", path)
	}
}
