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

func acceptExactCount(t *testing.T, m MailModel) MailModel {
	t.Helper()
	if !m.historyCountLoading || m.historyCountCache == nil {
		t.Fatal("initial content did not start one async exact-count task")
	}
	msg := m.historyCountCmd(m.historyCountCache, m.generation)()
	m, _ = m.Update(msg)
	if !m.historyCountLoaded || m.historyCountLoading {
		t.Fatal("exact-count result was not accepted")
	}
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
	if !m.historyCountLoading || m.historyCountLoaded {
		t.Fatal("content must paint while exact count remains in neutral loading state")
	}
	if strings.Contains(m.View(), "205 older") {
		t.Fatal("an estimate was presented as an exact older count")
	}
	m = acceptExactCount(t, m)
	if got := m.olderCount(); got != 205 {
		t.Fatalf("exact older count = %d, want 205", got)
	}
}

func TestCtrlULoadsOneConfiguredBatchPerRealKey(t *testing.T) {
	for _, tc := range []struct {
		page  int
		total int
		want  []int
	}{
		{page: 100, total: 250, want: []int{200, 250}},
		{page: 200, total: 450, want: []int{400, 450}},
	} {
		t.Run(fmt.Sprintf("page_%d", tc.page), func(t *testing.T) {
			m := NewMailModel(t.TempDir(), "human", t.TempDir(), buildWindowedAgentDir(t, tc.total), "agent", tc.page, "", "en", false, 0)
			m.verbose = verboseThinking
			m = acceptExactCount(t, installInitialWindow(t, m))
			originCountCache := m.historyCountCache
			for step, want := range tc.want {
				m.viewport.GotoTop()
				var cmd tea.Cmd
				m, cmd = m.Update(ctrlU())
				if cmd == nil || !m.olderLoadInFlight {
					t.Fatalf("step %d: real Ctrl+U did not start one older-page load", step+1)
				}
				msg, ok := cmd().(mailOlderPageMsg)
				if !ok {
					t.Fatalf("step %d: Ctrl+U command returned %T", step+1, cmd())
				}
				if msg.ingestWindow != (step+2)*tc.page {
					t.Fatalf("step %d content window = %d, want %d", step+1, msg.ingestWindow, (step+2)*tc.page)
				}
				m, _ = m.Update(msg)
				if got := len(m.visibleMessages()); got != want {
					t.Fatalf("step %d visible entries = %d, want %d", step+1, got, want)
				}
				if m.historyCountCache != originCountCache {
					t.Fatal("Ctrl+U replaced/restarted the activation's exact-count task")
				}
			}
			if m.cacheIsPartial() || m.olderCount() != 0 {
				t.Fatalf("repeated Ctrl+U did not reach complete history: partial=%v older=%d", m.cacheIsPartial(), m.olderCount())
			}
		})
	}
}

func TestHistoryCountMessageGenerationAndCacheIdentityGates(t *testing.T) {
	m := NewMailModel(t.TempDir(), "human", t.TempDir(), buildWindowedAgentDir(t, 250), "agent", 100, "", "en", false, 0)
	m.generation = 9
	m.verbose = verboseThinking
	m = installInitialWindow(t, m)
	valid := m.historyCountCmd(m.historyCountCache, m.generation)().(mailHistoryCountMsg)

	staleGeneration := valid
	staleGeneration.generation--
	got, _ := m.Update(staleGeneration)
	if got.historyCountLoaded {
		t.Fatal("stale-generation count result was accepted")
	}

	staleCache := valid
	staleCache.cache = NewMailModel(t.TempDir(), "human", t.TempDir(), "", "agent", 100, "", "en", false, 0).sessionCache
	got, _ = m.Update(staleCache)
	if got.historyCountLoaded {
		t.Fatal("wrong-cache count result was accepted")
	}

	got, _ = m.Update(valid)
	if !got.historyCountLoaded || got.olderCount() != 150 {
		t.Fatalf("valid exact count not accepted: loaded=%v older=%d", got.historyCountLoaded, got.olderCount())
	}
}

