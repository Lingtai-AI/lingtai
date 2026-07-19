//go:build !windows

package agentcounter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// heartbeatFreshSeconds is AgentCounter's own liveness rule: an agent
// counts as running only while its heartbeat is younger than this.
const heartbeatFreshSeconds = 2.0

// CountRunningAgents counts semantic agents with POSIX file evidence only.
func CountRunningAgents(globalDir string) Count {
	data, err := os.ReadFile(filepath.Join(globalDir, "registry.jsonl"))
	if err != nil {
		if globalDir != "" && os.IsNotExist(err) {
			return Count{State: StateKnown}
		}
		return Count{State: StateUnknown, Issues: []string{"registry: " + err.Error()}}
	}
	out := Count{State: StateKnown}
	for _, line := range strings.Split(string(data), "\n") {
		var row struct {
			Path string `json:"path"`
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		if json.Unmarshal([]byte(line), &row) != nil || row.Path == "" {
			out.Issues = append(out.Issues, "registry: malformed row skipped")
			continue
		}
		countProject(row.Path, &out)
	}
	return out
}

func countProject(projectDir string, out *Count) {
	base := filepath.Join(projectDir, ".lingtai")
	entries, err := os.ReadDir(base)
	if err != nil {
		if !os.IsNotExist(err) {
			out.Issues = append(out.Issues, base+": "+err.Error())
		}
		return // a stale registry row is a real zero, not a failure
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(base, e.Name())
		manifest, err := os.ReadFile(filepath.Join(dir, ".agent.json"))
		if err != nil {
			if !os.IsNotExist(err) {
				out.Issues = append(out.Issues, dir+": "+err.Error())
			}
			continue // no manifest: not an agent dir
		}
		var m struct {
			Admin *json.RawMessage `json:"admin"`
		}
		if json.Unmarshal(manifest, &m) != nil {
			out.Issues = append(out.Issues, dir+": malformed manifest")
			continue
		}
		if m.Admin == nil || string(*m.Admin) == "null" {
			continue // human mailbox row, not a semantic agent
		}
		hb, err := os.ReadFile(filepath.Join(dir, ".agent.heartbeat"))
		if err != nil {
			continue // no heartbeat: registered but not running
		}
		if ts, perr := strconv.ParseFloat(strings.TrimSpace(string(hb)), 64); perr != nil {
			out.Issues = append(out.Issues, dir+": malformed heartbeat")
		} else if time.Since(time.Unix(int64(ts), 0)).Seconds() < heartbeatFreshSeconds {
			out.Agents++
		}
	}
}
