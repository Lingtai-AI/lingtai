//go:build windows

package fs

import (
	"os"

	"golang.org/x/sys/windows"
)

func replaceFile(from, to string) error {
	fromPointer, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	toPointer, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(fromPointer, toPointer, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

func lockFileExclusive(file *os.File) error {
	var overlapped windows.Overlapped
	return windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, ^uint32(0), ^uint32(0), &overlapped)
}

func unlockFile(file *os.File) error {
	var overlapped windows.Overlapped
	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, ^uint32(0), ^uint32(0), &overlapped)
}
