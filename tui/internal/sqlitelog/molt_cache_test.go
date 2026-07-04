package sqlitelog

import (
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

// resetMoltWindowCache clears the process-global molt-window cache and installs a
// controllable clock so TTL behavior is deterministic. It restores the real
// time.Now clock on test cleanup so it cannot leak into other tests.
func resetMoltWindowCache(t *testing.T, clock func() time.Time) {
	t.Helper()
	moltWindowCache.Lock()
	moltWindowCache.byDir = map[string]moltWindow{}
	prev := moltWindowCache.now
	moltWindowCache.now = clock
	moltWindowCache.Unlock()
	t.Cleanup(func() {
		moltWindowCache.Lock()
		moltWindowCache.byDir = map[string]moltWindow{}
		moltWindowCache.now = prev
		moltWindowCache.Unlock()
	})
}

// Instead of intercepting exec, the cache tests assert query behavior via the
// returned windows and the stored cache entry's queriedAt stamp.

// TestQueryMoltSessionWindowsCachesWithinTTL asserts that a second call within
// the TTL for an unchanged sidecar reuses the cached window: the stored
// queriedAt stamp does not advance, proving no second subprocess ran.
func TestQueryMoltSessionWindowsCachesWithinTTL(t *testing.T) {
	agentDir := makeTestDB(t,
		`INSERT INTO events(ts,type,fields_json) VALUES(1000.0,'psyche_molt','{}');`,
		`INSERT INTO events(ts,type,fields_json) VALUES(1002.5,'psyche_molt','{}');`,
	)

	base := time.Unix(1_700_000_000, 0)
	clock := base
	resetMoltWindowCache(t, func() time.Time { return clock })

	// First call: cold cache, runs the real query and stamps queriedAt=base.
	cur1, _, _, ok1, err1 := QueryMoltSessionWindows(agentDir)
	if err1 != nil || !ok1 {
		t.Fatalf("first query: ok=%v err=%v", ok1, err1)
	}
	firstStamp := storedQueriedAt(t, agentDir)
	if !firstStamp.Equal(base) {
		t.Fatalf("first queriedAt = %v, want %v", firstStamp, base)
	}

	// Advance the clock but stay within the TTL. The sidecar is unchanged, so the
	// second call must be a cache hit: same window, and queriedAt unchanged
	// (no fresh subprocess launched).
	clock = base.Add(moltSessionWindowCacheTTL - time.Millisecond)
	cur2, _, _, ok2, err2 := QueryMoltSessionWindows(agentDir)
	if err2 != nil || !ok2 {
		t.Fatalf("second query: ok=%v err=%v", ok2, err2)
	}
	if !cur2.Equal(cur1) {
		t.Fatalf("cached window changed: %v != %v", cur2, cur1)
	}
	if got := storedQueriedAt(t, agentDir); !got.Equal(firstStamp) {
		t.Fatalf("cache miss within TTL: queriedAt advanced to %v (want %v)", got, firstStamp)
	}
}

// TestQueryMoltSessionWindowsRequeriesAfterTTL asserts that once the TTL has
// elapsed, an unchanged sidecar is re-queried (queriedAt advances).
func TestQueryMoltSessionWindowsRequeriesAfterTTL(t *testing.T) {
	agentDir := makeTestDB(t,
		`INSERT INTO events(ts,type,fields_json) VALUES(1002.5,'psyche_molt','{}');`,
	)

	base := time.Unix(1_700_000_000, 0)
	clock := base
	resetMoltWindowCache(t, func() time.Time { return clock })

	if _, _, _, ok, err := QueryMoltSessionWindows(agentDir); err != nil || !ok {
		t.Fatalf("first query: ok=%v err=%v", ok, err)
	}
	firstStamp := storedQueriedAt(t, agentDir)

	clock = base.Add(moltSessionWindowCacheTTL)
	if _, _, _, ok, err := QueryMoltSessionWindows(agentDir); err != nil || !ok {
		t.Fatalf("second query: ok=%v err=%v", ok, err)
	}
	if got := storedQueriedAt(t, agentDir); !got.After(firstStamp) {
		t.Fatalf("expected re-query after TTL: queriedAt=%v did not advance past %v", got, firstStamp)
	}
}

// TestQueryMoltSessionWindowsInvalidatesOnSidecarChange asserts that a changed
// sidecar (new mtime/size) forces a re-query even within the TTL, so a fresh
// psyche_molt is picked up promptly.
func TestQueryMoltSessionWindowsInvalidatesOnSidecarChange(t *testing.T) {
	agentDir := makeTestDB(t,
		`INSERT INTO events(ts,type,fields_json) VALUES(1000.0,'psyche_molt','{}');`,
	)

	base := time.Unix(1_700_000_000, 0)
	clock := base
	resetMoltWindowCache(t, func() time.Time { return clock })

	cur1, _, _, ok, err := QueryMoltSessionWindows(agentDir)
	if err != nil || !ok {
		t.Fatalf("first query: ok=%v err=%v", ok, err)
	}
	if got := cur1.Unix(); got != 1000 {
		t.Fatalf("first current = %d, want 1000", got)
	}

	// Append a newer molt and bump mtime forward so the size/mtime gate trips
	// even though we are still within the TTL window.
	appendMolt(t, agentDir, 2000.0)
	bumpModTime(t, DBPath(agentDir), base.Add(time.Second))
	clock = base.Add(moltSessionWindowCacheTTL / 4) // still within TTL

	cur2, _, _, ok, err := QueryMoltSessionWindows(agentDir)
	if err != nil || !ok {
		t.Fatalf("second query: ok=%v err=%v", ok, err)
	}
	if got := cur2.Unix(); got != 2000 {
		t.Fatalf("sidecar change not picked up: current = %d, want 2000", got)
	}
}

// TestQueryMoltSessionWindowsDegradesToStaleOnError is the core regression guard:
// when a live query cannot succeed, QueryMoltSessionWindows must return the last
// good (stale) window with ok=true and err=nil, so the caller does NOT fall back
// to the expensive events.jsonl parse on the UI path.
func TestQueryMoltSessionWindowsDegradesToStaleOnError(t *testing.T) {
	agentDir := makeTestDB(t,
		`INSERT INTO events(ts,type,fields_json) VALUES(1500.0,'psyche_molt','{}');`,
	)

	base := time.Unix(1_700_000_000, 0)
	clock := base
	resetMoltWindowCache(t, func() time.Time { return clock })

	cur1, _, _, ok, err := QueryMoltSessionWindows(agentDir)
	if err != nil || !ok {
		t.Fatalf("first query: ok=%v err=%v", ok, err)
	}
	if got := cur1.Unix(); got != 1500 {
		t.Fatalf("first current = %d, want 1500", got)
	}

	// Corrupt the sidecar so any live query fails, and change its mtime/size so the
	// cache's freshness gate forces a live attempt (which will error).
	db := DBPath(agentDir)
	if err := os.WriteFile(db, []byte("not a sqlite database at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	bumpModTime(t, db, base.Add(time.Second))
	clock = base.Add(2 * moltSessionWindowCacheTTL) // past TTL: must attempt live

	cur2, _, _, ok, err := QueryMoltSessionWindows(agentDir)
	if err != nil {
		t.Fatalf("degradation should not surface an error, got %v", err)
	}
	if !ok {
		t.Fatal("degradation should report ok=true so the caller trusts the stale window")
	}
	if got := cur2.Unix(); got != 1500 {
		t.Fatalf("expected stale window 1500, got %d", got)
	}
}

// TestQueryMoltSessionWindowsErrorsWithoutPriorCache asserts that when there is
// no prior good window to fall back to, a failing live query still propagates an
// error/ok=false — the first-ever query must be honest so the caller can do a
// correct (one-time, off-hot-path) parse.
func TestQueryMoltSessionWindowsErrorsWithoutPriorCache(t *testing.T) {
	agentDir := makeTestDB(t)
	base := time.Unix(1_700_000_000, 0)
	resetMoltWindowCache(t, func() time.Time { return base })

	// Corrupt before the first-ever query so there is nothing cached.
	db := DBPath(agentDir)
	if err := os.WriteFile(db, []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, _, ok, err := QueryMoltSessionWindows(agentDir)
	if err == nil || ok {
		t.Fatalf("expected error/ok=false with no prior cache, got ok=%v err=%v", ok, err)
	}
}

// --- test helpers ---

func storedQueriedAt(t *testing.T, agentDir string) time.Time {
	t.Helper()
	moltWindowCache.Lock()
	defer moltWindowCache.Unlock()
	w, ok := moltWindowCache.byDir[agentDir]
	if !ok {
		t.Fatalf("no cache entry for %s", agentDir)
	}
	return w.queriedAt
}

func appendMolt(t *testing.T, agentDir string, ts float64) {
	t.Helper()
	bin, err := findSQLite3()
	if err != nil {
		t.Skip("sqlite3 not available:", err)
	}
	db := DBPath(agentDir)
	sql := "INSERT INTO events(ts,type,fields_json) VALUES(" +
		strconv.FormatFloat(ts, 'f', 1, 64) + ",'psyche_molt','{}');"
	if out, err := exec.Command(bin, db, sql).CombinedOutput(); err != nil {
		t.Fatalf("appendMolt: %v\n%s", err, out)
	}
}

func bumpModTime(t *testing.T, path string, mod time.Time) {
	t.Helper()
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}
