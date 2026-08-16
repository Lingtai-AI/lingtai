package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/internal/fs"
)

// buildWindowedAgentDir writes n renderable events plus canonical SQLite rows.
func buildWindowedAgentDir(t *testing.T, n int) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("requires sqlite3 binary (POSIX only)")
	}
	bin, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 not in PATH")
	}
	orchDir := t.TempDir()
	logsDir := filepath.Join(orchDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	eventsPath := filepath.Join(logsDir, "events.jsonl")
	var jsonl strings.Builder
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf(`{"type":"text_output","ts":%d,"text":"w%d"}`+"\n", i+1, i)
		jsonl.WriteString(lines[i])
	}
	if err := os.WriteFile(eventsPath, []byte(jsonl.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(eventsPath)
	if err != nil {
		t.Fatal(err)
	}
	source := strings.ReplaceAll(resolved, "'", "''")
	var sql strings.Builder
	sql.WriteString(`CREATE TABLE events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		ts REAL NOT NULL,
		type TEXT NOT NULL,
		fields_json TEXT NOT NULL DEFAULT '{}',
		source_file TEXT,
		source_offset INTEGER,
		source_kind TEXT,
		scope TEXT
	);` + "\n")
	offset := 0
	for i, line := range lines {
		sql.WriteString(fmt.Sprintf(
			"INSERT INTO events(ts,type,fields_json,source_file,source_offset,source_kind,scope) VALUES(%d,'text_output','{\"text\":\"w%d\"}','%s',%d,'agent_events','agent');\n",
			i+1, i, source, offset,
		))
		offset += len(line)
	}
	sql.WriteString("CREATE UNIQUE INDEX idx_events_source_offset ON events(source_file, source_offset) WHERE source_file IS NOT NULL AND source_offset IS NOT NULL;\n")
	if out, err := exec.Command(bin, filepath.Join(logsDir, "log.sqlite"), sql.String()).CombinedOutput(); err != nil {
		t.Fatalf("build sqlite: %v\n%s", err, out)
	}
	return orchDir
}

func appendWindowedIndexedEvent(t *testing.T, orchDir, text string) {
	t.Helper()
	bin, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 not in PATH")
	}
	eventsPath := filepath.Join(orchDir, "logs", "events.jsonl")
	info, err := os.Stat(eventsPath)
	if err != nil {
		t.Fatal(err)
	}
	line := fmt.Sprintf(`{"type":"text_output","ts":999999,"text":%q}`+"\n", text)
	f, err := os.OpenFile(eventsPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(line); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(eventsPath)
	if err != nil {
		t.Fatal(err)
	}
	source := strings.ReplaceAll(resolved, "'", "''")
	body := strings.ReplaceAll(text, "'", "''")
	sql := fmt.Sprintf(
		"INSERT INTO events(ts,type,fields_json,source_file,source_offset,source_kind,scope) VALUES(999999,'text_output','{\"text\":\"%s\"}','%s',%d,'agent_events','agent');",
		body, source, info.Size(),
	)
	if out, err := exec.Command(bin, filepath.Join(orchDir, "logs", "log.sqlite"), sql).CombinedOutput(); err != nil {
		t.Fatalf("append sqlite row: %v\n%s", err, out)
	}
}

func installInitialWindow(t *testing.T, m MailModel) MailModel {
	t.Helper()
	rm, ok := m.initialRebuild().(mailRefreshMsg)
	if !ok {
		t.Fatalf("initialRebuild returned %T", rm)
	}
	var cmd tea.Cmd
	m, cmd = m.Update(rm)
	if cmd == nil {
		t.Fatal("accepted initial content did not schedule post-frame work")
	}
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.viewport.GotoTop()
	return m
}

func ctrlU() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl} }

