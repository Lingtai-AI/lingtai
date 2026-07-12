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

// buildWindowedAgentDir writes n text_output events to events.jsonl plus a
// covering SQLite index at the canonical root coordinate, so the indexed
// windowed first-frame path is exercised. Returns the orchDir.
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
	for i := 0; i < n; i++ {
		jsonl.WriteString(fmt.Sprintf(`{"type":"text_output","ts":%d,"text":"w%d"}`+"\n", i+1, i))
	}
	if err := os.WriteFile(eventsPath, []byte(jsonl.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(eventsPath)
	if err != nil {
		t.Fatal(err)
	}
	rootSrc := strings.ReplaceAll(resolved, "'", "''")

	var sql strings.Builder
	sql.WriteString(`CREATE TABLE events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		ts REAL NOT NULL,
		type TEXT NOT NULL,
		agent_address TEXT,
		fields_json TEXT NOT NULL DEFAULT '{}',
		source_file TEXT,
		source_offset INTEGER,
		source_line INTEGER,
		source_kind TEXT,
		scope TEXT,
		run_id TEXT,
		inserted_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
	);` + "\n")
	off := 0
	for i := 0; i < n; i++ {
		line := fmt.Sprintf(`{"type":"text_output","ts":%d,"text":"w%d"}`+"\n", i+1, i)
		sql.WriteString(fmt.Sprintf(
			"INSERT INTO events(ts,type,fields_json,source_file,source_offset,source_line,source_kind,scope) VALUES(%d,'text_output','{\"text\":\"w%d\"}','%s',%d,1,'agent_events','agent');\n",
			i+1, i, rootSrc, off,
		))
		off += len(line)
	}
	if out, err := exec.Command(bin, filepath.Join(logsDir, "log.sqlite"), sql.String()).CombinedOutput(); err != nil {
		t.Fatalf("build sqlite: %v\n%s", err, out)
	}
	return orchDir
}

// TestInitialRebuildWindowsToPageSize proves the first Mail frame loads only the
// newest `pageSize` session events (O(window)), not the whole history, and that
// the resulting partial cache is not persisted as a complete session.jsonl.
func TestInitialRebuildWindowsToPageSize(t *testing.T) {
	orchDir := buildWindowedAgentDir(t, 10)
	humanDir := t.TempDir()
	projectDir := t.TempDir()

	m := NewMailModel(humanDir, "human", projectDir, orchDir, "agent", 3, "", "en", false, 0)
	m.verbose = verboseThinking

	msg := m.initialRebuild()
	rm, ok := msg.(mailRefreshMsg)
	if !ok {
		t.Fatalf("initialRebuild returned %T", msg)
	}
	if got := rm.sessionCache.Len(); got != 3 {
		t.Fatalf("windowed first frame loaded %d entries, want 3 (newest window)", got)
	}
	if rm.sessionCache.Complete() {
		t.Fatal("windowed first frame over a larger history must be partial (Complete()==false)")
	}

	// Accept, then run the post-frame persistence phase. A partial cache must NOT
	// write session.jsonl.
	updated, persistCmd := m.Update(msg)
	if persistCmd == nil {
		t.Fatal("accepted initial rebuild returned no persist command")
	}
	persistMsg := persistCmd()
	if _, ok := persistMsg.(mailPersistMsg); !ok {
		t.Fatalf("persist command produced %T, want mailPersistMsg", persistMsg)
	}
	updated, _ = updated.Update(persistMsg)

	if _, err := os.Stat(filepath.Join(humanDir, "logs", "session.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("partial windowed cache must not persist session.jsonl; stat err = %v", err)
	}

	// The newest events are the ones shown.
	found := 0
	for _, cm := range updated.messages {
		if cm.Type == "text_output" && (strings.Contains(cm.Body, "w7") || strings.Contains(cm.Body, "w8") || strings.Contains(cm.Body, "w9")) {
			found++
		}
	}
	if found != 3 {
		t.Fatalf("expected newest 3 events (w7,w8,w9) in messages, found %d", found)
	}
}

// TestOlderPageLoadGrowsWindowAsyncAndGenerationGated proves the upward-navigation
// contract: with a partial (windowed) cache, an older-page load asynchronously
// rebuilds with a larger window, is generation-gated, and reveals older events.
func TestOlderPageLoadGrowsWindowAsyncAndGenerationGated(t *testing.T) {
	orchDir := buildWindowedAgentDir(t, 10)
	humanDir := t.TempDir()
	projectDir := t.TempDir()

	m := NewMailModel(humanDir, "human", projectDir, orchDir, "agent", 3, "", "en", false, 0)
	m.generation = 7
	m.verbose = verboseThinking

	// Land the initial windowed frame (newest 3: w7,w8,w9).
	rm := m.initialRebuild().(mailRefreshMsg)
	rm.generation = 7
	m, _ = m.Update(rm)
	if !m.hasMoreOlder() {
		t.Fatal("after a partial windowed first frame, hasMoreOlder() must be true so Ctrl+U is offered")
	}

	// Request an older page. It must return an async command tagged with the
	// current generation, not mutate the installed cache synchronously.
	m, cmd := m.requestOlderPage()
	if cmd == nil {
		t.Fatal("requestOlderPage returned no async command")
	}
	if !m.olderLoadInFlight {
		t.Fatal("requestOlderPage must mark an older load in flight")
	}
	raw := cmd()
	older, ok := raw.(mailOlderPageMsg)
	if !ok {
		t.Fatalf("older-page command produced %T, want mailOlderPageMsg", raw)
	}
	if older.generation != 7 {
		t.Fatalf("older-page msg generation = %d, want 7", older.generation)
	}

	// A stale (old-generation) older page must be ignored.
	stale := older
	stale.generation = 6
	before := m.sessionCache.Len()
	m2, staleCmd := m.Update(stale)
	if staleCmd != nil {
		t.Fatal("stale older-page must not reschedule")
	}
	if m2.sessionCache.Len() != before {
		t.Fatal("stale older-page must not grow the installed cache")
	}

	// Accept the matching older page: the window grows to newest 6 (w4..w9).
	m, _ = m.Update(older)
	if m.olderLoadInFlight {
		t.Fatal("accepted older page must clear in-flight")
	}
	got := 0
	for _, cm := range m.messages {
		if cm.Type == "text_output" && strings.HasPrefix(cm.Body, "w") {
			got++
		}
	}
	if got != 6 {
		t.Fatalf("after one older page the cache should hold 6 events (w4..w9), got %d", got)
	}
	// And they must be visible (render window grew in lockstep).
	visibleW := 0
	for _, cm := range m.visibleMessages() {
		if strings.HasPrefix(cm.Body, "w") {
			visibleW++
		}
	}
	if visibleW != 6 {
		t.Fatalf("older page loaded but not revealed: %d visible, want 6", visibleW)
	}
}

// TestOlderPageLoadReachingHistoryEndBecomesComplete proves that once the growing
// window covers the whole history, the cache becomes complete and persists.
func TestOlderPageLoadReachingHistoryEndBecomesComplete(t *testing.T) {
	orchDir := buildWindowedAgentDir(t, 5)
	humanDir := t.TempDir()
	projectDir := t.TempDir()

	m := NewMailModel(humanDir, "human", projectDir, orchDir, "agent", 3, "", "en", false, 0)
	m.generation = 1
	m.verbose = verboseThinking
	rm := m.initialRebuild().(mailRefreshMsg)
	rm.generation = 1
	m, _ = m.Update(rm)

	// One older page grows the window from 3 to 6, which exceeds the 5 events →
	// complete.
	m, cmd := m.requestOlderPage()
	older := cmd().(mailOlderPageMsg)
	m, persistCmd := m.Update(older)
	if !m.sessionCache.Complete() {
		t.Fatal("older page covering the whole history must yield a complete cache")
	}
	if persistCmd == nil {
		t.Fatal("a newly-complete cache should schedule persistence")
	}
	// The scheduled persist must pass the generation + identity gate and actually
	// write the now-complete derived session.jsonl.
	persistMsg := persistCmd()
	m.Update(persistMsg)
	if _, err := os.Stat(filepath.Join(humanDir, "logs", "session.jsonl")); err != nil {
		t.Fatalf("newly-complete older-page cache must persist session.jsonl: %v", err)
	}
	// With the whole history now loaded and revealed, no older history remains.
	if m.cacheIsPartial() {
		t.Fatal("cache should no longer be partial after covering the whole history")
	}
}

// buildLegacyGroupAgentDir writes a legacy event stream (no explicit
// api_call_id) whose grouping is derived only from a hidden llm_response
// boundary marker, and indexes it in log.sqlite. The stream is:
//
//	1 llm_response            (group A header)
//	2 tool_call  read({})     (group A)
//	3 tool_result read → ok   (group A)
//	4 tool_call  grep({})     (group A, same api round)
//	5 tool_result grep → ok   (group A)
//
// A newest-3 window cuts to rows 3,4,5 — dropping the llm_response header. If the
// window does not reach back to that header, rows 3→4 (tool_result → tool_call)
// fall back to the legacy "new call after result" separator heuristic and render
// a SPURIOUS group separator that full-history rendering (where both share the
// derived group id) never shows.
func buildLegacyGroupAgentDir(t *testing.T) string {
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

	type ev struct {
		typ    string
		fields string
	}
	rows := []ev{
		{"llm_response", `{"model":"m"}`},
		{"tool_call", `{"tool_name":"read","tool_args":"{}"}`},
		{"tool_result", `{"tool_name":"read","status":"ok"}`},
		{"tool_call", `{"tool_name":"grep","tool_args":"{}"}`},
		{"tool_result", `{"tool_name":"grep","status":"ok"}`},
	}

	var jsonl strings.Builder
	lines := make([]string, len(rows))
	for i, r := range rows {
		// Full on-disk JSONL record: type + ts + the row-specific fields.
		inner := r.fields[1 : len(r.fields)-1] // strip the outer braces
		line := fmt.Sprintf(`{"type":"%s","ts":%d,%s}`+"\n", r.typ, i+1, inner)
		lines[i] = line
		jsonl.WriteString(line)
	}
	if err := os.WriteFile(eventsPath, []byte(jsonl.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(eventsPath)
	if err != nil {
		t.Fatal(err)
	}
	rootSrc := strings.ReplaceAll(resolved, "'", "''")

	var sql strings.Builder
	sql.WriteString(`CREATE TABLE events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		ts REAL NOT NULL,
		type TEXT NOT NULL,
		agent_address TEXT,
		fields_json TEXT NOT NULL DEFAULT '{}',
		source_file TEXT,
		source_offset INTEGER,
		source_line INTEGER,
		source_kind TEXT,
		scope TEXT,
		run_id TEXT,
		inserted_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
	);` + "\n")
	off := 0
	for i, r := range rows {
		fields := strings.ReplaceAll(r.fields, "'", "''")
		sql.WriteString(fmt.Sprintf(
			"INSERT INTO events(ts,type,fields_json,source_file,source_offset,source_line,source_kind,scope) VALUES(%d,'%s','%s','%s',%d,1,'agent_events','agent');\n",
			i+1, r.typ, fields, rootSrc, off,
		))
		off += len(lines[i])
	}
	if out, err := exec.Command(bin, filepath.Join(logsDir, "log.sqlite"), sql.String()).CombinedOutput(); err != nil {
		t.Fatalf("build sqlite: %v\n%s", err, out)
	}
	return orchDir
}

// TestWindowedLegacyGroupBoundaryPreservesGrouping proves finding C: a newest-N
// window that cuts INTO a legacy api-call group (dropping its hidden llm_response
// boundary marker) must still reach back to that marker so the group's tool
// entries derive the same group id — no spurious separator that full-history
// rendering never shows.
func TestWindowedLegacyGroupBoundaryPreservesGrouping(t *testing.T) {
	orchDir := buildLegacyGroupAgentDir(t)
	humanDir := t.TempDir()
	projectDir := t.TempDir()

	// pageSize 3 → newest-3 window cuts to rows 3,4,5 (header row 1 excluded).
	m := NewMailModel(humanDir, "human", projectDir, orchDir, "agent", 3, "", "en", false, 0)
	m.width = 100
	m.verbose = verboseThinking

	rm := m.initialRebuild().(mailRefreshMsg)
	m, _ = m.Update(rm)

	out := m.renderMessages(m.visibleMessages())
	if !strings.Contains(out, "read → ok") || !strings.Contains(out, "grep({})") {
		t.Fatalf("windowed render missing expected tool bodies: %q", out)
	}
	if strings.Contains(out, "┈") {
		t.Fatalf("newest-N window cutting into a legacy group rendered a SPURIOUS "+
			"api-call-group separator; the window must reach back to the llm_response "+
			"boundary so the entries stay grouped. got: %q", out)
	}
}

// buildTailIndexedAgentDir writes `prefix`+`tail` padded text_output events to
// events.jsonl but indexes ONLY the tail suffix in log.sqlite (offsets pushed
// past 4096 so the pre-index prefix is real). This is the "partially indexed
// JSONL prefix" shape used to prove Ctrl+U converges to complete rather than
// looping partial forever.
func buildTailIndexedAgentDir(t *testing.T, prefix, tail int) string {
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
	pad := strings.Repeat("x", 600)
	total := prefix + tail

	var jsonl strings.Builder
	offsets := make([]int, total)
	off := 0
	lines := make([]string, total)
	for i := 0; i < total; i++ {
		offsets[i] = off
		line := fmt.Sprintf(`{"type":"text_output","ts":%d,"text":"w%d|%s"}`+"\n", i+1, i, pad)
		lines[i] = line
		jsonl.WriteString(line)
		off += len(line)
	}
	if offsets[prefix] <= 4096 {
		t.Fatalf("fixture prefix too small: first indexed offset %d <= 4096", offsets[prefix])
	}
	if err := os.WriteFile(eventsPath, []byte(jsonl.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(eventsPath)
	if err != nil {
		t.Fatal(err)
	}
	rootSrc := strings.ReplaceAll(resolved, "'", "''")

	var sql strings.Builder
	sql.WriteString(`CREATE TABLE events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		ts REAL NOT NULL,
		type TEXT NOT NULL,
		agent_address TEXT,
		fields_json TEXT NOT NULL DEFAULT '{}',
		source_file TEXT,
		source_offset INTEGER,
		source_line INTEGER,
		source_kind TEXT,
		scope TEXT,
		run_id TEXT,
		inserted_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
	);` + "\n")
	for i := prefix; i < total; i++ {
		body := fmt.Sprintf("w%d|%s", i, pad)
		fields := strings.ReplaceAll(fmt.Sprintf(`{"text":"%s"}`, body), "'", "''")
		sql.WriteString(fmt.Sprintf(
			"INSERT INTO events(ts,type,fields_json,source_file,source_offset,source_line,source_kind,scope) VALUES(%d,'text_output','%s','%s',%d,1,'agent_events','agent');\n",
			i+1, fields, rootSrc, offsets[i],
		))
	}
	if out, err := exec.Command(bin, filepath.Join(logsDir, "log.sqlite"), sql.String()).CombinedOutput(); err != nil {
		t.Fatalf("build sqlite: %v\n%s", err, out)
	}
	return orchDir
}

// TestOlderPageLoadOverTailIndexConvergesToComplete proves finding D's
// no-infinite-partial guarantee: with an index covering only a tail suffix of
// events.jsonl (the pre-index prefix reachable only via JSONL fallback), repeated
// Ctrl+U older-page loads must make progress every step and eventually reach a
// COMPLETE cache — never spin forever on a partial cache.
func TestOlderPageLoadOverTailIndexConvergesToComplete(t *testing.T) {
	// 8 un-indexed prefix events + 4 indexed tail events = 12 total.
	orchDir := buildTailIndexedAgentDir(t, 8, 4)
	humanDir := t.TempDir()
	projectDir := t.TempDir()

	m := NewMailModel(humanDir, "human", projectDir, orchDir, "agent", 3, "", "en", false, 0)
	m.generation = 1
	m.verbose = verboseThinking
	rm := m.initialRebuild().(mailRefreshMsg)
	rm.generation = 1
	m, _ = m.Update(rm)

	if !m.cacheIsPartial() {
		t.Fatal("newest-window over a larger tail-indexed history must start partial")
	}

	// Drive Ctrl+U older-page loads until complete, bounding the iterations so a
	// non-converging (infinite partial) implementation FAILS loudly rather than
	// hanging. 12 events / pageSize 3 → at most ~5 pages.
	const maxSteps = 8
	prevLoaded := m.sessionCache.Len()
	steps := 0
	for m.cacheIsPartial() {
		steps++
		if steps > maxSteps {
			t.Fatalf("Ctrl+U did not converge to a complete cache after %d steps — infinite partial", maxSteps)
		}
		var cmd tea.Cmd
		m, cmd = m.requestOlderPage()
		if cmd == nil {
			t.Fatalf("step %d: partial cache offered no older-page load (stuck partial)", steps)
		}
		older := cmd().(mailOlderPageMsg)
		m, _ = m.Update(older)
		if got := m.sessionCache.Len(); got <= prevLoaded && !m.sessionCache.Complete() {
			t.Fatalf("step %d: older page made no progress (loaded %d, was %d) yet still partial", steps, got, prevLoaded)
		}
		prevLoaded = m.sessionCache.Len()
	}

	if !m.sessionCache.Complete() {
		t.Fatal("loop exited but cache is not complete")
	}
	// All 12 events (prefix + tail) are now loaded.
	got := 0
	for _, cm := range m.messages {
		if cm.Type == "text_output" && strings.HasPrefix(cm.Body, "w") {
			got++
		}
	}
	if got != 12 {
		t.Fatalf("complete cache should hold all 12 events, got %d", got)
	}
}

// TestInitialRebuildInfinitePageSizeIsComplete proves that the "infinite"
// mail_page_size setting still performs a complete rebuild that persists.
func TestInitialRebuildInfinitePageSizeIsComplete(t *testing.T) {
	orchDir := buildWindowedAgentDir(t, 5)
	humanDir := t.TempDir()
	projectDir := t.TempDir()

	// pageSize 0 is normalized to unlimitedPageSize by NewMailModel (infinite).
	m := NewMailModel(humanDir, "human", projectDir, orchDir, "agent", 0, "", "en", false, 0)
	m.verbose = verboseThinking

	msg := m.initialRebuild()
	rm := msg.(mailRefreshMsg)
	if got := rm.sessionCache.Len(); got != 5 {
		t.Fatalf("infinite page size loaded %d entries, want all 5", got)
	}
	if !rm.sessionCache.Complete() {
		t.Fatal("infinite page size must yield a complete cache")
	}

	updated, persistCmd := m.Update(msg)
	persistMsg := persistCmd()
	updated.Update(persistMsg)

	if _, err := os.Stat(filepath.Join(humanDir, "logs", "session.jsonl")); err != nil {
		t.Fatalf("complete cache must persist session.jsonl: %v", err)
	}
}
