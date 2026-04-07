//go:build !windows

package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// countRunningAgents returns the number of `lingtai run` processes on this
// machine. It mirrors the discovery logic in listMain (list_unix.go) but
// only counts — no parsing of agent/project metadata, no phantom detection.
// Returns 0 on any error so the startup summary stays non-fatal.
func countRunningAgents() int {
	out, err := exec.Command("ps", "aux").Output()
	if err != nil {
		return 0
	}
	n := 0
	self := os.Getpid()
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "lingtai run") || strings.Contains(line, "grep") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 11 {
			continue
		}
		pid, err := strconv.Atoi(fields[1])
		if err != nil || pid == self {
			continue
		}
		n++
	}
	return n
}
