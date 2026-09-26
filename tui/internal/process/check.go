package process

import "github.com/anthropics/lingtai-tui/internal/processscan"

// AgentProcess is a LingTai agent process discovered by
// scanning the process table. Puffo ACP rows are visible for inventory and
// duplicate prevention, but only TUI-owned run rows may be terminated here.
type AgentProcess = processscan.AgentProcess

func parsePSOutput(out, abs string) []AgentProcess {
	return processscan.ParsePSOutput(out, abs)
}

// FindAgentProcesses returns LingTai agent processes visible to the current
// user via `ps -eo pid=,command=`. Empty slice on
// error or no match. Use IsAgentRunning if you only need a boolean.
func FindAgentProcesses(agentDir string) []AgentProcess {
	return processscan.FindAgentProcesses(agentDir)
}

func tuiOwnedRunProcesses(agentDir string) ([]AgentProcess, error) {
	procs := FindAgentProcesses(agentDir)
	for _, proc := range procs {
		if proc.PuffoACP {
			return nil, ErrPuffoManagedAgent
		}
	}
	return procs, nil
}

// IsAgentRunning returns true if any supported `lingtai run` or Puffo ACP
// process exists for the workdir on this machine.
// Independent of `.agent.heartbeat`: even when the heartbeat file is missing or
// stale, the lingering Python interpreter is still visible in `ps`.
//
// Used by LaunchAgent as a hard gate. Callers that want fast-path liveness from
// the heartbeat freshness should use fs.IsAlive instead — this scan shells out to
// ps and is meant for the launch boundary, not hot paths.
func IsAgentRunning(agentDir string) bool {
	return processscan.IsAgentRunning(agentDir)
}
