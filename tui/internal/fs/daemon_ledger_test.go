package fs

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// writeDaemonLedger writes a daemon's per-call token_ledger.jsonl under
// agentDir/daemons/<runID>/logs/token_ledger.jsonl. Each line is one entry.
func writeDaemonLedger(t *testing.T, agentDir, runID string, lines []string) {
	t.Helper()
	dir := filepath.Join(agentDir, "daemons", runID, "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir daemon logs: %v", err)
	}
	body := ""
	for _, l := range lines {
		body += l + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "token_ledger.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatalf("write daemon ledger: %v", err)
	}
}

func TestDaemonRecentLedgerMissing(t *testing.T) {
	agentDir := t.TempDir()
	// No daemons/ directory at all → empty, not an error.
	entries := DaemonRecentLedger(agentDir, 100)
	if len(entries) != 0 {
		t.Fatalf("expected empty, got %d entries", len(entries))
	}
}

func TestDaemonRecentLedgerEmptyDaemonsDir(t *testing.T) {
	agentDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(agentDir, "daemons"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	entries := DaemonRecentLedger(agentDir, 100)
	if len(entries) != 0 {
		t.Fatalf("expected empty, got %d entries", len(entries))
	}
}

func TestDaemonRecentLedgerTagsIdentity(t *testing.T) {
	agentDir := t.TempDir()
	// Daemon run with a daemon.json identity card and one ledger entry.
	writeDaemonState(t, agentDir, "em-1-20260101-000000-abc123", map[string]interface{}{
		"handle": "em-1",
		"run_id": "em-1-20260101-000000-abc123",
		"state":  "running",
		"task":   "do a thing",
		"model":  "glm-4.6",
	})
	writeDaemonLedger(t, agentDir, "em-1-20260101-000000-abc123", []string{
		`{"ts":"2026-01-01T00:00:01","input":10,"output":5,"thinking":1,"cached":2,"model":"glm-4.6","endpoint":"https://z.ai/api"}`,
	})

	entries := DaemonRecentLedger(agentDir, 100)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.RunID != "em-1-20260101-000000-abc123" {
		t.Errorf("RunID = %q", e.RunID)
	}
	if e.Handle != "em-1" {
		t.Errorf("Handle = %q", e.Handle)
	}
	if e.State != "running" {
		t.Errorf("State = %q", e.State)
	}
	if e.Input != 10 || e.Output != 5 {
		t.Errorf("tokens wrong: in=%d out=%d", e.Input, e.Output)
	}
	if e.Model != "glm-4.6" {
		t.Errorf("Model = %q", e.Model)
	}
}

func TestDaemonRecentLedgerAggregatesAndSortsNewestFirst(t *testing.T) {
	agentDir := t.TempDir()
	// Two daemons, interleaved timestamps. The result must be globally sorted
	// newest-first by ts, regardless of which daemon dir they came from.
	writeDaemonState(t, agentDir, "em-1-x", map[string]interface{}{"handle": "em-1", "state": "done"})
	writeDaemonState(t, agentDir, "em-2-y", map[string]interface{}{"handle": "em-2", "state": "running"})
	writeDaemonLedger(t, agentDir, "em-1-x", []string{
		`{"ts":"2026-01-01T00:00:01","input":1}`,
		`{"ts":"2026-01-01T00:00:05","input":5}`,
	})
	writeDaemonLedger(t, agentDir, "em-2-y", []string{
		`{"ts":"2026-01-01T00:00:03","input":3}`,
		`{"ts":"2026-01-01T00:00:09","input":9}`,
	})

	entries := DaemonRecentLedger(agentDir, 100)
	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}
	wantOrder := []int64{9, 5, 3, 1}
	for i, w := range wantOrder {
		if entries[i].Input != w {
			t.Errorf("entry %d input = %d, want %d", i, entries[i].Input, w)
		}
	}
	// Identity tags travel with each entry.
	if entries[0].Handle != "em-2" {
		t.Errorf("newest entry handle = %q, want em-2", entries[0].Handle)
	}
}

func TestDaemonRecentLedgerTrimsToRecentN(t *testing.T) {
	agentDir := t.TempDir()
	writeDaemonState(t, agentDir, "em-1-x", map[string]interface{}{"handle": "em-1", "state": "done"})
	var lines []string
	for i := 0; i < 250; i++ {
		// ts strictly ascending so newest is the last written. Encode the
		// loop index directly into the seconds-fractional slot so ordering is
		// unambiguous past 60.
		lines = append(lines, fmt.Sprintf(`{"ts":"2026-01-01T00:00:00.%06d","input":%d}`, i, i))
	}
	writeDaemonLedger(t, agentDir, "em-1-x", lines)

	entries := DaemonRecentLedger(agentDir, 100)
	if len(entries) != 100 {
		t.Fatalf("expected 100 entries, got %d", len(entries))
	}
	// Newest first: input 249 should lead.
	if entries[0].Input != 249 {
		t.Errorf("newest input = %d, want 249", entries[0].Input)
	}
}

func TestDaemonRecentLedgerSkipsMalformed(t *testing.T) {
	agentDir := t.TempDir()
	writeDaemonState(t, agentDir, "em-1-x", map[string]interface{}{"handle": "em-1", "state": "done"})
	writeDaemonLedger(t, agentDir, "em-1-x", []string{
		`{not json`,
		`{"ts":"2026-01-01T00:00:01","input":7}`,
		``,
	})
	entries := DaemonRecentLedger(agentDir, 100)
	if len(entries) != 1 {
		t.Fatalf("expected 1 valid entry, got %d", len(entries))
	}
	if entries[0].Input != 7 {
		t.Errorf("input = %d, want 7", entries[0].Input)
	}
}

func TestDaemonRecentLedgerMissingDaemonJSON(t *testing.T) {
	agentDir := t.TempDir()
	// Ledger present but no daemon.json — identity tags fall back to run dir name.
	writeDaemonLedger(t, agentDir, "em-9-z", []string{
		`{"ts":"2026-01-01T00:00:01","input":4}`,
	})
	entries := DaemonRecentLedger(agentDir, 100)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].RunID != "em-9-z" {
		t.Errorf("RunID = %q, want em-9-z", entries[0].RunID)
	}
}
