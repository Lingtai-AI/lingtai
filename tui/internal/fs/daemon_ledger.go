package fs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DaemonLedgerEntry is a single per-call token_ledger.jsonl line from a
// daemon run directory, tagged with the daemon's identity so the kanban
// detail view can show which daemon each call belongs to. The embedded
// LedgerEntry carries the token/model/endpoint fields; RunID/Handle/State
// come from the run's daemon.json (or the run directory name when the
// identity card is missing).
type DaemonLedgerEntry struct {
	LedgerEntry
	RunID  string // daemons/<run_id> directory name
	Handle string // daemon.json "handle" (e.g. "em-1"); empty when no card
	State  string // daemon.json "state" (running/done/failed/...); empty when no card
}

// daemonIdentity is the subset of daemon.json the recent-call lane needs.
type daemonIdentity struct {
	Handle string `json:"handle"`
	RunID  string `json:"run_id"`
	State  string `json:"state"`
}

// DaemonRecentLedger aggregates the per-call token ledgers of every daemon
// run directory under agentDir/daemons/<run_id>/logs/token_ledger.jsonl,
// returning the most-recent recentN calls across all daemons, newest first.
// Each returned entry retains the originating daemon's run id, handle, and
// status.
//
// A missing daemons/ directory, missing per-daemon ledgers, or malformed
// lines are treated as empty/partial data — callers render an empty state
// rather than seeing an error. This mirrors the best-effort contract of the
// other fs ledger readers (SumTokenLedgerByProvider).
func DaemonRecentLedger(agentDir string, recentN int) []DaemonLedgerEntry {
	daemonDir := filepath.Join(agentDir, "daemons")
	dirEntries, err := os.ReadDir(daemonDir)
	if err != nil {
		return nil
	}

	var all []DaemonLedgerEntry
	for _, de := range dirEntries {
		if !de.IsDir() {
			continue
		}
		runID := de.Name()
		runDir := filepath.Join(daemonDir, runID)

		id := readDaemonIdentity(filepath.Join(runDir, "daemon.json"))
		// run_id from the card is authoritative when present; otherwise the
		// directory name is the run id by construction.
		if id.RunID == "" {
			id.RunID = runID
		}

		ledgerPath := filepath.Join(runDir, "logs", "token_ledger.jsonl")
		for _, e := range readLedgerEntries(ledgerPath) {
			all = append(all, DaemonLedgerEntry{
				LedgerEntry: e,
				RunID:       id.RunID,
				Handle:      id.Handle,
				State:       id.State,
			})
		}
	}

	// Global newest-first sort by ts. Timestamps are ISO-8601 strings, which
	// sort lexicographically in chronological order. Entries without a ts
	// (older kernel ledgers) sort to the end (treated as oldest); ties keep a
	// stable order so the view is deterministic across refreshes.
	sort.SliceStable(all, func(i, j int) bool {
		return all[i].TS > all[j].TS
	})

	if recentN > 0 && len(all) > recentN {
		all = all[:recentN]
	}
	return all
}

// readDaemonIdentity reads the identity fields from a daemon.json. Missing or
// malformed files yield a zero identity — the caller fills run_id from the
// directory name.
func readDaemonIdentity(path string) daemonIdentity {
	var id daemonIdentity
	data, err := os.ReadFile(path)
	if err != nil {
		return id
	}
	json.Unmarshal(data, &id)
	return id
}

// readLedgerEntries parses every well-formed line of a token_ledger.jsonl
// file into LedgerEntry values (file order preserved). Missing file or
// malformed lines are skipped silently.
func readLedgerEntries(path string) []LedgerEntry {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []LedgerEntry
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e LedgerEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		out = append(out, e)
	}
	return out
}
