package fs

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const AgentAliveThresholdSec = 2.0

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
