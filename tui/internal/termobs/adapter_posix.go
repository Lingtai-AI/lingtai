//go:build !windows

package termobs

import (
	"fmt"
	"os"
)

func identify(pid int) (Incarnation, error) {
	if pid <= 0 {
		return Incarnation{}, fmt.Errorf("termobs: invalid PID %d", pid)
	}
	token, found, err := readStartToken(pid)
	if err != nil {
		return Incarnation{}, fmt.Errorf("termobs: identify PID %d: %w", pid, err)
	}
	if !found {
		return Incarnation{}, ErrNotFound
	}
	return Incarnation{PID: pid, token: token}, nil
}

func observe(inc Incarnation) Observation {
	if inc.PID <= 0 {
		return Observation{Status: StatusError, Err: fmt.Errorf("termobs: invalid incarnation PID %d", inc.PID)}
	}
	token, found, err := readStartToken(inc.PID)
	switch {
	case err != nil && os.IsPermission(err):
		return Observation{Status: StatusUnknown, Err: err}
	case err != nil:
		return Observation{Status: StatusError, Err: err}
	case !found: // no process holds the PID: the incarnation exited
		return Observation{Status: StatusExited}
	case token != inc.token: // PID reused: the identified incarnation exited
		return Observation{Status: StatusExited}
	default:
		return Observation{Status: StatusRunning}
	}
}
