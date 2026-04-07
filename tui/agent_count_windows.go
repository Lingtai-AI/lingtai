//go:build windows

package main

import (
	"os/exec"
	"strings"
)

// countRunningAgents returns the number of `lingtai run` processes on this
// machine. It mirrors the discovery logic in listMain (list_windows.go) but
// only counts. Returns 0 on any error.
func countRunningAgents() int {
	out, err := exec.Command("wmic", "process", "where",
		"commandline like '%lingtai run%'",
		"get", "processid,commandline", "/format:list").Output()
	if err != nil {
		return 0
	}
	n := 0
	var cmdline string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "CommandLine=") {
			cmdline = strings.TrimPrefix(line, "CommandLine=")
		}
		if strings.HasPrefix(line, "ProcessId=") {
			if cmdline != "" && strings.Contains(cmdline, "lingtai run") {
				n++
			}
			cmdline = ""
		}
	}
	return n
}
