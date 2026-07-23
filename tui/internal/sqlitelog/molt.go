package sqlitelog

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// moltSessionWindowQueryTimeout bounds the derived-sidecar sqlite3 subprocess.
// Normal home telemetry resolves session boundaries in-process from canonical
// events.jsonl and never reaches this helper; it remains for detail views and for
// compatibility when an old/incomplete agent directory has only log.sqlite. A
// pathological sidecar must not pin those callers forever, so expiry degrades to
// the last cached window (see moltWindowCache). The conservative 1s allowance is
// far above a normal writer's brief lock.
const moltSessionWindowQueryTimeout = 1 * time.Second

// moltSessionWindowBusyTimeoutMS is the sqlite busy_timeout (milliseconds) for
// the derived-sidecar compatibility/detail read. A short wait rides out a normal
// concurrent kernel write while remaining well within the outer deadline.
const moltSessionWindowBusyTimeoutMS = 150

// moltSessionWindowCacheTTL is the minimum interval between live sqlite3
// launches for the same agent's derived-sidecar molt windows. Detail and
// compatibility callers may arrive back-to-back, while the boundary itself only
// moves when the kernel writes a psyche_molt row, so a sub-second floor avoids
// needless process churn without observably changing the window.
const moltSessionWindowCacheTTL = 750 * time.Millisecond

// moltWindow is a resolved molt-session-window result plus enough freshness
// metadata to decide when a cached copy may be reused without re-querying.
type moltWindow struct {
	currentSince time.Time
	lastSince    time.Time
	lastBefore   time.Time
	ok           bool

	dbSize    int64     // sidecar size at query time — a cheap change detector
	dbModTime time.Time // sidecar mtime at query time
	queriedAt time.Time // wall clock of the successful query, for the TTL floor
}

// moltWindowCache holds the last successful derived-sidecar query per agent.
// Its two jobs are to skip the subprocess within moltSessionWindowCacheTTL when
// the sidecar is unchanged and to serve a stale window if a later live query
// times out or errors.
var moltWindowCache = struct {
	sync.Mutex
	byDir map[string]moltWindow

	// now is overridable in tests so TTL behavior is deterministic without
	// sleeping. Production always uses time.Now.
	now func() time.Time
}{byDir: map[string]moltWindow{}, now: time.Now}

func moltWindowNow() time.Time {
	moltWindowCache.Lock()
	fn := moltWindowCache.now
	moltWindowCache.Unlock()
	if fn == nil {
		return time.Now()
	}
	return fn()
}

// QueryMoltSessionWindows fetches the latest two psyche_molt timestamps from
// the derived SQLite sidecar. It returns the current session lower bound, the
// previous session lower bound, and the previous session upper bound.
//
// This is a compatibility/detail helper, not the normal home-telemetry path.
// The subprocess has a deadline and busy timeout; moltWindowCache skips an
// unchanged recent sidecar and serves the last good result if a later query
// fails. A first-query failure remains visible so the caller can choose its own
// canonical-source fallback.
func QueryMoltSessionWindows(agentDir string) (currentSince, lastSince, lastBefore time.Time, ok bool, err error) {
	db := DBPath(agentDir)
	info, statErr := os.Stat(db)
	if statErr != nil {
		return time.Time{}, time.Time{}, time.Time{}, false, fmt.Errorf("sqlite sidecar not found: %s", db)
	}

	// Fast path: a recent cached window for an unchanged sidecar. Skips the
	// subprocess entirely for back-to-back detail/compatibility calls.
	if w, hit := cachedMoltWindow(agentDir, info); hit {
		return w.currentSince, w.lastSince, w.lastBefore, w.ok, nil
	}

	bin, err := findSQLite3()
	if err != nil {
		return time.Time{}, time.Time{}, time.Time{}, false, err
	}

	w, qErr := queryMoltWindowLive(bin, db)
	if qErr != nil {
		// Degrade to the last good window (however stale). Only when no successful
		// result exists do we propagate the error so the caller can choose its
		// canonical-source fallback.
		if stale, has := lastMoltWindow(agentDir); has {
			return stale.currentSince, stale.lastSince, stale.lastBefore, stale.ok, nil
		}
		return time.Time{}, time.Time{}, time.Time{}, false, qErr
	}

	w.dbSize = info.Size()
	w.dbModTime = info.ModTime()
	w.queriedAt = moltWindowNow()
	storeMoltWindow(agentDir, w)
	return w.currentSince, w.lastSince, w.lastBefore, w.ok, nil
}

