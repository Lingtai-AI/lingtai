//go:build windows

package main

import (
	"strings"

	"github.com/anthropics/lingtai-tui/internal/processscan"
)

// countRunningAgents returns the number of `lingtai run` processes on this
// machine. It mirrors the discovery logic in listMain (list_windows.go) but
// only counts. Returns 0 on any error.
//
// Process enumeration is delegated to processscan.WindowsAgentProcessOutput so
// the wmic → PowerShell Get-CimInstance fallback is shared: wmic is gone from
// Windows 11 24H2+ and Server 2025, and a wmic-only scan silently reports zero
// agents there while agents are in fact running.
func countRunningAgents() int {
	out, err := processscan.WindowsAgentProcessOutput()
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
