//go:build windows

package termobs

import (
	"fmt"
	"syscall"
)

// Observation-only access rights; the terminate right is never requested.
const (
	processQueryLimitedInformation = 0x1000
	synchronizeRight               = 0x00100000
	errorInvalidParameter          = syscall.Errno(87)
)

func openTarget(pid int) (syscall.Handle, error) {
	return syscall.OpenProcess(processQueryLimitedInformation|synchronizeRight, false, uint32(pid))
}

func creationToken(h syscall.Handle) (uint64, error) {
	var creation, exit, kernel, user syscall.Filetime
	if err := syscall.GetProcessTimes(h, &creation, &exit, &kernel, &user); err != nil {
		return 0, err
	}
	return uint64(creation.HighDateTime)<<32 | uint64(creation.LowDateTime), nil
}

func identify(pid int) (Incarnation, error) {
	if pid <= 0 {
		return Incarnation{}, fmt.Errorf("termobs: invalid PID %d", pid)
	}
	h, err := openTarget(pid)
	if err == errorInvalidParameter {
		return Incarnation{}, ErrNotFound
	}
	if err != nil {
		return Incarnation{}, fmt.Errorf("termobs: identify PID %d: %w", pid, err)
	}
	defer syscall.CloseHandle(h)
	token, err := creationToken(h)
	if err != nil {
		return Incarnation{}, fmt.Errorf("termobs: identify PID %d: %w", pid, err)
	}
	return Incarnation{PID: pid, token: token}, nil
}

func observe(inc Incarnation) Observation {
	if inc.PID <= 0 {
		return Observation{Status: StatusError, Err: fmt.Errorf("termobs: invalid incarnation PID %d", inc.PID)}
	}
	h, err := openTarget(inc.PID)
	switch {
	case err == errorInvalidParameter: // no process holds this PID
		return Observation{Status: StatusExited}
	case err == syscall.ERROR_ACCESS_DENIED:
		return Observation{Status: StatusUnknown, Err: err}
	case err != nil:
		return Observation{Status: StatusError, Err: err}
	}
	defer syscall.CloseHandle(h)
	token, err := creationToken(h)
	if err != nil {
		return Observation{Status: StatusError, Err: err}
	}
	if token != inc.token { // PID reused: the identified incarnation exited
		return Observation{Status: StatusExited}
	}
	// Zero-timeout wait: exited (signaled) vs running (timeout).
	event, err := syscall.WaitForSingleObject(h, 0)
	switch event {
	case syscall.WAIT_OBJECT_0:
		return Observation{Status: StatusExited}
	case uint32(syscall.WAIT_TIMEOUT):
		return Observation{Status: StatusRunning}
	default:
		if err == nil {
			err = fmt.Errorf("termobs: unexpected wait event %#x", event)
		}
		return Observation{Status: StatusError, Err: err}
	}
}
