//go:build windows

package migrationguard

import (
	"errors"
	"fmt"
	"io/fs"
	"syscall"
	"unsafe"
)

// The kernel holds a one-byte range lock at offset 0 of the lock file. A
// fail-immediately exclusive LockFileEx probe on that same byte conflicts
// with it, giving a faithful non-creating test.
var (
	guardKernel32     = syscall.NewLazyDLL("kernel32.dll")
	guardLockFileEx   = guardKernel32.NewProc("LockFileEx")
	guardUnlockFileEx = guardKernel32.NewProc("UnlockFileEx")
)

const (
	guardLockExclusive       = 0x00000002        // LOCKFILE_EXCLUSIVE_LOCK
	guardLockFailImmediately = 0x00000001        // LOCKFILE_FAIL_IMMEDIATELY
	guardErrLockViolation    = syscall.Errno(33) // ERROR_LOCK_VIOLATION
)

// probeKernelLock tests the kernel's range lock on lockPath without creating
// the file. OPEN_EXISTING keeps the probe non-creating; full sharing avoids
// disturbing the running kernel's open handle.
func probeKernelLock(lockPath string) Decision {
	name, err := syscall.UTF16PtrFromString(lockPath)
	if err != nil {
		return Decision{Unknown, fmt.Sprintf("kernel lock path invalid: %v", err)}
	}
	h, err := syscall.CreateFile(name, syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Decision{Allow, "kernel lock file does not exist"}
		}
		return Decision{Unknown, fmt.Sprintf("kernel lock file unreadable: %v", err)}
	}
	defer syscall.CloseHandle(h)
	var ov syscall.Overlapped // zero offset: the byte the kernel locks
	r, _, callErr := guardLockFileEx.Call(uintptr(h),
		guardLockExclusive|guardLockFailImmediately, 0, 1, 0,
		uintptr(unsafe.Pointer(&ov)))
	if r == 0 {
		if errno, ok := callErr.(syscall.Errno); ok && errno == guardErrLockViolation {
			return Decision{Block, "kernel lock is held"}
		}
		return Decision{Unknown, fmt.Sprintf("kernel lock probe failed: %v", callErr)}
	}
	guardUnlockFileEx.Call(uintptr(h), 0, 1, 0, uintptr(unsafe.Pointer(&ov)))
	return Decision{Allow, "kernel lock is free"}
}
