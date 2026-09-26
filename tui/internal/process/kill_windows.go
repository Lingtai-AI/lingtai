//go:build windows

package process

import (
	"os"
	"strconv"
	"time"
)

// TerminateAgentProcesses ends every `lingtai run <agentDir>` process using
// os.Process.Kill (mapped to TerminateProcess on Windows), waits briefly for
// them to exit, then returns nil if the agent dir is clear.
//
// Used by /refresh to forcibly clear an agent that did not honor `.suspend`.
// Normal launch paths must NOT call this — duplicate-launch protection in
// LaunchAgent assumes the prior agent shut down gracefully.
func TerminateAgentProcesses(agentDir string) error {
	procs, err := tuiOwnedRunProcesses(agentDir)
	if err != nil {
		return err
	}
	if len(procs) == 0 {
		return nil
	}
	for _, p := range procs {
		if proc, err := os.FindProcess(p.PID); err == nil {
			_ = proc.Kill()
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		remaining, err := tuiOwnedRunProcesses(agentDir)
		if err != nil {
			return err
		}
		if len(remaining) == 0 {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	remaining, err := tuiOwnedRunProcesses(agentDir)
	if err != nil {
		return err
	}
	if len(remaining) > 0 {
		return &terminateError{remaining: len(remaining)}
	}
	return nil
}

type terminateError struct{ remaining int }

func (e *terminateError) Error() string {
	return "agent processes still alive after kill (" + strconv.Itoa(e.remaining) + ")"
}
