package fs

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// AgentAliveThresholdSec is the heartbeat-liveness window shared with the
// kernel. The kernel fixes the heartbeat tick at one second and liveness at
// five ticks (src/lingtai/kernel/config.py:132-133), and applies a strict
// age < threshold comparison (src/lingtai/kernel/agent_presence/__init__.py).
// Go consumers must use the same window and the same strict boundary so a
// live agent is never reported dead or suspended (issue #845).
const AgentAliveThresholdSec = 5.0

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
