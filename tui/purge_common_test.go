package main

import (
	"path/filepath"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/processscan"
)

func TestPurgeProcsFromAgentProcessesPreservesSpacesAndFilter(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "Project With Spaces")
	agentDir := filepath.Join(project, ".lingtai", "agent A")
	otherAgentDir := filepath.Join(root, "Other Project", ".lingtai", "agent B")

	got := purgeProcsFromAgentProcesses([]processscan.AgentProcess{
		{PID: 111, AgentDir: agentDir},
		{PID: 222, AgentDir: otherAgentDir},
		{PID: 333, AgentDir: agentDir},
		{PID: 444, AgentDir: agentDir, PuffoACP: true},
	}, project, 333)

	if len(got) != 1 {
		t.Fatalf("len = %d, want 1: %+v", len(got), got)
	}
	if got[0].pid != 111 || got[0].agent != "agent A" || got[0].dir != agentDir {
		t.Fatalf("space-containing purge target was not preserved: %+v", got[0])
	}
}
