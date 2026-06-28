package sqlitelog

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// QueryMoltSessionWindows fetches the latest two psyche_molt timestamps from
// the sqlite sidecar. It returns the current session lower bound, the previous
// session lower bound, and the previous session upper bound.
func QueryMoltSessionWindows(agentDir string) (currentSince, lastSince, lastBefore time.Time, ok bool, err error) {
	db := DBPath(agentDir)
	if _, err := os.Stat(db); err != nil {
		return time.Time{}, time.Time{}, time.Time{}, false, fmt.Errorf("sqlite sidecar not found: %s", db)
	}
	bin, err := findSQLite3()
	if err != nil {
		return time.Time{}, time.Time{}, time.Time{}, false, err
	}

	const sql = `SELECT ts FROM events WHERE type='psyche_molt' ORDER BY ts DESC LIMIT 2`
	out, err := exec.Command(bin, "-separator", "\x1f", db, sql).Output()
	if err != nil {
		msg := ""
		if ee, ok := err.(*exec.ExitError); ok {
			msg = strings.TrimSpace(string(ee.Stderr))
		}
		if msg != "" {
			return time.Time{}, time.Time{}, time.Time{}, false, fmt.Errorf("sqlite3: %s", msg)
		}
		return time.Time{}, time.Time{}, time.Time{}, false, fmt.Errorf("sqlite3 query failed: %w", err)
	}

	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return time.Time{}, time.Time{}, time.Time{}, true, nil
	}
	lines := strings.Split(raw, "\n")
	latest, err := strconv.ParseFloat(strings.TrimSpace(lines[0]), 64)
	if err != nil || latest <= 0 {
		return time.Time{}, time.Time{}, time.Time{}, false, fmt.Errorf("invalid psyche_molt ts %q", strings.TrimSpace(lines[0]))
	}
	currentSince = unixFloatTimeUTC(latest)
	if len(lines) > 1 {
		previous, err := strconv.ParseFloat(strings.TrimSpace(lines[1]), 64)
		if err != nil || previous <= 0 {
			return time.Time{}, time.Time{}, time.Time{}, false, fmt.Errorf("invalid previous psyche_molt ts %q", strings.TrimSpace(lines[1]))
		}
		lastSince = unixFloatTimeUTC(previous)
		lastBefore = currentSince
	}
	return currentSince, lastSince, lastBefore, true, nil
}

// QueryRecentMoltTimes fetches the most recent psyche_molt (context rebuild)
// timestamps from the sqlite sidecar, newest first, capped at limit. It is a
// targeted, LIMIT-bounded query — never a full table scan. Used to mark
// molt boundaries in the /kanban Ctrl+D ledger. Degrades like the other
// queries here: a missing database or binary returns a descriptive error and
// a nil slice, and the caller falls back to JSONL or draws nothing.
func QueryRecentMoltTimes(agentDir string, limit int) ([]time.Time, error) {
	return queryRecentEventTimes(agentDir, "psyche_molt", limit)
}

// QueryRecentRefreshCompleteTimes fetches the most recent refresh_complete
// (/refresh context reconstruction) timestamps from the sqlite sidecar,
// newest first, capped at limit. Same targeted LIMIT-bounded contract and
// graceful degradation as QueryRecentMoltTimes. refresh_start is deliberately
// excluded — only completed refreshes mark a reconstruction boundary.
func QueryRecentRefreshCompleteTimes(agentDir string, limit int) ([]time.Time, error) {
	return queryRecentEventTimes(agentDir, "refresh_complete", limit)
}

// queryRecentEventTimes runs a targeted, LIMIT-bounded query for the newest
// timestamps of a single event type. eventType is a fixed internal constant
// (never user input), so it is interpolated directly into the SQL.
func queryRecentEventTimes(agentDir, eventType string, limit int) ([]time.Time, error) {
	if limit <= 0 {
		limit = 10
	}
	db := DBPath(agentDir)
	if _, err := os.Stat(db); err != nil {
		return nil, fmt.Errorf("sqlite sidecar not found: %s", db)
	}
	bin, err := findSQLite3()
	if err != nil {
		return nil, err
	}

	sql := fmt.Sprintf(`SELECT ts FROM events WHERE type='%s' ORDER BY ts DESC LIMIT %d`, eventType, limit)
	out, err := exec.Command(bin, "-separator", "\x1f", db, sql).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			if msg := strings.TrimSpace(string(ee.Stderr)); msg != "" {
				return nil, fmt.Errorf("sqlite3: %s", msg)
			}
		}
		return nil, fmt.Errorf("sqlite3 query failed: %w", err)
	}

	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil, nil
	}
	var times []time.Time
	for _, line := range strings.Split(raw, "\n") {
		ts, err := strconv.ParseFloat(strings.TrimSpace(line), 64)
		if err != nil || ts <= 0 {
			continue
		}
		times = append(times, unixFloatTimeUTC(ts))
	}
	return times, nil
}

func unixFloatTimeUTC(ts float64) time.Time {
	sec := int64(ts)
	nsec := int64((ts - float64(sec)) * 1e9)
	return time.Unix(sec, nsec).UTC()
}