// queryMoltWindowLive runs the actual sqlite3 subprocess under a deadline and a
// busy_timeout. Errors (including a killed-on-deadline subprocess) are returned
// for the caller to degrade to cache.
func queryMoltWindowLive(bin, db string) (moltWindow, error) {
	ctx, cancel := context.WithTimeout(context.Background(), moltSessionWindowQueryTimeout)
	defer cancel()

	// The `.timeout` dot-command sets sqlite's busy_timeout for the session so the
	// read waits out a concurrent kernel writer instead of failing instantly with
	// SQLITE_BUSY. It runs via -cmd (before the SELECT) rather than as an inline
	// `PRAGMA busy_timeout=N;` statement because setting that pragma emits its new
	// value as a result row, which would corrupt the ts parsing below. Both the
	// timeout and the SELECT are fixed constants — never user input.
	const sql = `SELECT ts FROM events WHERE type='psyche_molt' ORDER BY ts DESC LIMIT 2`
	out, err := exec.CommandContext(ctx, bin,
		"-separator", "\x1f",
		"-cmd", fmt.Sprintf(".timeout %d", moltSessionWindowBusyTimeoutMS),
		db, sql,
	).Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return moltWindow{}, fmt.Errorf("sqlite3 molt-window query timed out after %s", moltSessionWindowQueryTimeout)
		}
		if ee, ok := err.(*exec.ExitError); ok {
			if msg := strings.TrimSpace(string(ee.Stderr)); msg != "" {
				return moltWindow{}, fmt.Errorf("sqlite3: %s", msg)
			}
		}
		return moltWindow{}, fmt.Errorf("sqlite3 query failed: %w", err)
	}

	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return moltWindow{ok: true}, nil
	}
	lines := strings.Split(raw, "\n")
	latest, err := strconv.ParseFloat(strings.TrimSpace(lines[0]), 64)
	if err != nil || latest <= 0 {
		return moltWindow{}, fmt.Errorf("invalid psyche_molt ts %q", strings.TrimSpace(lines[0]))
	}
	w := moltWindow{ok: true, currentSince: unixFloatTimeUTC(latest)}
	if len(lines) > 1 {
		previous, err := strconv.ParseFloat(strings.TrimSpace(lines[1]), 64)
		if err != nil || previous <= 0 {
			return moltWindow{}, fmt.Errorf("invalid previous psyche_molt ts %q", strings.TrimSpace(lines[1]))
		}
		w.lastSince = unixFloatTimeUTC(previous)
		w.lastBefore = w.currentSince
	}
	return w, nil
}

// cachedMoltWindow returns a cached window that is safe to reuse without a live
// query: the sidecar is byte-identical (size+mtime) to when it was cached AND
// the cache entry is younger than moltSessionWindowCacheTTL. The size+mtime gate
// means a kernel write (new psyche_molt) invalidates the cache immediately; the
// TTL floor keeps back-to-back background fetches from re-launching the subprocess
// even while a write is streaming in.
func cachedMoltWindow(agentDir string, info os.FileInfo) (moltWindow, bool) {
	moltWindowCache.Lock()
	defer moltWindowCache.Unlock()
	w, ok := moltWindowCache.byDir[agentDir]
	if !ok {
		return moltWindow{}, false
	}
	if w.dbSize != info.Size() || !w.dbModTime.Equal(info.ModTime()) {
		return moltWindow{}, false
	}
	nowFn := moltWindowCache.now
	if nowFn == nil {
		nowFn = time.Now
	}
	if nowFn().Sub(w.queriedAt) >= moltSessionWindowCacheTTL {
		return moltWindow{}, false
	}
	return w, true
}

// lastMoltWindow returns the last stored window for agentDir regardless of age
// or sidecar changes — the stale-degradation source used when a live query
// times out or errors.
func lastMoltWindow(agentDir string) (moltWindow, bool) {
	moltWindowCache.Lock()
	defer moltWindowCache.Unlock()
	w, ok := moltWindowCache.byDir[agentDir]
	return w, ok
}

func storeMoltWindow(agentDir string, w moltWindow) {
	moltWindowCache.Lock()
	defer moltWindowCache.Unlock()
	moltWindowCache.byDir[agentDir] = w
}

