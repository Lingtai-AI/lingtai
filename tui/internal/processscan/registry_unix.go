//go:build !windows

package processscan

import (
	"os"
	"syscall"
)

// openRegistryFile cannot block if a regular path is replaced by a FIFO
// between Lstat and open. NOFOLLOW also prevents a swapped-in symlink.
func openRegistryFile(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	syscall.CloseOnExec(fd)
	return os.NewFile(uintptr(fd), path), nil
}
