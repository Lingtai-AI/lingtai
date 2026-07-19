// Package actionstate is the AgentActionState policy: it computes explicit
// start/restart/refresh enablement for one agent directory from its own
// read-only heartbeat observation. See CONTRACT.md.
package actionstate

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Observation is the tri-state result of one heartbeat observation.
type Observation int

const (
	ObservationUnknown Observation = iota
	ObservationRunning
	ObservationStopped
)

// Decision is the computed enablement for one agent directory. Unknown
// observation leaves every enablement false.
type Decision struct {
	Observation Observation
	CanStart    bool
	CanRestart  bool
	CanRefresh  bool
	Reason      string
}

// Evaluate observes dir's heartbeat once and maps the observation to
// action enablement. It never launches, stops, or writes anything.
func Evaluate(dir string, thresholdSec float64) Decision {
	obs, reason := observe(dir, thresholdSec)
	d := Decision{Observation: obs, Reason: reason}
	switch obs {
	case ObservationRunning:
		d.CanRefresh = true
	case ObservationStopped:
		d.CanStart = true
		d.CanRestart = true
	}
	return d
}

// observe reads dir's heartbeat file. Only proven absence or a valid stale
// timestamp is Stopped; any other failure is Unknown, never Stopped.
func observe(dir string, thresholdSec float64) (Observation, string) {
	data, err := os.ReadFile(filepath.Join(dir, ".agent.heartbeat"))
	if err != nil {
		if heartbeatAbsent(err) {
			return ObservationStopped, "no heartbeat file"
		}
		return ObservationUnknown, "heartbeat unreadable: " + err.Error()
	}
	ts, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
	if err != nil {
		return ObservationUnknown, "heartbeat malformed: " + err.Error()
	}
	sec := int64(ts)
	age := time.Since(time.Unix(sec, int64((ts-float64(sec))*1e9))).Seconds()
	switch {
	case age < 0:
		return ObservationUnknown, fmt.Sprintf("heartbeat timestamp %.1fs in the future", -age)
	case age < thresholdSec:
		return ObservationRunning, fmt.Sprintf("heartbeat fresh (%.1fs old)", age)
	default:
		return ObservationStopped, fmt.Sprintf("heartbeat stale (%.1fs old)", age)
	}
}
