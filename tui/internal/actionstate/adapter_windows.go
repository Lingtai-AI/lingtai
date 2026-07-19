//go:build windows

package actionstate

import (
	"errors"
	iofs "io/fs"
	"syscall"
)

// heartbeatAbsent reports whether err proves the heartbeat file does not
// exist, as opposed to being unreadable for any other reason.
func heartbeatAbsent(err error) bool {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno == syscall.ERROR_FILE_NOT_FOUND || errno == syscall.ERROR_PATH_NOT_FOUND
	}
	return errors.Is(err, iofs.ErrNotExist)
}
