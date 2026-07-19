//go:build !windows

package duplaunch

import (
	"errors"
	"io/fs"
	"os"
	"syscall"
)

// probeKernelLock tests whether a live process holds the kernel's flock on
// path. The file is opened read-only and never created: a missing file means
// no lease, and a present but unheld file is a stale lease left by a dead
// holder (a clean kernel unlinks it on release).
func probeKernelLock(path string) Decision {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Decision{Verdict: Allow, Reason: "no kernel lock lease at " + path}
		}
		return Decision{Verdict: Unknown, Reason: "lock lease unreadable: " + err.Error()}
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if err == syscall.EWOULDBLOCK || err == syscall.EAGAIN {
			return Decision{Verdict: Block, Reason: "kernel lock held on " + path}
		}
		return Decision{Verdict: Unknown, Reason: "flock probe failed: " + err.Error()}
	}
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return Decision{Verdict: Allow, Reason: "lock lease present but unheld (stale)"}
}
