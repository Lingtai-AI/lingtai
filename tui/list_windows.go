//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/anthropics/lingtai-tui/internal/processscan"
)

func listMain() {
	opts, err := parseListArgs(os.Args[2:])
	if err != nil {
		listUsageError(err)
	}

	var procs []listProc
	for _, proc := range processscan.FindWindowsAgentProcesses("") {
		cmdline := proc.Command
		agentDir := agentDirFromWindowsCommandLine(cmdline)
		agent := "unknown"
		if agentDir != "" {
			agent = filepath.Base(agentDir)
		}

		// Filter by dir if specified.
		if opts.FilterDir != "" {
			lingtaiPrefix := filepath.Join(opts.FilterDir, ".lingtai") + string(filepath.Separator)
			if !strings.HasPrefix(agentDir, lingtaiPrefix) {
				continue
			}
		}

		project := ""
		if idx := strings.Index(agentDir, `\.lingtai\`); idx >= 0 {
			project = agentDir[:idx]
		}

		procs = append(procs, listProc{PID: fmt.Sprint(proc.PID), Agent: agent, Dir: agentDir, Project: project})
	}

	if len(procs) == 0 {
		if opts.JSON {
			printListJSON(os.Stdout, procs, nil, opts)
			return
		}
		if opts.FilterDir != "" {
			fmt.Printf("No lingtai processes running in %s.\n", opts.FilterDir)
		} else {
			fmt.Println("No lingtai processes running.")
		}
		return
	}

	phantomDirs := detectPhantomDirs(procs, opts.FilterDir)
	annotateListProcs(procs)
	procs = collapseListProcsByAgentDir(procs)
	if opts.JSON {
		printListJSON(os.Stdout, procs, phantomDirs, opts)
		return
	}
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

func agentDirFromWindowsCommandLine(cmdline string) string {
	lower := strings.ToLower(cmdline)
	idx := strings.Index(lower, "lingtai run ")
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(cmdline[idx+len("lingtai run "):])
	if rest == "" {
		return ""
	}
	if strings.HasPrefix(rest, `"`) {
		rest = strings.TrimPrefix(rest, `"`)
		end := strings.Index(rest, `"`)
		if end < 0 {
			return strings.TrimSpace(rest)
		}
		return strings.TrimSpace(rest[:end])
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return ""
	}
	return strings.Trim(fields[0], `"`)
}
