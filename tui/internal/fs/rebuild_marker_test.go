package fs

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecentRebuildTimesJSONLFallback(t *testing.T) {
	agentDir := t.TempDir()
	logsDir := filepath.Join(agentDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"type":"text_input","ts":1000.0}`,
		`{"type":"psyche_molt","ts":1100.0}`,
		`{"type":"tool_call","ts":1200.0}`,
		`{"type":"psyche_molt","ts":1300.5}`,
	}
	if err := os.WriteFile(filepath.Join(logsDir, "events.jsonl"),
		[]byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// No log.sqlite present, so the reader falls back to the JSONL tail.
	times := RecentRebuildTimes(agentDir, 10)
	if len(times) != 2 {
		t.Fatalf("expected 2 rebuild times, got %d", len(times))
	}
	if times[0].Unix() != 1300 || times[1].Unix() != 1100 {
		t.Fatalf("expected newest-first (1300,1100), got %d,%d", times[0].Unix(), times[1].Unix())
	}
}

func TestRecentRebuildTimesJSONLTailOnly(t *testing.T) {
	agentDir := t.TempDir()
	logsDir := filepath.Join(agentDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A molt event far above the tail window must NOT be seen; only the
	// recent tail is inspected, per the no-full-scan constraint.
	var b strings.Builder
	b.WriteString(`{"type":"psyche_molt","ts":1.0}` + "\n") // line 1 — outside the tail
	for i := 0; i < tailScanLines+50; i++ {
		fmt.Fprintf(&b, `{"type":"tool_call","ts":%d.0}`+"\n", 1000+i)
	}
	b.WriteString(`{"type":"psyche_molt","ts":99999.0}` + "\n") // near end — inside the tail
	if err := os.WriteFile(filepath.Join(logsDir, "events.jsonl"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	times := RecentRebuildTimes(agentDir, 10)
	if len(times) != 1 {
		t.Fatalf("expected only the in-tail molt, got %d", len(times))
	}
	if times[0].Unix() != 99999 {
		t.Fatalf("expected the recent molt (99999), got %d", times[0].Unix())
	}
}

func TestRecentRebuildTimesJSONLTailIncludesOldestLineInWindow(t *testing.T) {
	agentDir := t.TempDir()
	logsDir := filepath.Join(agentDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Exactly tailScanLines data lines plus a trailing newline. The first line
	// is still inside the tail window and must not be dropped because Split
	// sees a trailing empty segment.
	var b strings.Builder
	b.WriteString(`{"type":"psyche_molt","ts":777.0}` + "\n")
	for i := 1; i < tailScanLines; i++ {
		fmt.Fprintf(&b, `{"type":"tool_call","ts":%d.0}`+"\n", 1000+i)
	}
	if err := os.WriteFile(filepath.Join(logsDir, "events.jsonl"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	times := RecentRebuildTimes(agentDir, 10)
	if len(times) != 1 {
		t.Fatalf("expected the oldest in-window molt, got %d", len(times))
	}
	if times[0].Unix() != 777 {
		t.Fatalf("expected in-window molt (777), got %d", times[0].Unix())
	}
}

func TestRecentRebuildTimesHugePrefixLineDoesNotBlockTail(t *testing.T) {
	agentDir := t.TempDir()
	logsDir := filepath.Join(agentDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A >1MiB prefix line, far outside the last-1000-line window. A naive
	// bufio.Scanner with a 1MiB token cap chokes on this line and stops,
	// hiding the recent molt. The true tail reader must seek past it.
	var b strings.Builder
	huge := strings.Repeat("x", 2*1024*1024)
	fmt.Fprintf(&b, `{"type":"text_input","ts":1.0,"junk":"%s"}`+"\n", huge)
	for i := 0; i < tailScanLines+50; i++ {
		fmt.Fprintf(&b, `{"type":"tool_call","ts":%d.0}`+"\n", 1000+i)
	}
	b.WriteString(`{"type":"psyche_molt","ts":88888.0}` + "\n") // in the tail
	if err := os.WriteFile(filepath.Join(logsDir, "events.jsonl"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	times := RecentRebuildTimes(agentDir, 10)
	if len(times) != 1 {
		t.Fatalf("expected the in-tail molt despite huge prefix line, got %d", len(times))
	}
	if times[0].Unix() != 88888 {
		t.Fatalf("expected the recent molt (88888), got %d", times[0].Unix())
	}
}

func TestRecentRebuildTimesMissingLogsIsEmpty(t *testing.T) {
	times := RecentRebuildTimes(t.TempDir(), 10)
	if len(times) != 0 {
		t.Fatalf("expected no markers for missing logs, got %d", len(times))
	}
}

func TestRecentRebuildTimesPrefersSQLite(t *testing.T) {
	sqliteBin, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 not available")
	}
	agentDir := t.TempDir()
	logsDir := filepath.Join(agentDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// JSONL has a molt the sqlite sidecar does not — proves sqlite wins when present.
	if err := os.WriteFile(filepath.Join(logsDir, "events.jsonl"),
		[]byte(`{"type":"psyche_molt","ts":1.0}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	createSQL := `
		CREATE TABLE events (id INTEGER PRIMARY KEY AUTOINCREMENT, ts REAL NOT NULL, type TEXT NOT NULL, fields_json TEXT NOT NULL DEFAULT '{}');
		INSERT INTO events (ts, type) VALUES (5000.0, 'psyche_molt');
	`
	cmd := exec.Command(sqliteBin, filepath.Join(logsDir, "log.sqlite"), createSQL)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sqlite3 setup failed: %v\n%s", err, out)
	}

	times := RecentRebuildTimes(agentDir, 10)
	if len(times) != 1 {
		t.Fatalf("expected 1 rebuild time from sqlite, got %d", len(times))
	}
	if times[0].Unix() != 5000 {
		t.Fatalf("expected sqlite molt (5000), got %d", times[0].Unix())
	}
}
