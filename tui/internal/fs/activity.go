package fs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	NetworkStatusActive       = "active"
	NetworkStatusDaemonActive = "daemon-active"
	NetworkStatusIdle         = "idle"
	NetworkStatusAsleep       = "asleep"
	NetworkStatusSuspend      = "suspend"
)

// NetworkActivity is the project-level activity summary for non-human agents.
type NetworkActivity struct {
	Status         string `json:"status"`
	ActiveAgents   int    `json:"active_agents"`
	RunningDaemons int    `json:"running_daemons"`
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

	for _, node := range nodes {
		if node.IsHuman {
			continue
		}

		state := strings.ToUpper(node.State)
		if state == "ACTIVE" {
			activity.ActiveAgents++
		}
		if node.Alive {
			activity.RunningDaemons += countRunningDaemons(node.WorkingDir)
		}

		switch state {
		case "IDLE":
			hasIdle = true
		case "STUCK":
			// STUCK stays an individual agent state. At network level we fold a
			// heartbeat-fresh STUCK agent into idle so a live but errored agent
			// does not make the project look asleep or suspended.
			if node.Alive {
				hasIdle = true
			}
		case "ASLEEP":
			hasAsleep = true
		}
	}

	switch {
	case activity.ActiveAgents > 0:
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

type daemonStateFile struct {
	State      string          `json:"state"`
	FinishedAt json.RawMessage `json:"finished_at"`
}

func countRunningDaemons(agentDir string) int {
	daemonDir := filepath.Join(agentDir, "daemons")
	entries, err := os.ReadDir(daemonDir)
	if err != nil {
		return 0
	}

	var count int
	for _, entry := range entries {
		var path string
		if entry.IsDir() {
			path = filepath.Join(daemonDir, entry.Name(), "daemon.json")
		} else if entry.Name() == "daemon.json" {
			path = filepath.Join(daemonDir, entry.Name())
		} else {
			continue
		}
		if isRunningDaemonFile(path) {
			count++
		}
	}
	return count
}

func isRunningDaemonFile(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	var state daemonStateFile
	if err := json.Unmarshal(data, &state); err != nil {
		return false
	}

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
