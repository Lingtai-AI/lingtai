//go:build linux

package stopprocess

import (
	"errors"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"syscall"
)

func deliver(t Target, k Kind) Result {
	comm, err := os.ReadFile("/proc/" + strconv.Itoa(t.PID) + "/comm")
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
		return Result{Outcome: AlreadyAbsent}
	}
	if err != nil {
		return Result{Outcome: Unknown, Err: err}
	}
	// The kernel truncates comm to 15 bytes (TASK_COMM_LEN - 1).
	if strings.TrimSpace(string(comm)) != t.Executable[:min(len(t.Executable), 15)] {
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
