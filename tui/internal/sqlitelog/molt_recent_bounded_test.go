package sqlitelog

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestQueryRecentEventTimesDeadlineBounded is the issue #526 regression guard for
// the /kanban Ctrl+D detail path. RecentRebuildTimes / RecentRefreshCompleteTimes
// reload via queryRecentEventTimes synchronously on the Bubble Tea Update thread
// every auto-refresh tick (tui: autoRefreshActiveView -> PropsModel.loadDetail).
// A slow or lock-contended sqlite3 subprocess on an external volume must NOT pin
// that loop indefinitely: the read runs under a worker-local context deadline
// (moltSessionWindowQueryTimeout), so a hung subprocess is killed and the caller
// degrades to the bounded events.jsonl tail instead of stalling input/render.
//
// The test shadows the sqlite3 binary on PATH with a stub that hangs forever,
// then asserts queryRecentEventTimes returns an error promptly — bounded by the
// deadline, not the (infinite) subprocess. Before the fix (a plain exec.Command
// with no context) this call would never return.
func TestQueryRecentEventTimesDeadlineBounded(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hanging-stub shim is POSIX shell only")
	}
	// Build a real sidecar first (needs the real sqlite3), then shadow PATH.
	agentDir := makeTestDB(t,
		`INSERT INTO events(ts,type,fields_json) VALUES(1000.0,'psyche_molt','{}');`,
	)
	shadowSQLite3WithHang(t)

	start := time.Now()
	_, err := QueryRecentMoltTimes(agentDir, 10)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a deadline/error from the hanging sqlite3 stub, got nil")
	}
	// Must return within the deadline plus a generous margin for subprocess
	// teardown and CI jitter — the point is that it returns AT ALL, quickly,
	// rather than hanging on the never-exiting stub.
	maxBound := moltSessionWindowQueryTimeout + time.Second
	if elapsed > maxBound {
		t.Fatalf("QueryRecentMoltTimes blocked for %s on a hung sqlite3 (bound %s); "+
			"the context deadline guard is missing (err=%v)", elapsed, maxBound, err)
	}
}

// shadowSQLite3WithHang prepends a temp dir to PATH containing a `sqlite3`
// executable that ignores its args and sleeps far past the query deadline, so
// exec.LookPath("sqlite3") in findSQLite3 resolves to it. PATH is restored on
// test cleanup.
func shadowSQLite3WithHang(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	stub := filepath.Join(dir, "sqlite3")
	// `sleep 3600` outlives moltSessionWindowQueryTimeout (1s) by a wide margin;
	// the context deadline must fire and kill it long before then.
	script := "#!/bin/sh\nexec sleep 3600\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	prev := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+prev)
}
