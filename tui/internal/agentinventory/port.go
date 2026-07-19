// Package agentinventory answers one advisory query: which agent directories
// exist under a project's .lingtai root and what their own on-disk records,
// checked against one native PID existence probe, say about each. Records
// never authorize lifecycle or destructive work. See CONTRACT.md.
package agentinventory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Presence is the advisory state of one agent directory.
type Presence string

const (
	PresenceRunning  Presence = "running"
	PresenceAbsent   Presence = "absent"
	PresenceUnknown  Presence = "unknown"
	PresenceConflict Presence = "conflict"
)

// Record is one advisory agent record. PID is the recorded PID (0 if none).
type Record struct {
	AgentDir string
	Name     string
	Address  string
	PID      int
	Presence Presence
	Detail   string
}

// QueryError is the typed whole-query failure. Callers must treat it as
// "inventory unavailable", never as an empty inventory.
type QueryError struct {
	Root string
	Err  error
}

func (e *QueryError) Error() string {
	return fmt.Sprintf("agent inventory query on %s failed: %v", e.Root, e.Err)
}
func (e *QueryError) Unwrap() error { return e.Err }

// probeState is the result of one native PID existence probe. The probe never
// signals, stops, or waits; it only asks whether the PID exists right now.
type probeState int

const (
	probeAlive probeState = iota
	probeAbsent
	probeUnknown
)

// Query scans root (one project's .lingtai directory) and returns one Record
// per subdirectory containing an .agent.json manifest, sorted by name then
// directory. Subdirectories without a manifest are not agent directories and
// are skipped.
func Query(root string) ([]Record, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, &QueryError{Root: root, Err: err}
	}
	records := make([]Record, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if r, ok := collect(filepath.Join(root, entry.Name()), entry.Name()); ok {
			records = append(records, r)
		}
	}
	sort.Slice(records, func(i, j int) bool {
		a, b := records[i], records[j]
		return a.Name < b.Name || (a.Name == b.Name && a.AgentDir < b.AgentDir)
	})
	return records, nil
}

func collect(dir, fallbackName string) (Record, bool) {
	r := Record{AgentDir: dir, Name: fallbackName, Presence: PresenceUnknown}
	data, err := os.ReadFile(filepath.Join(dir, ".agent.json"))
	if os.IsNotExist(err) {
		return Record{}, false
	}
	if err != nil {
		r.Detail = "manifest unreadable: " + err.Error()
		return r, true
	}
	var manifest struct {
		AgentName string           `json:"agent_name"`
		Address   string           `json:"address"`
		Admin     *json.RawMessage `json:"admin"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		r.Detail = "manifest invalid: " + err.Error()
		return r, true
	}
	// A project always contains a human mailbox placeholder whose manifest has
	// admin: null. It is not an agent directory and must not become a standing
	// unknown advisory record.
	if manifest.Admin == nil {
		return Record{}, false
	}
	if manifest.AgentName != "" {
		r.Name = manifest.AgentName
	}
	r.Address = manifest.Address

	pid, recordedRunning, hasPID := recordedRuntime(dir)
	if !hasPID {
		r.Detail = "no recorded pid"
		return r, true
	}
	r.PID = pid
	state, detail := probePID(pid)
	switch {
	case state == probeUnknown:
		r.Detail = "pid probe undecided: " + detail
	case state == probeAlive && recordedRunning:
		r.Presence = PresenceRunning
	case state == probeAlive:
		r.Presence = PresenceConflict
		r.Detail = fmt.Sprintf("recorded stopped but pid %d exists", pid)
	case recordedRunning:
		r.Presence = PresenceConflict
		r.Detail = fmt.Sprintf("recorded running but pid %d is gone", pid)
	default:
		r.Presence = PresenceAbsent
	}
	return r, true
}

// recordedRuntime reads the agent's own .status.json runtime claim.
func recordedRuntime(dir string) (pid int, running bool, ok bool) {
	data, err := os.ReadFile(filepath.Join(dir, ".status.json"))
	if err != nil {
		return 0, false, false
	}
	var status struct {
		Runtime struct {
			PID     int  `json:"pid"`
			Running bool `json:"running"`
		} `json:"runtime"`
	}
	if json.Unmarshal(data, &status) != nil || status.Runtime.PID <= 0 {
		return 0, false, false
	}
	return status.Runtime.PID, status.Runtime.Running, true
}
