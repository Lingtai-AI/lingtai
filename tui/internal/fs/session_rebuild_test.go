package fs

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestRebuildDeduplicatesMailAcrossRestart is a regression test for the
// "every relaunch duplicates all mail" bug caused by loadExisting+mailSeen
// keying on different strings than IngestMail. With rebuild-on-every-launch,
// running RebuildFromSources twice in a row must produce the same output.
func TestRebuildDeduplicatesMailAcrossRestart(t *testing.T) {
	tmp := t.TempDir()
	humanDir := filepath.Join(tmp, "human")
	orchDir := filepath.Join(tmp, "orch")
	inboxDir := filepath.Join(humanDir, "mailbox", "inbox")
	if err := os.MkdirAll(inboxDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(orchDir, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Write 3 mail messages to the inbox with raw From="xiake" and
	// identity.agent_name="徐霞客" — same shape as the real bug.
	mails := []MailMessage{
		{ID: "m1", From: "xiake", To: "human", ReceivedAt: "2026-04-07T15:36:44Z", Subject: "one", Message: "first", Identity: map[string]interface{}{"agent_name": "徐霞客"}},
		{ID: "m2", From: "xiake", To: "human", ReceivedAt: "2026-04-07T15:37:47Z", Subject: "two", Message: "second", Identity: map[string]interface{}{"agent_name": "徐霞客"}},
		{ID: "m3", From: "xiake", To: "human", ReceivedAt: "2026-04-07T15:38:59Z", Subject: "three", Message: "third", Identity: map[string]interface{}{"agent_name": "徐霞客"}},
	}
	for _, m := range mails {
		dir := filepath.Join(inboxDir, m.ID)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(m)
		if err := os.WriteFile(filepath.Join(dir, "message.json"), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// First rebuild (simulates fresh launch)
	sc1 := NewSessionCache(humanDir, tmp, MainAggregateWriter)
	cache1 := NewMailCache(humanDir).Refresh()
	sc1.RebuildFromSources(cache1, "human", orchDir, "xiake")
	firstLen := sc1.Len()
	if firstLen != len(mails) {
		t.Fatalf("first rebuild: expected %d entries, got %d", len(mails), firstLen)
	}

	// Second rebuild (simulates relaunch — the bug scenario)
	sc2 := NewSessionCache(humanDir, tmp, MainAggregateWriter)
	cache2 := NewMailCache(humanDir).Refresh()
	sc2.RebuildFromSources(cache2, "human", orchDir, "xiake")
	secondLen := sc2.Len()
	if secondLen != firstLen {
		t.Fatalf("second rebuild: expected %d entries (same as first), got %d — duplicates indicate the bug regressed", firstLen, secondLen)
	}

	// Verify the on-disk file has exactly the expected number of lines
	data, err := os.ReadFile(filepath.Join(humanDir, "logs", "session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lineCount := 0
	for _, b := range data {
		if b == '\n' {
			lineCount++
		}
	}
	if lineCount != len(mails) {
		t.Fatalf("session.jsonl: expected %d lines, got %d", len(mails), lineCount)
	}
}

// TestIngestMailWatermarkSkipsOldMail verifies that during a live session
// (not a rebuild), IngestMail skips mail older than the watermark.
func TestIngestMailWatermarkSkipsOldMail(t *testing.T) {
	tmp := t.TempDir()
	humanDir := filepath.Join(tmp, "human")
	inboxDir := filepath.Join(humanDir, "mailbox", "inbox")
	if err := os.MkdirAll(inboxDir, 0o755); err != nil {
		t.Fatal(err)
	}

	sc := NewSessionCache(humanDir, tmp, MainAggregateWriter)
	sc.lastMailTs = "2026-04-10T00:00:00Z" // simulate post-rebuild watermark

	cache := MailCache{
		seen: map[string]int{},
		Messages: []MailMessage{
			{From: "human", ReceivedAt: "2026-04-07T00:00:00Z", Message: "old"}, // below watermark — should skip
			{From: "human", ReceivedAt: "2026-04-11T00:00:00Z", Message: "new"}, // above watermark — should ingest
		},
	}

	sc.IngestMail(cache, "human", "", "orch")
	if got := sc.Len(); got != 1 {
		t.Fatalf("expected 1 entry (new only), got %d", got)
	}
	if sc.Entries()[0].Body != "new" {
		t.Fatalf("wrong entry admitted: body=%q", sc.Entries()[0].Body)
	}
	if sc.lastMailTs != "2026-04-11T00:00:00Z" {
		t.Fatalf("watermark not advanced: got %q", sc.lastMailTs)
	}
}

func TestRefreshDoesNotReingestSQLiteHistory(t *testing.T) {
	sqliteBin := requireSessionSQLite(t)
	tmp := t.TempDir()
	humanDir := filepath.Join(tmp, "human")
	// The apostrophe exercises SQL literal escaping for the canonical source path.
	orchDir := filepath.Join(tmp, "orch's root")
	logsDir := filepath.Join(orchDir, "logs")
	eventsPath := filepath.Join(logsDir, "events.jsonl")
	firstLine := `{"type":"text_input","ts":1.0,"text":"hello from sqlite"}` + "\n"
	writeSessionTestFile(t, eventsPath, firstLine)
	rootSource := canonicalSessionTestPath(t, eventsPath)
	createSessionSQLite(t, sqliteBin, orchDir,
		sessionSQLiteInsert(1.0, "text_input", "hello from sqlite", rootSource, 0, "agent_events", "agent"),
	)

	sc := NewSessionCache(humanDir, tmp, MainAggregateWriter)
	cache := NewMailCache(humanDir).Refresh()
	sc.afterRebuildIngest = func() {
		appendSessionTestFile(t, eventsPath,
			`{"type":"text_output","ts":2.0,"text":"appended during sqlite rebuild"}`+"\n")
	}
	sc.RebuildFromSources(cache, "human", orchDir, "orch")
	sc.afterRebuildIngest = nil
	assertSessionBodiesExactly(t, sc.Entries(), "hello from sqlite")

	sc.Refresh(cache, "human", orchDir, "orch")
	sc.Refresh(cache, "human", orchDir, "orch")
	assertSessionBodiesExactly(t, sc.Entries(), "hello from sqlite", "appended during sqlite rebuild")
}

func TestCanonicalJSONLRebuildIgnoresForeignSQLiteRows(t *testing.T) {
	sqliteBin := requireSessionSQLite(t)

	t.Run("daemon offset beyond root EOF cannot leak or reset replay", func(t *testing.T) {
		root, humanDir, orchDir := newSessionTestDirs(t)
		eventsPath := filepath.Join(orchDir, "logs", "events.jsonl")
		firstLine := `{"type":"text_input","ts":1.0,"text":"root indexed"}` + "\n"
		tailLine := `{"type":"text_output","ts":2.0,"text":"root tail"}` + "\n"
		writeSessionTestFile(t, eventsPath, firstLine+tailLine)
		rootSource := canonicalSessionTestPath(t, eventsPath)
		daemonSource := filepath.Join(orchDir, "daemons", "em-test", "logs", "events.jsonl")
		createSessionSQLite(t, sqliteBin, orchDir,
			sessionSQLiteInsert(1.0, "text_input", "root indexed", rootSource, 0, "agent_events", "agent")+
				sessionSQLiteInsert(1.5, "text_output", "daemon leaked", daemonSource, int64(len(firstLine+tailLine)+100), "daemon_events", "daemon"),
		)

		sc := NewSessionCache(humanDir, root, MainAggregateWriter)
		cache := NewMailCache(humanDir).Refresh()
		sc.RebuildFromSourcesInMemory(cache, "human", orchDir, "orch")
		assertSessionBodiesExactly(t, sc.Entries(), "root indexed", "root tail")
		sc.Refresh(cache, "human", orchDir, "orch")
		sc.Refresh(cache, "human", orchDir, "orch")
		assertSessionBodiesExactly(t, sc.Entries(), "root indexed", "root tail")
	})

	t.Run("foreign offset inside root JSONL cannot leak or skip tail", func(t *testing.T) {
		root, humanDir, orchDir := newSessionTestDirs(t)
		eventsPath := filepath.Join(orchDir, "logs", "events.jsonl")
		firstLine := `{"type":"text_input","ts":1.0,"text":"root indexed"}` + "\n"
		tailLine := `{"type":"text_output","ts":2.0,"text":"root must not be skipped"}` + "\n"
		writeSessionTestFile(t, eventsPath, firstLine+tailLine)
		rootSource := canonicalSessionTestPath(t, eventsPath)
		foreignSource := filepath.Join(orchDir, "elsewhere", "events.jsonl")
		foreignOffset := int64(len(firstLine) + 8)
		createSessionSQLite(t, sqliteBin, orchDir,
			sessionSQLiteInsert(1.0, "text_input", "root indexed", rootSource, 0, "agent_events", "agent")+
				sessionSQLiteInsert(1.5, "text_output", "foreign leaked", foreignSource, foreignOffset, "agent_events", "agent"),
		)

		sc := NewSessionCache(humanDir, root, MainAggregateWriter)
		cache := NewMailCache(humanDir).Refresh()
		sc.RebuildFromSourcesInMemory(cache, "human", orchDir, "orch")
		assertSessionBodiesExactly(t, sc.Entries(), "root indexed", "root must not be skipped")
		sc.Refresh(cache, "human", orchDir, "orch")
		sc.Refresh(cache, "human", orchDir, "orch")
		assertSessionBodiesExactly(t, sc.Entries(), "root indexed", "root must not be skipped")
	})
}

func TestCanonicalJSONLRebuildIncludesRowsMissingFromSQLite(t *testing.T) {
	sqliteBin := requireSessionSQLite(t)
	root, humanDir, orchDir := newSessionTestDirs(t)
	eventsPath := filepath.Join(orchDir, "logs", "events.jsonl")
	firstLine := `{"type":"text_input","ts":1.0,"text":"covered before query"}` + "\n"
	secondLine := `{"type":"text_output","ts":2.0,"text":"missing from sqlite"}` + "\n"
	writeSessionTestFile(t, eventsPath, firstLine+secondLine)
	rootSource := canonicalSessionTestPath(t, eventsPath)
	createSessionSQLite(t, sqliteBin, orchDir,
		sessionSQLiteInsert(1.0, "text_input", "covered before query", rootSource, 0, "agent_events", "agent"),
	)

	sc := NewSessionCache(humanDir, root, MainAggregateWriter)
	cache := NewMailCache(humanDir).Refresh()
	sc.RebuildFromSourcesInMemory(cache, "human", orchDir, "orch")
	assertSessionBodiesExactly(t, sc.Entries(), "covered before query", "missing from sqlite")

	sc.Refresh(cache, "human", orchDir, "orch")
	sc.Refresh(cache, "human", orchDir, "orch")
	assertSessionBodiesExactly(t, sc.Entries(), "covered before query", "missing from sqlite")
}

func TestSQLiteReplayRejectsInvalidNoNewlineBoundary(t *testing.T) {
	sqliteBin := requireSessionSQLite(t)
	root, humanDir, orchDir := newSessionTestDirs(t)
	eventsPath := filepath.Join(orchDir, "logs", "events.jsonl")
	line := `{"type":"text_input","ts":1.0,"text":"complete only after newline"}`
	writeSessionTestFile(t, eventsPath, line)
	rootSource := canonicalSessionTestPath(t, eventsPath)
	createSessionSQLite(t, sqliteBin, orchDir,
		sessionSQLiteInsert(1.0, "text_input", "complete only after newline", rootSource, 0, "agent_events", "agent"),
	)

	sc := NewSessionCache(humanDir, root, MainAggregateWriter)
	cache := NewMailCache(humanDir).Refresh()
	sc.RebuildFromSourcesInMemory(cache, "human", orchDir, "orch")
	assertSessionBodiesExactly(t, sc.Entries())

	appendSessionTestFile(t, eventsPath, "\n")
	sc.Refresh(cache, "human", orchDir, "orch")
	sc.Refresh(cache, "human", orchDir, "orch")
	assertSessionBodiesExactly(t, sc.Entries(), "complete only after newline")
}

func TestSQLiteReplayFallsBackWhenRootIdentityCannotBeProven(t *testing.T) {
	sqliteBin := requireSessionSQLite(t)
	root, humanDir, orchDir := newSessionTestDirs(t)
	eventsPath := filepath.Join(orchDir, "logs", "events.jsonl")
	writeSessionTestFile(t, eventsPath,
		`{"type":"text_input","ts":1.0,"text":"authoritative first"}`+"\n"+
			`{"type":"text_output","ts":2.0,"text":"authoritative second"}`+"\n",
	)
	oldSchema := `
		CREATE TABLE events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ts REAL NOT NULL,
			type TEXT NOT NULL,
			fields_json TEXT NOT NULL DEFAULT '{}',
			source_offset INTEGER
		);
		INSERT INTO events (ts, type, fields_json, source_offset)
		VALUES (1.0, 'text_input', '{"text":"unclassified sidecar"}', 0);
	`
	runSessionSQLiteSQL(t, sqliteBin, orchDir, oldSchema)

	sc := NewSessionCache(humanDir, root, MainAggregateWriter)
	cache := NewMailCache(humanDir).Refresh()
	sc.RebuildFromSourcesInMemory(cache, "human", orchDir, "orch")
	sc.Refresh(cache, "human", orchDir, "orch")
	assertSessionBodiesExactly(t, sc.Entries(), "authoritative first", "authoritative second")
}

func requireSessionSQLite(t *testing.T) string {
	t.Helper()
	sqliteBin, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 not available")
	}
	return sqliteBin
}

func createSessionSQLite(t *testing.T, sqliteBin, orchDir, inserts string) {
	t.Helper()
	createSQL := `
		CREATE TABLE events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ts REAL NOT NULL,
			type TEXT NOT NULL,
			agent_address TEXT,
			agent_name_snapshot TEXT,
			fields_json TEXT NOT NULL,
			source_file TEXT,
			source_offset INTEGER,
			source_line INTEGER,
			source_kind TEXT,
			scope TEXT,
			run_id TEXT,
			inserted_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
		);
	` + inserts
	runSessionSQLiteSQL(t, sqliteBin, orchDir, createSQL)
}

func runSessionSQLiteSQL(t *testing.T, sqliteBin, orchDir, sql string) {
	t.Helper()
	dbPath := filepath.Join(orchDir, "logs", "log.sqlite")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(sqliteBin, dbPath, sql)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sqlite3 setup failed: %v\n%s", err, out)
	}
}

func sessionSQLiteInsert(ts float64, eventType, body, sourceFile string, sourceOffset int64, sourceKind, scope string) string {
	fields := fmt.Sprintf(`{"text":%q}`, body)
	return fmt.Sprintf(
		"INSERT INTO events (ts, type, fields_json, source_file, source_offset, source_line, source_kind, scope) VALUES (%g, %s, %s, %s, %d, 1, %s, %s);\n",
		ts,
		sessionSQLLiteral(eventType),
		sessionSQLLiteral(fields),
		sessionSQLLiteral(sourceFile),
		sourceOffset,
		sessionSQLLiteral(sourceKind),
		sessionSQLLiteral(scope),
	)
}

func sessionSQLLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func canonicalSessionTestPath(t *testing.T, path string) string {
	t.Helper()
	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(resolved)
}

func assertSessionBodiesExactly(t *testing.T, entries []SessionEntry, want ...string) {
	t.Helper()
	got := make([]string, len(entries))
	for i := range entries {
		got[i] = entries[i].Body
	}
	if len(got) != len(want) {
		t.Fatalf("session bodies = %#v (%d entries), want %#v (%d entries)", got, len(got), want, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("session bodies = %#v, want %#v", got, want)
		}
	}
}
