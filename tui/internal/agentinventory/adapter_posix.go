//go:build !windows

package agentinventory

import (
	"errors"
	"syscall"
)

// probePID asks whether pid exists via kill(pid, 0). Signal 0 delivers
// nothing; EPERM still proves existence.
func probePID(pid int) (probeState, string) {
	err := syscall.Kill(pid, 0)
	switch {
	case err == nil, errors.Is(err, syscall.EPERM):
		return probeAlive, ""
	case errors.Is(err, syscall.ESRCH):
		return probeAbsent, ""
	default:
		return probeUnknown, err.Error()
	}
}
