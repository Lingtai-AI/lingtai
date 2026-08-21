package fs

import (
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// AgentAliveThresholdEnvVar overrides the heartbeat-liveness window resolved
// by AgentAliveThresholdSec.
const AgentAliveThresholdEnvVar = "LINGTAI_AGENT_ALIVE_THRESHOLD_SEC"

// DefaultAgentAliveThresholdSec is the fallback heartbeat-liveness window
// when AgentAliveThresholdEnvVar is unset or invalid. It is shared with the
// TUI and kernel heartbeat-liveness contract.
const DefaultAgentAliveThresholdSec = 10.0

// AgentAliveThresholdSec resolves the heartbeat-liveness window from
// AgentAliveThresholdEnvVar, falling back to DefaultAgentAliveThresholdSec
// when the variable is missing, blank, malformed, non-finite, zero, or
// negative. Callers apply the result with a strict age < threshold
// comparison (see IsAlive below). Liveness observation never rewrites the
// manifest lifecycle state.
func AgentAliveThresholdSec() float64 {
	raw := strings.TrimSpace(os.Getenv(AgentAliveThresholdEnvVar))
	if raw == "" {
		return DefaultAgentAliveThresholdSec
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
		return DefaultAgentAliveThresholdSec
	}
	return v
}

func IsAlive(dir string, thresholdSec float64) bool {
	data, err := os.ReadFile(filepath.Join(dir, ".agent.heartbeat"))
	if err != nil {
		return false
	}
	ts, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
	if err != nil {
		return false
	}
	age := time.Since(time.Unix(int64(ts), 0)).Seconds()
	return age < thresholdSec
}

func IsAliveHuman() bool {
	return true
}
