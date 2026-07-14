package fs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestSessionCacheConcurrentRebuildAndRefresh is the regression test for the
// launch-perf concurrency blocker (GLM review B1): deferring the authoritative
// RebuildFromSources into a Bubble Tea Cmd goroutine made it race with the
// periodic mail tick, which keeps calling Refresh + Entries on the main
// goroutine. Both mutate the same *SessionCache (entries slice, offsets,
// rebuilding flag, and session.jsonl on disk) with no synchronization in the
// pre-fix code.
//
// This test reproduces that exact overlap: one goroutine repeatedly runs
// RebuildFromSources (the deferred launch work) while another repeatedly runs
// Refresh / Entries / Len (the tick path), against a content-heavy orchestrator
// log so the rebuild has real work to do. Run with `-race`; before the mutex
// fix the race detector fires on sc.entries / sc.eventsOff / the rebuilding
// flag. After the fix it must pass cleanly and never panic.
func TestSessionCacheConcurrentRebuildAndRefresh(t *testing.T) {
	tmp := t.TempDir()
	humanDir := filepath.Join(tmp, "human")
	orchDir := filepath.Join(tmp, "orch")
	logsDir := filepath.Join(orchDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Content-heavy events.jsonl — this is the scenario the patch targets, where
	// RebuildFromSources takes long enough that ticks overlap it.
	var b strings.Builder
	for i := 0; i < 4000; i++ {
		fmt.Fprintf(&b, `{"ts":%d,"type":"text_output","text":"line %d with some body content to parse"}`+"\n", 1781300000+i, i)
	}
	if err := os.WriteFile(filepath.Join(logsDir, "events.jsonl"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	sc := NewSessionCache(humanDir, tmp, MainAggregateWriter)
	cache := NewMailCache(humanDir).Refresh()

	var wg sync.WaitGroup

	// Goroutine 1: the deferred authoritative rebuild, run repeatedly to keep it
	// overlapping the refresh loop (mirrors initialRebuild on the Cmd goroutine).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			sc.RebuildFromSources(cache, "human", orchDir, "orch")
		}
	}()

	// Goroutine 2: the tick path — Refresh then read Entries/Len, the exact
	// buildMessages sequence that runs on Bubble Tea's main goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			sc.Refresh(cache, "human", orchDir, "orch")
			entries := sc.Entries()
			for range entries {
				// Touch the snapshot the way buildMessages iterates it; this is
				// where a non-copying Entries() would race the rebuild's mutation.
			}
			_ = sc.Len()
		}
	}()

	wg.Wait()

	// After all the churn the cache must still reflect the full event history
	// (every entry ingested at least once), and the on-disk file must be
	// well-formed — not truncated mid-write by a concurrent rewrite/append.
	if got := sc.Len(); got != 4000 {
		t.Fatalf("after concurrent rebuild/refresh: expected 4000 entries, got %d", got)
	}
	data, err := os.ReadFile(filepath.Join(humanDir, "logs", "session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := 0
	for _, c := range data {
		if c == '\n' {
			lines++
		}
	}
	if lines != 4000 {
		t.Fatalf("session.jsonl: expected 4000 lines, got %d (concurrent truncate/append corruption?)", lines)
	}
}