func TestHistoryCountUsesCurrentCacheTailDeltaAfterCtrlU(t *testing.T) {
	orchDir := buildWindowedAgentDir(t, 250)
	m := NewMailModel(t.TempDir(), "human", t.TempDir(), orchDir, "agent", 100, "", "en", false, 0)
	m.verbose = verboseThinking
	m = installInitialWindow(t, m)
	originCache := m.historyCountCache
	valid := m.historyCountCmd(originCache, m.generation)().(mailHistoryCountMsg)

	m.viewport.GotoTop()
	var cmd tea.Cmd
	m, cmd = m.Update(ctrlU())
	if cmd == nil {
		t.Fatal("Ctrl+U did not start the same-horizon replacement cache")
	}
	older := cmd().(mailOlderPageMsg)
	m, _ = m.Update(older)
	if m.sessionCache == originCache || m.sessionCache.HistoryCountIdentity() != valid.identity {
		t.Fatal("Ctrl+U did not install a distinct cache for the same count horizon")
	}

	appendMailGenerationEvent(t, orchDir, "tail after cache replacement")
	m.buildMessages()
	if got := originCache.HistoryStats().Detailed; got != 0 {
		t.Fatalf("detached origin cache received tail delta %d, want 0", got)
	}
	if got := m.sessionCache.HistoryStats().Detailed; got != 1 {
		t.Fatalf("current cache tail delta = %d, want 1", got)
	}

	m, _ = m.Update(valid)
	if !m.historyCountLoaded || m.historyStats.Detailed != 251 {
		t.Fatalf("accepted count did not use current-cache tail delta: loaded=%v stats=%+v", m.historyCountLoaded, m.historyStats)
	}
	if got := m.olderCount(); got != 51 {
		t.Fatalf("exact older count after replacement-cache tail = %d, want 51", got)
	}
}

func TestOlderPageNewHorizonSupersedesAsyncCountOnce(t *testing.T) {
	orchDir := buildWindowedAgentDir(t, 250)
	m := NewMailModel(t.TempDir(), "human", t.TempDir(), orchDir, "agent", 100, "", "en", false, 0)
	m.verbose = verboseThinking
	m = installInitialWindow(t, m)
	oldCache := m.historyCountCache
	oldIdentity := m.historyCountIdentity
	stale := m.historyCountCmd(oldCache, m.generation)().(mailHistoryCountMsg)

	appendWindowedIndexedEvent(t, orchDir, "new horizon before Ctrl+U")
	m.viewport.GotoTop()
	var cmd tea.Cmd
	m, cmd = m.Update(ctrlU())
	if cmd == nil {
		t.Fatal("Ctrl+U did not start newer-horizon content load")
	}
	older := cmd().(mailOlderPageMsg)
	m, cmd = m.Update(older)
	if m.historyCountIdentity == oldIdentity || m.historyCountCache != m.sessionCache {
		t.Fatal("new source horizon did not replace the async count identity/cache")
	}
	if !m.historyCountLoading || m.historyCountLoaded || cmd == nil {
		t.Fatalf("new source horizon did not schedule one neutral replacement count: loading=%v loaded=%v cmd=%v", m.historyCountLoading, m.historyCountLoaded, cmd != nil)
	}

	m, _ = m.Update(stale)
	if m.historyCountLoaded {
		t.Fatal("superseded source-horizon count was accepted")
	}
	newCount, ok := cmd().(mailHistoryCountMsg)
	if !ok {
		t.Fatalf("replacement count command returned %T", cmd())
	}
	m, _ = m.Update(newCount)
	if !m.historyCountLoaded || m.historyStats.Detailed != 251 {
		t.Fatalf("replacement source-horizon count not accepted exactly once: loaded=%v stats=%+v", m.historyCountLoaded, m.historyStats)
	}
}

func TestOlderCountViewMatrix(t *testing.T) {
	for _, tc := range []struct {
		name     string
		verbose  verboseLevel
		insights bool
		aux      int
		want     int
	}{
		{name: "normal", verbose: verboseOff, aux: 4, want: 2},
		{name: "thinking", verbose: verboseThinking, aux: 4, want: 7},
		{name: "extended", verbose: verboseExtended, aux: 4, want: 7},
		{name: "normal_insights", verbose: verboseOff, insights: true, aux: 6, want: 7},
		{name: "thinking_insights", verbose: verboseThinking, insights: true, aux: 6, want: 12},
		{name: "extended_insights", verbose: verboseExtended, insights: true, aux: 6, want: 12},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewMailModel(t.TempDir(), "human", t.TempDir(), "", "agent", 100, "", "en", tc.insights, 0)
			m.verbose = tc.verbose
			m.historyCountLoaded = true
			m.historyStats.Detailed = 5
			m.historyStats.Insights = 3
			m.auxiliaryMessages = tc.aux // mail + human inquiry; insight inquiries only when enabled
			m.messages = []ChatMessage{{Type: "mail"}, {Type: "insight", Source: "human"}}
			if got := m.olderCount(); got != tc.want {
				t.Fatalf("olderCount() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestUnsupportedMailPageSizeNormalizesAtConstruction(t *testing.T) {
	for _, unsupported := range []int{-1, 0, 99, 300, 2001, 999999} {
		m := NewMailModel(t.TempDir(), "human", t.TempDir(), "", "agent", unsupported, "", "en", false, 0)
		if m.pageSize != 200 || m.firstFrameWindow() != 200 {
			t.Fatalf("page size %d normalized to page=%d window=%d, want 200/200", unsupported, m.pageSize, m.firstFrameWindow())
		}
	}
}
