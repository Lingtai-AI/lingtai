package fs

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecentRefreshCompleteTimesJSONLFallback(t *testing.T) {
	agentDir := t.TempDir()
	logsDir := filepath.Join(agentDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"type":"text_input","ts":1000.0}`,
		`{"type":"refresh_start","ts":1090.0}`,
		`{"type":"refresh_complete","ts":1100.0}`,
		`{"type":"tool_call","ts":1200.0}`,
		`{"type":"refresh_complete","ts":1300.5}`,
	}
	if err := os.WriteFile(filepath.Join(logsDir, "events.jsonl"),
		[]byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// No log.sqlite present, so the reader falls back to the JSONL tail.
	times := RecentRefreshCompleteTimes(agentDir, 10)
	if len(times) != 2 {
		t.Fatalf("expected 2 refresh_complete times, got %d", len(times))
	}
	if times[0].Unix() != 1300 || times[1].Unix() != 1100 {
		t.Fatalf("expected newest-first (1300,1100), got %d,%d", times[0].Unix(), times[1].Unix())
	}
}

func TestRecentRefreshCompleteTimesPrefersSQLite(t *testing.T) {
	sqliteBin, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 not available")
	}
	agentDir := t.TempDir()
	logsDir := filepath.Join(agentDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// JSONL has a refresh the sqlite sidecar does not — proves sqlite wins.
	if err := os.WriteFile(filepath.Join(logsDir, "events.jsonl"),
		[]byte(`{"type":"refresh_complete","ts":1.0}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	createSQL := `
		CREATE TABLE events (id INTEGER PRIMARY KEY AUTOINCREMENT, ts REAL NOT NULL, type TEXT NOT NULL, fields_json TEXT NOT NULL DEFAULT '{}');
		INSERT INTO events (ts, type) VALUES (5000.0, 'refresh_complete');
	`
	cmd := exec.Command(sqliteBin, filepath.Join(logsDir, "log.sqlite"), createSQL)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sqlite3 setup failed: %v\n%s", err, out)
	}

	times := RecentRefreshCompleteTimes(agentDir, 10)
	if len(times) != 1 {
		t.Fatalf("expected 1 refresh time from sqlite, got %d", len(times))
	}
	if times[0].Unix() != 5000 {
		t.Fatalf("expected sqlite refresh (5000), got %d", times[0].Unix())
	}
}
