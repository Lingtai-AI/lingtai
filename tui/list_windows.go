//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func listMain() {
	out, err := exec.Command("wmic", "process", "where",
		"commandline like '%lingtai run%'",
		"get", "processid,commandline", "/format:list").Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error listing processes: %v\n", err)
		os.Exit(1)
	}

	type proc struct {
		pid   string
		agent string
	}

	var procs []proc
	var cmdline, pid string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "CommandLine=") {
			cmdline = strings.TrimPrefix(line, "CommandLine=")
		}
		if strings.HasPrefix(line, "ProcessId=") {
			pid = strings.TrimPrefix(line, "ProcessId=")
			if cmdline != "" && strings.Contains(cmdline, "lingtai run") {
				// Extract agent dir
				agent := "unknown"
				if idx := strings.Index(cmdline, "lingtai run "); idx >= 0 {
					dir := cmdline[idx+len("lingtai run "):]
					dir = strings.TrimSpace(strings.Split(dir, " ")[0])
					agent = filepath.Base(dir)
				}
				procs = append(procs, proc{pid: pid, agent: agent})
			}
			cmdline = ""
			pid = ""
		}
	}

	if len(procs) == 0 {
		fmt.Println("No lingtai processes running.")
		return
	}

	fmt.Printf("%-8s %s\n", "PID", "AGENT")
	for _, p := range procs {
		fmt.Printf("%-8s %s\n", p.pid, p.agent)
	}
	fmt.Printf("\n%d process(es) running.\n", len(procs))
}
