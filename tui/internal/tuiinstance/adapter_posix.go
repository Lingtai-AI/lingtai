//go:build !windows

package tuiinstance

import "syscall"

type lockHandle = int

// acquireLock opens (creating if absent) the lock file and takes a
// non-blocking exclusive flock. The kernel lock, not the file's
// existence, is the only evidence.
func acquireLock(path string) (lockHandle, State, string) {
	fd, err := syscall.Open(path, syscall.O_CREAT|syscall.O_RDWR|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return -1, Unknown, "open lock file: " + err.Error()
	}
	err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		return fd, Acquired, ""
	}
	syscall.Close(fd)
	if err == syscall.EWOULDBLOCK || err == syscall.EAGAIN {
		return -1, Contended, "lock held by another TUI instance"
	}
	return -1, Unknown, "flock: " + err.Error()
}

// releaseLock closes the fd, which drops the flock. The file remains.
func releaseLock(h lockHandle) {
	syscall.Close(h)
}
