package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestFetchHomeTelemetryDoesNotSpawnSQLiteForMoltWindows is the issue #643
// contract: the once-per-second background home telemetry worker may read the
// canonical events and token ledgers, but it must not fork an external sqlite3
// process just to resolve the current molt-session boundary. Under Rosetta that
// fork is what writes the MallocStackLogging warning into the terminal.
func TestFetchHomeTelemetryDoesNotSpawnSQLiteForMoltWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sqlite3 shadow executable fixture is POSIX-only")
	}

	agentDir := filepath.Join(t.TempDir(), "agent")
	logsDir := filepath.Join(agentDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	previousMolt := time.Date(2026, 7, 15, 17, 0, 0, 0, time.UTC)
	latestMolt := previousMolt.Add(time.Hour)
	events := fmt.Sprintf(
		`{"type":"psyche_molt","ts":%d}`+"\n"+
			`{"type":"tool_call","ts":%d}`+"\n"+
			`{"type":"psyche_molt","ts":%d}`+"\n"+
			`{"type":"tool_call","ts":%d}`+"\n",
		previousMolt.Unix(), previousMolt.Add(time.Minute).Unix(),
		latestMolt.Unix(), latestMolt.Add(time.Minute).Unix(),
	)
	if err := os.WriteFile(filepath.Join(logsDir, "events.jsonl"), []byte(events), 0o644); err != nil {
		t.Fatal(err)
	}
	ledger := fmt.Sprintf(
		`{"ts":%q,"input":21,"output":3,"thinking":2,"cached":13}`+"\n",
		latestMolt.Add(time.Minute).Format(time.RFC3339),
	)
	if err := os.WriteFile(filepath.Join(logsDir, "token_ledger.jsonl"), []byte(ledger), 0o644); err != nil {
		t.Fatal(err)
	}

	// Current main stats log.sqlite before looking up sqlite3, so keep a sidecar
	// present and put a recording sqlite3 stub first on PATH. Its output is valid
	// for the old implementation; the assertion below therefore fails on the
	// forbidden fork itself, not on an environment or fixture error.
	if err := os.WriteFile(filepath.Join(logsDir, "log.sqlite"), []byte("fixture sidecar"), 0o644); err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	marker := filepath.Join(t.TempDir(), "sqlite3-invoked")
	sqliteStub := filepath.Join(binDir, "sqlite3")
	script := fmt.Sprintf("#!/bin/sh\nprintf 'invoked\\n' >> \"$LINGTAI_SQLITE_MARKER\"\nprintf '%d\\n%d\\n'\n", latestMolt.Unix(), previousMolt.Unix())
	if err := os.WriteFile(sqliteStub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("LINGTAI_SQLITE_MARKER", marker)

	msg, ok := (MailModel{orchestrator: agentDir}).fetchHomeTelemetry().(homeTelemetryMsg)
	if !ok {
		t.Fatalf("fetchHomeTelemetry returned %T, want homeTelemetryMsg", msg)
	}
	if got := msg.t.apiCalls; got != 1 {
		t.Fatalf("apiCalls = %d, want 1 from the current canonical molt window", got)
	}
	if got := msg.t.sessionTokens; got != 26 {
		t.Fatalf("sessionTokens = %d, want 26 from the current canonical molt window", got)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("home telemetry spawned sqlite3 to resolve molt windows; canonical events.jsonl must serve this high-frequency path in-process")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat sqlite3 marker: %v", err)
	}
}
