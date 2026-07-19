//go:build windows

package tuiinstance

import "syscall"

type lockHandle = syscall.Handle

// Not exported by package syscall; values are stable Win32 constants.
const (
	errorSharingViolation syscall.Errno = 32
	errorLockViolation    syscall.Errno = 33
)

// acquireLock opens (creating if absent) the lock file with share mode
// 0: while this handle is held, any second open fails with a sharing
// violation. The held handle itself is the lock; file existence, age,
// and content are never evidence.
func acquireLock(path string) (lockHandle, State, string) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return syscall.InvalidHandle, Unknown, "lock path: " + err.Error()
	}
	h, err := syscall.CreateFile(p,
		syscall.GENERIC_READ|syscall.GENERIC_WRITE,
		0, // no sharing
		nil, syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err == nil {
		return h, Acquired, ""
	}
	if errno, ok := err.(syscall.Errno); ok &&
		(errno == errorSharingViolation || errno == errorLockViolation) {
		return syscall.InvalidHandle, Contended, "lock held by another TUI instance"
	}
	return syscall.InvalidHandle, Unknown, "CreateFile: " + err.Error()
}

// releaseLock closes the handle, releasing the sharing lock. The file
// remains.
func releaseLock(h lockHandle) {
	syscall.CloseHandle(h)
}
