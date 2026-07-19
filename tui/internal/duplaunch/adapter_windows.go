//go:build windows

package duplaunch

import (
	"errors"
	"io/fs"
	"os"
	"syscall"
	"unsafe"
)

// A running kernel holds a 1-byte msvcrt region lock at offset 0 of
// .agent.lock. LockFileEx with fail-immediately on that same byte is the
// non-creating probe: a lock violation proves a live holder.
var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = kernel32.NewProc("LockFileEx")
	procUnlockFileEx = kernel32.NewProc("UnlockFileEx")
)

const (
	lockfileFailImmediately = 0x00000001
	lockfileExclusiveLock   = 0x00000002
	errSharingViolation     = syscall.Errno(32)
	errLockViolation        = syscall.Errno(33)
)

func probeKernelLock(path string) Decision {
	f, err := os.Open(path)
	if err != nil {
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return Decision{Verdict: Allow, Reason: "no kernel lock lease at " + path}
		case errors.Is(err, errSharingViolation):
			return Decision{Verdict: Block, Reason: "lock lease held open exclusively: " + path}
		default:
			return Decision{Verdict: Unknown, Reason: "lock lease unreadable: " + err.Error()}
		}
	}
	defer f.Close()
	var ol syscall.Overlapped
	ok, _, callErr := procLockFileEx.Call(
		f.Fd(), lockfileExclusiveLock|lockfileFailImmediately,
		0, 1, 0, uintptr(unsafe.Pointer(&ol)),
	)
	if ok == 0 {
		if errors.Is(callErr, errLockViolation) {
			return Decision{Verdict: Block, Reason: "kernel lock held on " + path}
		}
		return Decision{Verdict: Unknown, Reason: "LockFileEx probe failed: " + callErr.Error()}
	}
	procUnlockFileEx.Call(f.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&ol)))
	return Decision{Verdict: Allow, Reason: "lock lease present but unheld (stale)"}
}
