//go:build !windows

package fs

import (
	"os"
	"syscall"
)

func replaceFile(from, to string) error {
	return os.Rename(from, to)
}

func lockFileExclusive(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_EX)
}

func unlockFile(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}
