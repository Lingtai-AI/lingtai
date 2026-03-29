package fs

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Signal string

const (
	SignalSleep     Signal = ".sleep"
	SignalSuspend   Signal = ".suspend"
	SignalInterrupt Signal = ".interrupt"
)

func TouchSignal(dir string, sig Signal) error {
	return os.WriteFile(filepath.Join(dir, string(sig)), nil, 0o644)
}

func HasSignal(dir string, sig Signal) bool {
	_, err := os.Stat(filepath.Join(dir, string(sig)))
	return err == nil
}

func CleanSignals(dir string) {
	for _, sig := range []Signal{SignalSleep, SignalSuspend, SignalInterrupt} {
		os.Remove(filepath.Join(dir, string(sig)))
	}
}

// KillAgent terminates an agent by PID (SIGTERM), falling back to .suspend signal.
// Waits up to timeout for the process to die.
func KillAgent(dir string, timeout time.Duration) {
	if !IsAlive(dir, 2.0) {
		return
	}

	// Try PID file first (direct kill)
	pidFile := filepath.Join(dir, ".pid")
	if data, err := os.ReadFile(pidFile); err == nil {
		pidStr := strings.TrimSpace(string(data))
		if pid, err := strconv.Atoi(pidStr); err == nil && pid > 0 {
			if proc, err := os.FindProcess(pid); err == nil {
				proc.Signal(syscall.SIGTERM)
			}
		}
	}

	// Also write .suspend as fallback (in case PID file was stale)
	TouchSignal(dir, SignalSuspend)

	// Wait for death
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		if !IsAlive(dir, 2.0) {
			return
		}
	}
}
