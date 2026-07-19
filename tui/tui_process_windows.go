//go:build windows

package main

import (
	"fmt"

	"github.com/anthropics/lingtai-tui/internal/stopprocess"
)

func findOtherTUIProcesses() []runningTUIProcess {
	return nil
}

// stopTUIProcess delivers one forceful, identity-verified native stop
// request; delivery is not exit proof and there is no wait or escalation.
func stopTUIProcess(pid int) error {
	res := stopprocess.Deliver(stopprocess.Target{PID: pid, Executable: "lingtai-tui"}, stopprocess.Forceful)
	if res.Outcome == stopprocess.Delivered || res.Outcome == stopprocess.AlreadyAbsent {
		return nil
	}
	if res.Err != nil {
		return fmt.Errorf("stop lingtai-tui pid %d: %s: %w", pid, res.Outcome, res.Err)
	}
	return fmt.Errorf("stop lingtai-tui pid %d: %s", pid, res.Outcome)
}
