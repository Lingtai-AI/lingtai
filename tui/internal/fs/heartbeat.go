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
// when AgentAliveThresholdEnvVar is unset or invalid. This window is a
// TUI-side presentation/operational setting, intentionally independent of
// the kernel's own heartbeat liveness config (src/lingtai/kernel/config.py).
const DefaultAgentAliveThresholdSec = 5.0

// AgentAliveThresholdSec resolves the heartbeat-liveness window from
// AgentAliveThresholdEnvVar, falling back to DefaultAgentAliveThresholdSec
// when the variable is missing, blank, malformed, non-finite, zero, or
// negative. Callers apply the result with a strict age < threshold
// comparison (see ReadHeartbeat/IsAlive below) so a live agent is never
// reported dead or suspended.
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

type HeartbeatStatus struct {
	Exists     bool    `json:"exists"`
	Fresh      bool    `json:"fresh"`
	Timestamp  float64 `json:"timestamp,omitempty"`
	AgeSeconds float64 `json:"age_seconds,omitempty"`
	Error      string  `json:"error,omitempty"`
}

func ReadHeartbeat(dir string, thresholdSec float64) HeartbeatStatus {
	data, err := os.ReadFile(filepath.Join(dir, ".agent.heartbeat"))
	if err != nil {
		return HeartbeatStatus{Exists: false, Fresh: false, Error: err.Error()}
	}
	ts, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
	if err != nil {
		return HeartbeatStatus{Exists: true, Fresh: false, Error: err.Error()}
	}
	sec := int64(ts)
	nsec := int64((ts - float64(sec)) * 1e9)
	age := time.Since(time.Unix(sec, nsec)).Seconds()
	return HeartbeatStatus{
		Exists:     true,
		Fresh:      age < thresholdSec,
		Timestamp:  ts,
		AgeSeconds: age,
	}
}

func IsAlive(dir string, thresholdSec float64) bool {
	return ReadHeartbeat(dir, thresholdSec).Fresh
}

func IsAliveHuman() bool {
	return true
}
