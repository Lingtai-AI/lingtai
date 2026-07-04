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

// moltSessionWindowQueryTimeout is a WORKER-LOCAL BACKSTOP that bounds how long
// the molt-session-window sqlite3 subprocess may run before it is killed. It is
// NOT the mechanism that protects the UI thread: home telemetry is gathered on a
// background tea.Cmd (tui: fetchHomeTelemetry), never on the render (View) or
// keypress (syncViewportHeight) path, so the UI never waits on this query. This
// deadline exists only so a pathological sidecar — e.g. the kernel wedged holding
// the write lock indefinitely — cannot pin a background worker forever; on expiry
// the query degrades to the last cached window (see moltWindowCache). It is
// deliberately CONSERVATIVE (1s, far above a normal write's brief lock) precisely
// because it no longer sits on a latency-sensitive path: a tight deadline here
// would only cause spurious stale reads with no UI-responsiveness benefit.
const moltSessionWindowQueryTimeout = 1 * time.Second

// moltSessionWindowBusyTimeoutMS is the sqlite busy_timeout (milliseconds) applied
// to the molt-session-window read. A concurrent kernel writer briefly holds the
// database lock; without busy_timeout the read fails instantly with SQLITE_BUSY,
// which the caller (fs.SumMoltSessionTokenLedger) treats as "no sqlite result"
// and falls back to an expensive full events.jsonl parse. A short busy_timeout
// lets the read wait out a normal write and still return promptly, well within
// moltSessionWindowQueryTimeout. This too runs on the background worker, not the
// UI thread.
const moltSessionWindowBusyTimeoutMS = 150

// moltSessionWindowCacheTTL is the minimum interval between two live sqlite
// subprocess launches for the same agent's molt-session windows. It is a
// secondary optimization: the TUI already debounces telemetry fetches at the
// model level (tui: homeTelemetryTTL), so in practice this rarely fires, but it
// keeps any other caller (and back-to-back background fetches) from relaunching
// the subprocess needlessly. The molt window itself only moves when the kernel
// writes a new psyche_molt row (minutes apart at most), so a sub-second staleness
// on the boundary is invisible to the token totals, which are separately re-summed
// from the ledger by the caller.
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

// moltWindowCache holds the last successful molt-window query per agent. It is a
// process-global cache shared across background telemetry fetches — the caller
// (fs.SumMoltSessionTokenLedger, reached from the fetchHomeTelemetry tea.Cmd)
// copies the model by value and so cannot hold the cache itself, exactly like
// fs.moltSessionTokenLedgerCache. Its two jobs: (1) skip the subprocess entirely
// within moltSessionWindowCacheTTL when the sidecar is unchanged, and (2) serve a
// STALE window if a live query times out or errors, so a locked/slow database
// degrades to stale-but-cheap telemetry instead of the expensive events.jsonl
// fallback path.
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
// the sqlite sidecar. It returns the current session lower bound, the previous
// session lower bound, and the previous session upper bound.
//
// It is called from the background telemetry fetch (tui: fetchHomeTelemetry),
// NOT from the render/keypress path, so it does not need to protect the UI
// thread. It is nonetheless guarded so a single stuck sidecar cannot pin a
// background worker: the sqlite3 subprocess runs under a conservative worker-local
// deadline (moltSessionWindowQueryTimeout) with a sqlite busy timeout, and a
// process-global cache (moltWindowCache) both skips the subprocess for unchanged
// sidecars within moltSessionWindowCacheTTL and serves the last good result when
// a live query times out or errors. On such a degradation it returns ok=true,
// err=nil so the caller uses the (stale) cached window rather than falling back
// to an expensive full events.jsonl parse.
func QueryMoltSessionWindows(agentDir string) (currentSince, lastSince, lastBefore time.Time, ok bool, err error) {
	db := DBPath(agentDir)
	info, statErr := os.Stat(db)
	if statErr != nil {
		return time.Time{}, time.Time{}, time.Time{}, false, fmt.Errorf("sqlite sidecar not found: %s", db)
	}

	// Fast path: a recent cached window for an unchanged sidecar. Skips the
	// subprocess entirely for back-to-back background fetches.
	if w, hit := cachedMoltWindow(agentDir, info); hit {
		return w.currentSince, w.lastSince, w.lastBefore, w.ok, nil
	}

	bin, err := findSQLite3()
	if err != nil {
		return time.Time{}, time.Time{}, time.Time{}, false, err
	}

	w, qErr := queryMoltWindowLive(bin, db)
	if qErr != nil {
		// Degrade to the last good window (however stale) rather than surfacing
		// an error, which would send the caller into the expensive events.jsonl
		// fallback. Only when we have never had a good result do
		// we propagate the error so the caller can do a correct first-time parse.
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
