//go:build windows

package stopprocess

import (
	"errors"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// stillActive is GetExitCodeProcess's sentinel for a running process.
const stillActive = 259

func deliver(t Target, k Kind) Result {
	if k == Graceful { // no reliable native cross-process graceful stop
		return Result{Outcome: Unsupported}
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE, false, uint32(t.PID))
	switch {
	case errors.Is(err, windows.ERROR_INVALID_PARAMETER):
		return Result{Outcome: AlreadyAbsent}
	case errors.Is(err, windows.ERROR_ACCESS_DENIED):
		return Result{Outcome: AccessDenied, Err: err}
	case err != nil:
		return Result{Outcome: Unknown, Err: err}
	}
	// Verify and terminate via one held handle: no PID recycling in between.
	defer windows.CloseHandle(h)
	var buf [windows.MAX_PATH]uint16
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return Result{Outcome: Unknown, Err: err}
	}
	img := strings.TrimSuffix(strings.ToLower(filepath.Base(windows.UTF16ToString(buf[:n]))), ".exe")
	if img != strings.ToLower(t.Executable) {
		return Result{Outcome: StaleIdentity}
	}
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err == nil && code != stillActive {
		return Result{Outcome: AlreadyAbsent}
	}
	if err := windows.TerminateProcess(h, 1); err != nil {
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return Result{Outcome: AccessDenied, Err: err}
		}
		return Result{Outcome: Unknown, Err: err}
	}
	return Result{Outcome: Delivered}
}
