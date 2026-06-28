package fs

import (
	"fmt"
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

func TestRecentRefreshCompleteTimesJSONLTailOnly(t *testing.T) {
	agentDir := t.TempDir()
	logsDir := filepath.Join(agentDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A refresh_complete far above the tail window must NOT be seen; only the
	// recent tail is inspected, per the no-full-scan constraint.
	var b strings.Builder
	b.WriteString(`{"type":"refresh_complete","ts":1.0}` + "\n") // line 1 — outside the tail
	for i := 0; i < tailScanLines+50; i++ {
		fmt.Fprintf(&b, `{"type":"tool_call","ts":%d.0}`+"\n", 1000+i)
	}
	b.WriteString(`{"type":"refresh_complete","ts":77777.0}` + "\n") // near end — inside the tail
	if err := os.WriteFile(filepath.Join(logsDir, "events.jsonl"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	times := RecentRefreshCompleteTimes(agentDir, 10)
	if len(times) != 1 {
		t.Fatalf("expected only the in-tail refresh_complete, got %d", len(times))
	}
	if times[0].Unix() != 77777 {
		t.Fatalf("expected the recent refresh_complete (77777), got %d", times[0].Unix())
	}
}

func TestRecentRefreshCompleteTimesHugePrefixLineDoesNotBlockTail(t *testing.T) {
	agentDir := t.TempDir()
	logsDir := filepath.Join(agentDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	huge := strings.Repeat("x", 2*1024*1024)
	fmt.Fprintf(&b, `{"type":"text_input","ts":1.0,"junk":"%s"}`+"\n", huge)
	for i := 0; i < tailScanLines+50; i++ {
		fmt.Fprintf(&b, `{"type":"tool_call","ts":%d.0}`+"\n", 1000+i)
	}
	b.WriteString(`{"type":"refresh_complete","ts":66666.0}` + "\n")
	if err := os.WriteFile(filepath.Join(logsDir, "events.jsonl"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	times := RecentRefreshCompleteTimes(agentDir, 10)
	if len(times) != 1 {
		t.Fatalf("expected the in-tail refresh_complete despite huge prefix line, got %d", len(times))
	}
	if times[0].Unix() != 66666 {
		t.Fatalf("expected the recent refresh_complete (66666), got %d", times[0].Unix())
	}
}

func TestRecentRefreshCompleteTimesMissingLogsIsEmpty(t *testing.T) {
	if times := RecentRefreshCompleteTimes(t.TempDir(), 10); len(times) != 0 {
		t.Fatalf("expected no markers for missing logs, got %d", len(times))
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
