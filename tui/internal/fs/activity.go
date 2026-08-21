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
	networkFutureTimestampGrace = 2 * time.Minute
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
	Queued  int `json:"queued"`
	Done    int `json:"done"`
	Failed  int `json:"failed"`
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
		// Heartbeat freshness is liveness evidence, not a lifecycle state.
		// A stale/missing heartbeat surfaces through Alive=false; it must
		// not fabricate a manifest state transition into SUSPENDED (issue
		// false-suspended, 2026-08-18). A manifest that is genuinely
		// SUSPENDED stays SUSPENDED because this leaves State untouched.
		nodes[i].Alive = IsAlive(nodes[i].WorkingDir, AgentAliveThresholdSec())
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
		if state == "ACTIVE" && live {
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

		// Every branch below requires live: state alone is manifest lifecycle,
		// not liveness, and a stale/missing heartbeat no longer rewrites State
		// to SUSPENDED (see normalizeAgentLiveness), so this aggregate must
		// gate on Alive itself to keep a heartbeat-dead agent out of the
		// active/idle/asleep buckets.
		switch state {
		case "IDLE":
			if live {
				hasIdle = true
			}
		case "STUCK":
			// STUCK stays an individual agent state. At network level we fold a
			// heartbeat-fresh STUCK agent into idle so a live but errored agent
			// does not make the project look asleep or suspended.
			if live {
				hasIdle = true
			}
		case "ASLEEP":
			if live {
				hasAsleep = true
			}
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
			LastApiCallAt  tolerantJSONNumber `json:"last_api_call_at"`
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
		candidates := []time.Time{mtime}
		if status.ActiveTurn.StartedAt.OK {
			candidates = append(candidates, unixSeconds(status.ActiveTurn.StartedAt.Value))
		}
		if status.Runtime.LastProgressAt.OK {
			candidates = append(candidates, unixSeconds(status.Runtime.LastProgressAt.Value))
		}
		freshAt := latestStatusFreshnessTime(now, candidates...)
		return within(now, freshAt, networkActiveTurnCap)
	}
	if status.Runtime.LastProgressAt.OK {
		freshAt := latestStatusFreshnessTime(now, unixSeconds(status.Runtime.LastProgressAt.Value))
		return within(now, freshAt, networkRecentProgressWindow)
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

// LastProgressAt returns the agent's clamped runtime.last_progress_at from its
// .status.json, or zero + false when the file is missing, the field is absent/
// invalid, the runtime state is not active, or the timestamp is beyond the
// future grace window. Requiring state==active on the SAME snapshot mirrors the
// kernel taskcard footer semantics and avoids seeding the elapsed badge from a
// stale timestamp after a restart (Jason 2026-08-08, fable review).
func LastProgressAt(agentDir string, now time.Time) (time.Time, bool) {
	status, _, ok := readNetworkStatusSnapshot(agentDir)
	if !ok || !status.Runtime.LastProgressAt.OK {
		return time.Time{}, false
	}
	if !strings.EqualFold(status.Runtime.State, NetworkStatusActive) {
		return time.Time{}, false
	}
	return statusFreshnessTime(now, unixSeconds(status.Runtime.LastProgressAt.Value))
}

// LastApiCallAt returns the agent's clamped runtime.last_api_call_at from its
// .status.json, falling back to runtime.last_progress_at when the newer field
// is absent (older kernels). Zero + false when the file is missing, both
// fields are absent/invalid, the runtime state is not active, or the
// timestamp is beyond the future grace window. This is the "how long has it
// been active since its last API call" signal (Jason 2026-08-16): while the
// model thinks or a tool runs, last_api_call_at stays put and the age grows;
// when a new API call starts it resets.
func LastApiCallAt(agentDir string, now time.Time) (time.Time, bool) {
	status, _, ok := readNetworkStatusSnapshot(agentDir)
	if !ok {
		return time.Time{}, false
	}
	if !strings.EqualFold(status.Runtime.State, NetworkStatusActive) {
		return time.Time{}, false
	}
	if status.Runtime.LastApiCallAt.OK {
		return statusFreshnessTime(now, unixSeconds(status.Runtime.LastApiCallAt.Value))
	}
	if !status.Runtime.LastProgressAt.OK {
		return time.Time{}, false
	}
	return statusFreshnessTime(now, unixSeconds(status.Runtime.LastProgressAt.Value))
}

func latestStatusFreshnessTime(now time.Time, candidates ...time.Time) time.Time {
	var latest time.Time
	for _, candidate := range candidates {
		t, ok := statusFreshnessTime(now, candidate)
		if !ok {
			continue
		}
		if t.After(latest) {
			latest = t
		}
	}
	return latest
}

func statusFreshnessTime(now, t time.Time) (time.Time, bool) {
	if t.IsZero() {
		return time.Time{}, false
	}
	if t.After(now) {
		if t.Sub(now) > networkFutureTimestampGrace {
			return time.Time{}, false
		}
		return now, true
	}
	return t, true
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

// daemonListWindow bounds the daemon runs shown by the live kanban summary and
// the mail async-work row to runs whose run directory was modified within the
// last 10 minutes. Done/active/failed states are all included while recent.
// Older historical runs remain browsable through the /daemons view; the
// per-second dashboard never opens their state files, so a quiet week of
// history costs no IO on the 1s tick.
const daemonListWindow = 10 * time.Minute

// withinDaemonListWindow reports whether a run's directory mtime is fresh
// enough for the live daemon summary to scan it. The boundary is inclusive: a
// run exactly daemonListWindow old is still shown. Future mtimes (clock skew)
// yield a negative age and are treated as fresh.
func withinDaemonListWindow(runModTime, now time.Time) bool {
	return now.Sub(runModTime) <= daemonListWindow
}

// CountDaemons counts parseable daemon.json files under agentDir/daemons whose
// run directory was modified within the last daemonListWindow. The recency gate
// runs on directory metadata already returned by ReadDir, so stale historical
// runs are skipped before any daemon.json read: the per-second kanban/mail
// summary only ever opens state files for recently-active runs.
func CountDaemons(agentDir string) DaemonCounts {
	daemonDir := filepath.Join(agentDir, "daemons")
	entries, err := os.ReadDir(daemonDir)
	if err != nil {
		return DaemonCounts{}
	}

	now := time.Now()
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
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if !withinDaemonListWindow(info.ModTime(), now) {
			continue
		}
		state, ok := readDaemonStateFile(path)
		if !ok {
			continue
		}
		counts.Total++
		switch stateBucket(state.State) {
		case "running":
			if isRunningDaemonState(state) {
				counts.Running++
			}
		case "queued":
			counts.Queued++
		case "done":
			counts.Done++
		case "failed":
			counts.Failed++
		}
	}
	return counts
}

// stateBucket maps a daemon.json state string to the home async-stats bucket.
// Terminal failure states (failed/cancelled/timeout) share the "failed" bucket;
// "running" and "active" are the live bucket; everything else (queued, pending,
// etc.) falls through to "queued" when not finished, and is ignored otherwise.
func stateBucket(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "running", "active":
		return "running"
	case "done":
		return "done"
	case "failed", "cancelled", "timeout", "canceled":
		return "failed"
	default:
		if state == "" {
			return ""
		}
		return "queued"
	}
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
