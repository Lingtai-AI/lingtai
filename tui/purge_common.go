package main

import (
	"path/filepath"

	"github.com/anthropics/lingtai-tui/internal/inventory"
	"github.com/anthropics/lingtai-tui/internal/processscan"
)

type purgeProc struct {
	pid   int
	agent string
	dir   string
}

func purgeProcsFromAgentProcesses(found []processscan.AgentProcess, filterDir string, selfPID int) []purgeProc {
	procs := make([]purgeProc, 0, len(found))
	for _, proc := range found {
		if proc.PID == selfPID {
			continue
		}
		agentDir := proc.AgentDir
		if agentDir == "" {
			var ok bool
			agentDir, ok = processscan.ExtractAgentDir(proc.Command)
			if !ok {
				continue
			}
		}
		if !inventory.AgentDirInFilter(agentDir, filterDir) {
			continue
		}
		procs = append(procs, purgeProc{
			pid:   proc.PID,
			agent: filepath.Base(agentDir),
			dir:   agentDir,
		})
	}
	return procs
}
