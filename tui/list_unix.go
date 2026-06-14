//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func listMain() {
	opts, err := parseListArgs(os.Args[2:])
	if err != nil {
		listUsageError(err)
	}

	out, err := exec.Command("ps", "aux").Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error running ps: %v\n", err)
		os.Exit(1)
	}

	var procs []listProc
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "lingtai run") || strings.Contains(line, "grep") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 11 {
			continue
		}
		pid, err := strconv.Atoi(fields[1])
		if err != nil || pid == os.Getpid() {
			continue
		}

		// Parse agent dir from command args: ... lingtai run <dir>
		var agentDir string
		for i, f := range fields {
			if f == "run" && i+1 < len(fields) {
				agentDir = fields[i+1]
				break
			}
		}

		// If filtering by dir, only include processes under <dir>/.lingtai/
		if opts.FilterDir != "" {
			lingtaiPrefix := filepath.Join(opts.FilterDir, ".lingtai") + string(filepath.Separator)
			if !strings.HasPrefix(agentDir, lingtaiPrefix) {
				continue
			}
		}

		agent := filepath.Base(agentDir)
		project := ""
		// Walk up to find .lingtai parent
		if idx := strings.Index(agentDir, "/.lingtai/"); idx >= 0 {
			project = agentDir[:idx]
		}

		// Get process start time from ps output. This is replaced with elapsed time below when available.
		elapsed := fields[9]

		procs = append(procs, listProc{PID: fields[1], Uptime: elapsed, Agent: agent, Project: project, Dir: agentDir})
	}

	if len(procs) == 0 {
		if opts.FilterDir != "" {
			fmt.Printf("No lingtai processes running in %s.\n", opts.FilterDir)
		} else {
			fmt.Println("No lingtai processes running.")
		}
		return
	}

	// Also try to get elapsed time via ps -o etimes.
	pidStrs := make([]string, len(procs))
	procByPID := map[string]*listProc{}
	for i := range procs {
		pidStrs[i] = procs[i].PID
		procByPID[procs[i].PID] = &procs[i]
	}
	if out2, err := exec.Command("ps", "-o", "pid=,etimes=", "-p", strings.Join(pidStrs, ",")).Output(); err == nil {
		for _, line := range strings.Split(string(out2), "\n") {
			fields := strings.Fields(line)
			if len(fields) != 2 {
				continue
			}
			secs, err := strconv.Atoi(fields[1])
			if err != nil {
				continue
			}
			d := time.Duration(secs) * time.Second
			elapsed := ""
			if d >= 24*time.Hour {
				elapsed = fmt.Sprintf("%dd %dh", int(d.Hours())/24, int(d.Hours())%24)
			} else if d >= time.Hour {
				elapsed = fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
			} else {
				elapsed = fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
			}
			if proc := procByPID[fields[0]]; proc != nil {
				proc.Uptime = elapsed
			}
		}
	}

	phantomDirs := detectPhantomDirs(procs, opts.FilterDir)
	annotateListProcs(procs)
	printList(os.Stdout, procs, phantomDirs, opts, true)
	fmt.Printf("\n%d process(es) running.\n", len(procs))
	printListWarnings(os.Stdout, phantomDirs, opts.FilterDir)
}

func detectPhantomDirs(procs []listProc, filterDir string) map[string]bool {
	phantomDirs := map[string]bool{}
	if filterDir != "" {
		lingtaiDir := filepath.Join(filterDir, ".lingtai")
		if _, err := os.Stat(lingtaiDir); os.IsNotExist(err) {
			phantomDirs[filterDir] = true
		}
		return phantomDirs
	}

	seen := map[string]bool{}
	for _, p := range procs {
		if p.Project == "" || seen[p.Project] {
			continue
		}
		seen[p.Project] = true
		lingtaiDir := filepath.Join(p.Project, ".lingtai")
		if _, err := os.Stat(lingtaiDir); os.IsNotExist(err) {
			phantomDirs[p.Project] = true
		}
	}
	return phantomDirs
}
