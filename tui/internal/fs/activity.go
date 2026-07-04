package fs

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	NetworkStatusActive       = "active"
	NetworkStatusDaemonActive = "daemon-active"
	NetworkStatusIdle         = "idle"
	NetworkStatusAsleep       = "asleep"
	NetworkStatusSuspend      = "suspend"
)

const (
	networkActiveTurnCap        = 600 * time.Second
	networkRecentProgressWindow = 90 * time.Second
)

// NetworkActivity is the project-level activity summary for non-human agents.
type NetworkActivity struct {
	Status         string `json:"status"`
	ActiveAgents   int    `json:"active_agents"`
	RunningDaemons int    `json:"running_daemons"`
}

// DaemonCounts summarizes daemon run files under a single agent directory.
type DaemonCounts struct {
	Running int `json:"running"`
	Total   int `json:"total"`
}

// ComputeNetworkActivity returns a lightweight activity summary without reading
// mailboxes, ledgers, contacts, or token logs.
func ComputeNetworkActivity(baseDir string) (NetworkActivity, error) {
	nodes, err := DiscoverAgents(baseDir)
	if err != nil {
		return NetworkActivity{}, fmt.Errorf("discover agents: %w", err)
	}
	normalizeAgentLiveness(nodes)
	return computeNetworkActivity(nodes), nil
}

func normalizeAgentLiveness(nodes []AgentNode) {
	for i := range nodes {
		if nodes[i].IsHuman {
			nodes[i].Alive = true
			continue
		}
		nodes[i].Alive = IsAlive(nodes[i].WorkingDir, AgentAliveThresholdSec)
		if !nodes[i].Alive && nodes[i].State != "" {
			nodes[i].State = "SUSPENDED"
		}
	}
}

func computeNetworkActivity(nodes []AgentNode) NetworkActivity {
	activity := NetworkActivity{Status: NetworkStatusSuspend}
	var hasIdle bool
	var hasAsleep bool
	var hasNonDaemonActive bool

	for _, node := range nodes {
		if node.IsHuman {
			continue
		}

		state := strings.ToUpper(node.State)
		live := node.Alive
		if state == "ACTIVE" {
			activity.ActiveAgents++
			hasNonDaemonActive = true
		}

		if live {
			activity.RunningDaemons += CountDaemons(node.WorkingDir).Running
			if state != "ACTIVE" && hasStatusActivity(node.WorkingDir, time.Now()) {
				activity.ActiveAgents++
				hasNonDaemonActive = true
			}
		}

		switch state {
		case "IDLE":
			hasIdle = true
		case "STUCK":
			// STUCK stays an individual agent state. At network level we fold a
			// heartbeat-fresh STUCK agent into idle so a live but errored agent
			// does not make the project look asleep or suspended.
			if live {
				hasIdle = true
			}
		case "ASLEEP":
			hasAsleep = true
		}
	}

	switch {
	case hasNonDaemonActive:
		activity.Status = NetworkStatusActive
	case activity.RunningDaemons > 0:
		activity.Status = NetworkStatusDaemonActive
	case hasIdle:
		activity.Status = NetworkStatusIdle
	case hasAsleep:
		activity.Status = NetworkStatusAsleep
	default:
		activity.Status = NetworkStatusSuspend
	}
	return activity
}

type networkStatusSnapshot struct {
	ActiveTurn *networkActiveTurn `json:"active_turn"`
	Runtime    struct {
		State          string             `json:"state"`
		LastProgressAt tolerantJSONNumber `json:"last_progress_at"`
		NoProgressSecs tolerantJSONNumber `json:"no_progress_seconds"`
		UptimeSeconds  tolerantJSONNumber `json:"uptime_seconds"`
		StaminaLeft    tolerantJSONNumber `json:"stamina_left"`
	} `json:"runtime"`
}

type networkActiveTurn struct {
	Kind           string             `json:"kind"`
	ID             string             `json:"id"`
	StartedAt      tolerantJSONNumber `json:"started_at"`
	ElapsedSeconds tolerantJSONNumber `json:"elapsed_seconds"`
}

