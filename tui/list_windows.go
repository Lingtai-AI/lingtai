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
	opts, err := parseListArgs(os.Args[2:])
	if err != nil {
		listUsageError(err)
	}

	out, err := exec.Command("wmic", "process", "where",
		"commandline like '%lingtai run%'",
		"get", "processid,commandline", "/format:list").Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error listing processes: %v\n", err)
		os.Exit(1)
	}

	var procs []listProc
	var cmdline, pid string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "CommandLine=") {
			cmdline = strings.TrimPrefix(line, "CommandLine=")
		}
		if strings.HasPrefix(line, "ProcessId=") {
			pid = strings.TrimPrefix(line, "ProcessId=")
			if cmdline != "" && strings.Contains(cmdline, "lingtai run") {
				agentDir := ""
				agent := "unknown"
				if idx := strings.Index(cmdline, "lingtai run "); idx >= 0 {
					agentDir = cmdline[idx+len("lingtai run "):]
					agentDir = strings.Trim(strings.TrimSpace(strings.Split(agentDir, " ")[0]), "\"")
					agent = filepath.Base(agentDir)
				}

				// Filter by dir if specified.
				if opts.FilterDir != "" {
					lingtaiPrefix := filepath.Join(opts.FilterDir, ".lingtai") + string(filepath.Separator)
					if !strings.HasPrefix(agentDir, lingtaiPrefix) {
						cmdline = ""
						pid = ""
						continue
					}
				}

				project := ""
				if idx := strings.Index(agentDir, `\.lingtai\`); idx >= 0 {
					project = agentDir[:idx]
				}

				procs = append(procs, listProc{PID: pid, Agent: agent, Dir: agentDir, Project: project})
			}
			cmdline = ""
			pid = ""
		}
	}

	if len(procs) == 0 {
		if opts.FilterDir != "" {
			fmt.Printf("No lingtai processes running in %s.\n", opts.FilterDir)
		} else {
			fmt.Println("No lingtai processes running.")
		}
		return
	}

	phantomDirs := detectPhantomDirs(procs, opts.FilterDir)
	annotateListProcs(procs)
	printList(os.Stdout, procs, phantomDirs, opts, false)
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
