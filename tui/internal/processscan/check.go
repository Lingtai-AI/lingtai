package processscan

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// AgentProcess is a single `lingtai run <agentDir>` process discovered by
// scanning `ps`. The command field holds the trimmed ps line; pid is parsed
// from the leading column. Used by FindAgentProcesses so callers can both
// detect and act on lingering interpreters.
type AgentProcess struct {
	PID     int
	Command string
}

// ParsePSOutput extracts AgentProcess records from `ps -eo pid=,command=`
// output that match `lingtai run <abs>`. Split out from FindAgentProcesses so
// the parsing logic is unit-testable without shelling out to ps.
//
// The ps output format is: leading whitespace, PID, single space, command
// line (which itself may contain spaces). We split on the first whitespace
// run to separate pid from command.
func ParsePSOutput(out, abs string) []AgentProcess {
	needle := "lingtai run " + abs
	var results []AgentProcess
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "lingtai run") {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Match either `... lingtai run <abs> ...` (additional args follow)
		// or `... lingtai run <abs>` at end-of-line. Without this guard we
		// would also match `lingtai run <abs>-suffix`, picking up sibling
		// agent dirs that share a prefix.
		if !(strings.Contains(trimmed, needle+" ") || strings.HasSuffix(trimmed, needle)) {
			continue
		}
		// Split off the leading pid column. ps emits "  1234 python ..." so
		// Fields collapses leading whitespace; we take the first token.
		fields := strings.Fields(trimmed)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		results = append(results, AgentProcess{PID: pid, Command: trimmed})
	}
	return results
}

// FindAgentProcesses returns all running `lingtai run <agentDir>` processes
// visible to the current user via `ps -eo pid=,command=`. Empty slice on
// error or no match. Use IsAgentRunning if you only need a boolean.
func FindAgentProcesses(agentDir string) []AgentProcess {
	abs, err := filepath.Abs(agentDir)
	if err != nil {
		abs = agentDir
	}
	out, err := exec.Command("ps", "-eo", "pid=,command=").Output()
	if err != nil {
		return nil
	}
	return ParsePSOutput(string(out), abs)
}

// IsAgentRunning returns true if any `python -m lingtai run <agentDir>`
// (or `lingtai-agent run <agentDir>`) process exists on this machine.
func IsAgentRunning(agentDir string) bool {
	return len(FindAgentProcesses(agentDir)) > 0
}