type tolerantJSONNumber struct {
	Value float64
	OK    bool
}

func (n *tolerantJSONNumber) UnmarshalJSON(data []byte) error {
	text := strings.TrimSpace(string(data))
	if text == "" || text == "null" {
		return nil
	}
	if len(text) >= 2 && text[0] == '"' && text[len(text)-1] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return nil
		}
		s = strings.TrimSpace(s)
		if s == "" {
			return nil
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return nil
		}
		n.Value = v
		n.OK = true
		return nil
	}
	v, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	n.Value = v
	n.OK = true
	return nil
}

func hasStatusActivity(agentDir string, now time.Time) bool {
	status, mtime, ok := readNetworkStatusSnapshot(agentDir)
	if !ok {
		return false
	}
	if status.ActiveTurn != nil {
		freshAt := mtime
		if status.ActiveTurn.StartedAt.OK {
			freshAt = laterTime(freshAt, unixSeconds(status.ActiveTurn.StartedAt.Value))
		}
		if status.Runtime.LastProgressAt.OK {
			freshAt = laterTime(freshAt, unixSeconds(status.Runtime.LastProgressAt.Value))
		}
		return within(now, freshAt, networkActiveTurnCap)
	}
	if status.Runtime.LastProgressAt.OK {
		return within(now, unixSeconds(status.Runtime.LastProgressAt.Value), networkRecentProgressWindow)
	}
	return false
}

func readNetworkStatusSnapshot(agentDir string) (networkStatusSnapshot, time.Time, bool) {
	path := filepath.Join(agentDir, ".status.json")
	info, err := os.Stat(path)
	if err != nil {
		return networkStatusSnapshot{}, time.Time{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return networkStatusSnapshot{}, time.Time{}, false
	}
	var status networkStatusSnapshot
	if err := json.Unmarshal(data, &status); err != nil {
		return networkStatusSnapshot{}, time.Time{}, false
	}
	return status, info.ModTime(), true
}

func laterTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func unixSeconds(v float64) time.Time {
	sec, frac := math.Modf(v)
	return time.Unix(int64(sec), int64(frac*1e9))
}

func within(now, t time.Time, window time.Duration) bool {
	if t.IsZero() {
		return false
	}
	age := now.Sub(t)
	return age >= 0 && age <= window
}

type daemonStateFile struct {
	State      string          `json:"state"`
	FinishedAt json.RawMessage `json:"finished_at"`
}

// CountDaemons counts parseable daemon.json files under agentDir/daemons.
func CountDaemons(agentDir string) DaemonCounts {
	daemonDir := filepath.Join(agentDir, "daemons")
	entries, err := os.ReadDir(daemonDir)
	if err != nil {
		return DaemonCounts{}
	}

	var counts DaemonCounts
	for _, entry := range entries {
		var path string
		if entry.IsDir() {
			path = filepath.Join(daemonDir, entry.Name(), "daemon.json")
		} else if entry.Name() == "daemon.json" {
			path = filepath.Join(daemonDir, entry.Name())
		} else {
			continue
		}
		state, ok := readDaemonStateFile(path)
		if !ok {
			continue
		}
		counts.Total++
		if isRunningDaemonState(state) {
			counts.Running++
		}
	}
	return counts
}

func readDaemonStateFile(path string) (daemonStateFile, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return daemonStateFile{}, false
	}

	var state daemonStateFile
	if err := json.Unmarshal(data, &state); err != nil {
		return daemonStateFile{}, false
	}
	return state, true
}

func isRunningDaemonState(state daemonStateFile) bool {
	switch strings.ToLower(strings.TrimSpace(state.State)) {
	case "running", "active":
	default:
		return false
	}
	return !hasFinishedAt(state.FinishedAt)
}

func hasFinishedAt(raw json.RawMessage) bool {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return false
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s) != ""
	}
	return true
}
