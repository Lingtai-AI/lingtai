package process

import "github.com/anthropics/lingtai-tui/internal/processscan"

// AgentProcess is a single `lingtai run <agentDir>` process discovered by
// scanning `ps`. The command field holds the trimmed ps line; pid is parsed
// from the leading column. Used by FindAgentProcesses /
// TerminateAgentProcesses so callers can both detect and act on lingering
// interpreters.
type AgentProcess = processscan.AgentProcess

func parsePSOutput(out, abs string) []AgentProcess {
	return processscan.ParsePSOutput(out, abs)
}

// FindAgentProcesses returns all running `lingtai run <agentDir>` processes
// visible to the current user via `ps -eo pid=,command=`. Empty slice on
// error or no match. Use IsAgentRunning if you only need a boolean.
func FindAgentProcesses(agentDir string) []AgentProcess {
	return processscan.FindAgentProcesses(agentDir)
}

// IsAgentRunning returns true if any `python -m lingtai run <agentDir>`
// (or `lingtai-agent run <agentDir>`) process exists on this machine.
// Independent of `.agent.heartbeat`: even when the heartbeat file is missing or
// stale, the lingering Python interpreter is still visible in `ps`.
//
// Used by LaunchAgent as a hard gate. Callers that want fast-path liveness from
// the heartbeat freshness should use fs.IsAlive instead — this scan shells out to
// ps and is meant for the launch boundary, not hot paths.
func IsAgentRunning(agentDir string) bool {
	return processscan.IsAgentRunning(agentDir)
}