// QueryMoltSessionToolCallCounts counts lifecycle type='tool_call' events in
// the same current and previous molt-session windows QueryMoltSessionWindows
// resolves. currentSince is the current window lower bound (open-ended); when
// a previous session exists (lastBefore non-zero) the previous window is
// [lastSince, lastBefore). A zero currentSince (no psyche_molt yet) counts
// every tool_call event as current, matching the token-ledger "current = all"
// semantics. tool_result events are never counted.
//
// It runs a single bounded COUNT pass over tool_call rows at/after the oldest
// relevant window lower bound, under the same conservative deadline and sqlite
// busy_timeout as the molt-window query. Ctrl+D detail refresh reaches this from
// the Bubble Tea update path (never View rendering), so the deadline caps the
// synchronous wait. It returns ok=false on a missing sidecar/binary, timeout, or
// query error so the caller falls back to events.jsonl.
func QueryMoltSessionToolCallCounts(agentDir string, currentSince, lastSince, lastBefore time.Time) (current, last int64, ok bool, err error) {
	db := DBPath(agentDir)
	if _, err := os.Stat(db); err != nil {
		return 0, 0, false, fmt.Errorf("sqlite sidecar not found: %s", db)
	}
	bin, err := findSQLite3()
	if err != nil {
		return 0, 0, false, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), moltSessionWindowQueryTimeout)
	defer cancel()

	cs := unixFloatSeconds(currentSince)
	ls := unixFloatSeconds(lastSince)
	lb := unixFloatSeconds(lastBefore)
	// Scan only tool_call rows at/after the oldest relevant lower bound. The
	// previous window only exists when lastBefore is set; until then its lower
	// bound is irrelevant and the scan starts at the current bound (or 0 when
	// there has been no molt at all, which makes the current bucket count every
	// tool_call row, matching the ledger).
	scanLower := cs
	if !lastBefore.IsZero() {
		scanLower = ls
	}
	// COUNT(CASE WHEN ... THEN 1 END) yields 0 (not NULL) for an empty bucket,
	// so a window with no tool_call rows reports 0 without a COALESCE. The
	// previous bucket's conditions are gated on lastBefore > 0 so they collapse
	// to a never-true predicate when there is no previous session. Rebuilt
	// sidecars may also contain daemon event logs; exclude those so these counts
	// match the main-agent-only token-ledger API rows. NULL scope/source_kind is
	// kept for compatibility with older sidecars that predate trace metadata.
	const mainAgentToolCallFilter = `type='tool_call' ` +
		`AND (scope IS NULL OR scope != 'daemon') ` +
		`AND (source_kind IS NULL OR source_kind != 'daemon_events')`
	sql := fmt.Sprintf(
		`SELECT COUNT(CASE WHEN ts >= %s THEN 1 END), `+
			`COUNT(CASE WHEN %s > 0 AND ts >= %s AND ts < %s THEN 1 END) `+
			`FROM events WHERE %s AND ts >= %s`,
		cs, lb, ls, lb, mainAgentToolCallFilter, scanLower,
	)
	out, err := exec.CommandContext(ctx, bin,
		"-separator", "\x1f",
		"-cmd", fmt.Sprintf(".timeout %d", moltSessionWindowBusyTimeoutMS),
		db, sql,
	).Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return 0, 0, false, fmt.Errorf("sqlite3 tool-call count query timed out after %s", moltSessionWindowQueryTimeout)
		}
		if ee, ok := err.(*exec.ExitError); ok {
			if msg := strings.TrimSpace(string(ee.Stderr)); msg != "" {
				return 0, 0, false, fmt.Errorf("sqlite3: %s", msg)
			}
		}
		return 0, 0, false, fmt.Errorf("sqlite3 tool-call count query failed: %w", err)
	}

	row := strings.Split(strings.TrimSpace(string(out)), "\x1f")
	if len(row) < 2 {
		return 0, 0, false, fmt.Errorf("sqlite3 tool-call count: unexpected output %q", strings.TrimSpace(string(out)))
	}
	current, perr := strconv.ParseInt(strings.TrimSpace(row[0]), 10, 64)
	if perr != nil {
		return 0, 0, false, fmt.Errorf("sqlite3 tool-call current count %q: %w", row[0], perr)
	}
	last, perr = strconv.ParseInt(strings.TrimSpace(row[1]), 10, 64)
	if perr != nil {
		return 0, 0, false, fmt.Errorf("sqlite3 tool-call last count %q: %w", row[1], perr)
	}
	return current, last, true, nil
}

// unixFloatSeconds renders a molt-window bound as a decimal seconds literal for
// interpolation into a COUNT query's ts comparison. FormatFloat with precision
// -1 preserves the float64 representation of the time's nanosecond value without
// imposing an arbitrary decimal precision. A zero time renders as "0".
func unixFloatSeconds(t time.Time) string {
	if t.IsZero() {
		return "0"
	}
	return strconv.FormatFloat(float64(t.UnixNano())/1e9, 'f', -1, 64)
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