func TestInitialContentWindowEqualsConfiguredPageSize(t *testing.T) {
	orchDir := buildWindowedAgentDir(t, 405)
	m := NewMailModel(t.TempDir(), "human", t.TempDir(), orchDir, "agent", 200, "", "en", false, 0)
	m.verbose = verboseThinking
	rm := m.initialRebuild().(mailRefreshMsg)
	if got := rm.sessionCache.Len(); got != 200 {
		t.Fatalf("initial content cache = %d, want configured page size 200", got)
	}
	if rm.sessionCache.Complete() {
		t.Fatal("200-entry window over 405 entries must remain partial")
	}
	m = installInitialWindow(t, m)
	if got := len(m.visibleMessages()); got != 200 {
		t.Fatalf("initial visible batch = %d, want 200", got)
	}
	// Nothing counts the 205 entries left on disk: no banner, no async task, no
	// state. The window is painted and that is the whole of the page's history.
	if strings.Contains(m.View(), "ctrl+u to load") || m.bannerLineCount() != 0 {
		t.Fatalf("view announced an older/total message count:\n%s", m.View())
	}
}

// TestInitialContentWindowNeverExceedsRecentMessageLimit pins the hard cap
// against the largest configurable page size.
func TestInitialContentWindowNeverExceedsRecentMessageLimit(t *testing.T) {
	m := NewMailModel(t.TempDir(), "human", t.TempDir(), "", "agent", 2000, "", "en", false, 0)
	if m.pageSize != 2000 {
		t.Fatalf("fixture page size = %d, want the configured 2000", m.pageSize)
	}
	if got := m.contentWindow(); got != fs.RecentMessageLimit() {
		t.Fatalf("content window = %d, want the hard cap %d", got, fs.RecentMessageLimit())
	}
	m.messages = make([]ChatMessage, fs.RecentMessageLimit()+500)
	if got := len(m.visibleMessages()); got != fs.RecentMessageLimit() {
		t.Fatalf("visible window = %d, want the hard cap %d", got, fs.RecentMessageLimit())
	}
}

// TestCtrlURevealsLoadedHistoryWithoutLoadingMore is the replacement for the
// old disk-backed older-page paging: Ctrl+U only reveals entries already inside
// the loaded window, one configured page at a time, and never issues a command
// that would read further history.
func TestCtrlURevealsLoadedHistoryWithoutLoadingMore(t *testing.T) {
	const page, total = 100, 250
	m := NewMailModel(t.TempDir(), "human", t.TempDir(), buildWindowedAgentDir(t, total), "agent", page, "", "en", false, 0)
	m.verbose = verboseThinking
	m = installInitialWindow(t, m)

	loaded := len(m.messages)
	if loaded != page {
		t.Fatalf("initial loaded window = %d, want the configured page %d", loaded, page)
	}
	for step := range 3 {
		m.viewport.GotoTop()
		var cmd tea.Cmd
		m, cmd = m.Update(ctrlU())
		if cmd != nil {
			if _, isRefresh := cmd().(mailRefreshMsg); isRefresh {
				t.Fatalf("step %d: Ctrl+U started a history load; the loaded window is the whole history", step+1)
			}
		}
		if got := len(m.visibleMessages()); got > loaded {
			t.Fatalf("step %d: revealed %d entries, more than the %d loaded", step+1, got, loaded)
		}
		if len(m.messages) != loaded {
			t.Fatalf("step %d: loaded set grew to %d from %d", step+1, len(m.messages), loaded)
		}
	}
}

func TestUnsupportedMailPageSizeNormalizesAtConstruction(t *testing.T) {
	for _, unsupported := range []int{-1, 0, 99, 300, 2001, 999999} {
		m := NewMailModel(t.TempDir(), "human", t.TempDir(), "", "agent", unsupported, "", "en", false, 0)
		if m.pageSize != 200 || m.contentWindow() != 200 {
			t.Fatalf("page size %d normalized to page=%d window=%d, want 200/200", unsupported, m.pageSize, m.contentWindow())
		}
	}
}
