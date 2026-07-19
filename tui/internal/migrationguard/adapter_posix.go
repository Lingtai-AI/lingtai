//go:build !windows

package migrationguard

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"
)

// probeKernelLock tests the kernel's flock on lockPath without creating it.
// A missing lock file means no kernel has locked this directory (the kernel
// creates the file on acquire), so the action is allowed.
func probeKernelLock(lockPath string) Decision {
	f, err := os.OpenFile(lockPath, os.O_RDONLY, 0)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Decision{Allow, "kernel lock file does not exist"}
		}
		return Decision{Unknown, fmt.Sprintf("kernel lock file unreadable: %v", err)}
	}
	defer f.Close()
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		return Decision{Allow, "kernel lock is free"}
	}
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		return Decision{Block, "kernel lock is held"}
	}
	return Decision{Unknown, fmt.Sprintf("kernel lock probe failed: %v", err)}
}
