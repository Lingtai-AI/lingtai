//go:build windows

package tui

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"unsafe"
)

var (
	testKernel32         = syscall.NewLazyDLL("kernel32.dll")
	testProcLockFileEx   = testKernel32.NewProc("LockFileEx")
	testProcUnlockFileEx = testKernel32.NewProc("UnlockFileEx")
)

const (
	testLockfileFailImmediately = 0x00000001
	testLockfileExclusiveLock   = 0x00000002
)

func TestTryLockWindowsHonorsHeldByteZero(t *testing.T) {
	agentDir := t.TempDir()
	lockPath := filepath.Join(agentDir, ".agent.lock")

	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("create lock fixture: %v", err)
	}
	defer f.Close()

	var ol syscall.Overlapped
	ok, _, callErr := testProcLockFileEx.Call(
		f.Fd(),
		testLockfileExclusiveLock|testLockfileFailImmediately,
		0,
		1,
		0,
		uintptr(unsafe.Pointer(&ol)),
	)
	if ok == 0 {
		t.Fatalf("lock byte 0 fixture: %v", callErr)
	}

	if tryLock(lockPath) {
		t.Fatalf("tryLock returned true while byte 0 was held")
	}

	ok, _, callErr = testProcUnlockFileEx.Call(f.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&ol)))
	if ok == 0 {
		t.Fatalf("unlock byte 0 fixture: %v", callErr)
	}

	if !tryLock(lockPath) {
		t.Fatalf("tryLock returned false for existing unlocked lock file")
	}
}

func TestTryLockWindowsAllowsMissingWithoutCreating(t *testing.T) {
	agentDir := t.TempDir()
	lockPath := filepath.Join(agentDir, ".agent.lock")

	if !tryLock(lockPath) {
		t.Fatalf("tryLock returned false for missing lock file")
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("missing lock probe created or changed fixture, stat err=%v", err)
	}
}

func TestTryLockWindowsFailsClosedOnUnknownProbe(t *testing.T) {
	parentFile := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(parentFile, []byte("fixture"), 0o600); err != nil {
		t.Fatalf("write parent file fixture: %v", err)
	}

	lockPath := filepath.Join(parentFile, ".agent.lock")
	if tryLock(lockPath) {
		t.Fatalf("tryLock returned true for an unusable lock probe path")
	}
}
