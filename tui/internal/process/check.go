package process

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// IsAgentRunning returns true if any `python -m lingtai run <agentDir>`
// process exists on this machine. Independent of `.agent.heartbeat`: even
// when the heartbeat file is missing or stale (a previous agent mid-teardown,
// a crashed process, an old kernel version that unlinked the file early),
// the lingering Python interpreter is still visible in `ps` and we can
// refuse to spawn a duplicate.
//
// Used by LaunchAgent as a hard gate. Callers that want fast-path liveness
// from the heartbeat freshness should use fs.IsAlive instead — this scan
// shells out to ps and is meant for the launch boundary, not hot paths.
func IsAgentRunning(agentDir string) bool {
	abs, err := filepath.Abs(agentDir)
	if err != nil {
		abs = agentDir
	}
	out, err := exec.Command("ps", "-eo", "pid=,command=").Output()
	if err != nil {
		return false
	}
	needle := "lingtai run " + abs
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "lingtai run") {
			continue
		}
		// Match either `... lingtai run <abs> ...` or `... lingtai run <abs>` (EOL).
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, needle+" ") || strings.HasSuffix(trimmed, needle) {
			return true
		}
	}
	return false
}
