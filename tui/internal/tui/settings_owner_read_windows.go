//go:build windows

package tui

import (
	"golang.org/x/sys/windows"
	"os"
)

// Open the owner itself, not a reparse-point target; common code verifies the
// opened handle against Lstat before reading any bytes.
func openSettingsPsycheOwner(path string) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), path), nil
}
