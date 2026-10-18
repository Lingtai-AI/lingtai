//go:build !windows

package tui

import (
	"os"
	"syscall"
)

// Limited to Psyche owner reads: a swapped-in FIFO cannot block and a swapped
// symlink is refused. Type/identity/size are checked again through the descriptor.
func openSettingsPsycheOwner(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	syscall.CloseOnExec(fd)
	return os.NewFile(uintptr(fd), path), nil
}
