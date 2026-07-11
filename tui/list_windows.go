//go:build windows

package main

import (
	"fmt"
	"os"

	"github.com/anthropics/lingtai-tui/internal/inventory"
	"github.com/anthropics/lingtai-tui/internal/processscan"
)

func listMain() {
	opts, err := parseListArgs(os.Args[2:])
	if err != nil {
		listUsageError(err)
	}

	found, err := processscan.FindAllAgentProcesses()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error listing processes: %v\n", err)
		os.Exit(1)
	}
	snap := inventory.FromProcesses(found, inventory.Options{FilterDir: opts.FilterDir, SelfPID: os.Getpid(), IncludeHuman: true})

	if len(snap.Records) == 0 {
		snap.PhantomDirs = nil
		if opts.JSON {
			printListJSON(os.Stdout, snap, opts)
			return
		}
		if opts.FilterDir != "" {
			fmt.Printf("No lingtai processes running in %s.\n", opts.FilterDir)
		} else {
			fmt.Println("No lingtai processes running.")
		}
		return
	}

	if opts.JSON {
		printListJSON(os.Stdout, snap, opts)
		return
	}
	printList(os.Stdout, snap.Records, opts, false)
	fmt.Printf("\n%d process(es) running.\n", len(snap.Records))
	printListWarnings(os.Stdout, snap.PhantomDirs, opts.FilterDir)
}

func agentDirFromWindowsCommandLine(cmdline string) string {
	agentDir, ok := processscan.ExtractAgentDir(cmdline)
	if !ok {
		return ""
	}
	return agentDir
}
