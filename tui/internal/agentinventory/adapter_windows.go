//go:build windows

package agentinventory

import (
	"errors"
	"syscall"
)

const (
	processQueryLimitedInformation = 0x1000
	stillActive                    = 259
	errorInvalidParameter          = syscall.Errno(87)
)

// probePID asks whether pid exists via a query-only process handle. It never
// signals or terminates; access denied still proves existence.
func probePID(pid int) (probeState, string) {
	handle, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		if errors.Is(err, errorInvalidParameter) {
			return probeAbsent, ""
		}
		if errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
			return probeAlive, ""
		}
		return probeUnknown, err.Error()
	}
	defer syscall.CloseHandle(handle)
	var code uint32
	if err := syscall.GetExitCodeProcess(handle, &code); err != nil {
		return probeUnknown, err.Error()
	}
	if code == stillActive {
		return probeAlive, ""
	}
	return probeAbsent, ""
}
