//go:build darwin

package stopprocess

import (
	"errors"
	"syscall"

	"golang.org/x/sys/unix"
)

func deliver(t Target, k Kind) Result {
	if err := syscall.Kill(t.PID, 0); errors.Is(err, syscall.ESRCH) {
		return Result{Outcome: AlreadyAbsent}
	}
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", t.PID)
	if err != nil {
		return Result{Outcome: Unknown, Err: err}
	}
	var comm []byte
	for _, c := range kp.Proc.P_comm {
		if c == 0 {
			break
		}
		comm = append(comm, byte(c))
	}
	// The kernel truncates p_comm to 16 bytes (MAXCOMLEN).
	if string(comm) != t.Executable[:min(len(t.Executable), 16)] {
		return Result{Outcome: StaleIdentity}
	}
	sig := syscall.SIGTERM
	if k == Forceful {
		sig = syscall.SIGKILL
	}
	switch err := syscall.Kill(t.PID, sig); {
	case err == nil:
		return Result{Outcome: Delivered}
	case errors.Is(err, syscall.ESRCH):
		return Result{Outcome: AlreadyAbsent}
	case errors.Is(err, syscall.EPERM):
		return Result{Outcome: AccessDenied, Err: err}
	default:
		return Result{Outcome: Unknown, Err: err}
	}
}
