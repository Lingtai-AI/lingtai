package fs

import (
	"os"
	"path/filepath"
	"testing"
)

// buildWindowSQLiteEvents writes N text_output events to events.jsonl and an
// index that covers them at the canonical root coordinate. It returns the
// orchDir. Bodies are "e0".."e{N-1}" so ordering is checkable.
func buildWindowSQLiteEvents(t *testing.T, sqliteBin, orchDir string, n int) {
	t.Helper()
	eventsPath := filepath.Join(orchDir, "logs", "events.jsonl")
	var content string
	for i := 0; i < n; i++ {
		content += sessionEventJSONL(float64(i+1), "text_output", bodyForIndex(i))
	}
	writeSessionTestFile(t, eventsPath, content)
	rootSource := canonicalSessionTestPath(t, eventsPath)
	inserts := ""
	off := int64(0)
	for i := 0; i < n; i++ {
		line := sessionEventJSONL(float64(i+1), "text_output", bodyForIndex(i))
		inserts += sessionSQLiteInsert(float64(i+1), "text_output", bodyForIndex(i), rootSource, off, "agent_events", "agent")
		off += int64(len(line))
	}
	createSessionSQLite(t, sqliteBin, orchDir, inserts)
}

func bodyForIndex(i int) string {
	return "e" + itoa(i)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}

// sessionEventJSONL mirrors the exact byte shape of the rows the sqlite fixture
// claims, so JSONL offsets line up with the index source_offset values.
func sessionEventJSONL(ts float64, typ, text string) string {
	// Match the sessionSQLiteInsert body: fields_json is {"text":"..."} but the
	// on-disk JSONL row must be a full event record. Keep it stable and short.
	return `{"type":"` + typ + `","ts":` + ftoa(ts) + `,"text":"` + text + `"}` + "\n"
}

func ftoa(f float64) string {
	// integer-valued timestamps only in these fixtures
	return itoa(int(f))
}

// paddedBody returns a body that begins with the "eN" marker followed by filler
// so events.jsonl rows are large enough to push the pre-index prefix past the
// StartsAtBeginning 4096-byte threshold. The marker is recoverable with
// markerOf so ordering stays checkable.
func paddedBody(i int) string {
	pad := ""
	for len(pad) < 600 {
		pad += "x"
	}
	return "e" + itoa(i) + "|" + pad
}

func markerOf(body string) string {
	if idx := indexByteString(body, '|'); idx >= 0 {
		return body[:idx]
	}
	return body
}

func indexByteString(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func assertSessionMarkersExactly(t *testing.T, entries []SessionEntry, want ...string) {
	t.Helper()
	got := make([]string, len(entries))
	for i := range entries {
		got[i] = markerOf(entries[i].Body)
	}
	if len(got) != len(want) {
		t.Fatalf("session markers = %#v (%d), want %#v (%d)", got, len(got), want, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("session markers = %#v, want %#v", got, want)
		}
	}
}

// buildTailIndexedEvents writes `prefix`+`tail` padded text_output events to
// events.jsonl but indexes ONLY the tail suffix in log.sqlite. The prefix rows
// (offsets [0, MinOffset)) exist on disk yet are absent from the index, and the
// padding guarantees MinOffset > 4096 so coverage.StartsAtBeginning() is false —
// exactly the "partially indexed JSONL prefix" shape from finding B.
func buildTailIndexedEvents(t *testing.T, sqliteBin, orchDir string, prefix, tail int) {
	t.Helper()
	eventsPath := filepath.Join(orchDir, "logs", "events.jsonl")
	total := prefix + tail
	var content string
	offsets := make([]int64, total)
	off := int64(0)
	for i := 0; i < total; i++ {
		offsets[i] = off
		line := sessionEventJSONL(float64(i+1), "text_output", paddedBody(i))
		content += line
		off += int64(len(line))
	}
	writeSessionTestFile(t, eventsPath, content)
	if offsets[prefix] <= 4096 {
		t.Fatalf("fixture prefix too small: first indexed offset %d must exceed 4096", offsets[prefix])
	}
	rootSource := canonicalSessionTestPath(t, eventsPath)
	inserts := ""
	for i := prefix; i < total; i++ {
		inserts += sessionSQLiteInsert(float64(i+1), "text_output", paddedBody(i), rootSource, offsets[i], "agent_events", "agent")
	}
	createSessionSQLite(t, sqliteBin, orchDir, inserts)
}

// TestWindowedRebuildTailIndexReachesJSONLPrefixOnOlderLoad proves finding B: with
// an index covering only a tail suffix of events.jsonl, the newest window is
// correct and partial; a larger explicit older request reaches the un-indexed
// JSONL prefix without duplicates or an order break; and a request at least as
// large as the whole history becomes complete and persistable.
func TestWindowedRebuildTailIndexReachesJSONLPrefixOnOlderLoad(t *testing.T) {
	sqliteBin := requireSessionSQLite(t)
	root, humanDir, orchDir := newSessionTestDirs(t)
	// 8 prefix events (un-indexed, on disk only) + 4 tail events (indexed).
	buildTailIndexedEvents(t, sqliteBin, orchDir, 8, 4)
	cache := NewMailCache(humanDir).Refresh()

	// (1) Initial newest window of 3 → newest 3 events (e9,e10,e11), partial.
	sc := NewSessionCache(humanDir, root)
	sc.RebuildFromSourcesWindowedInMemory(cache, "human", orchDir, "orch", 3)
	assertSessionMarkersExactly(t, sc.Entries(), "e9", "e10", "e11")
	if sc.Complete() {
		t.Fatal("newest-window over a larger history must be partial")
	}

	// (2) A larger explicit older request (window 8) exhausts the 4 indexed tail
	// rows and must reach the un-indexed JSONL prefix — no duplicates, no order
	// break. Newest 8 events are e4..e11.
	sc2 := NewSessionCache(humanDir, root)
	sc2.RebuildFromSourcesWindowedInMemory(cache, "human", orchDir, "orch", 8)
	assertSessionMarkersExactly(t, sc2.Entries(),
		"e4", "e5", "e6", "e7", "e8", "e9", "e10", "e11")

	// (3) A window >= whole history (12) reaches the whole prefix → complete and
	// persistable.
	sc3 := NewSessionCache(humanDir, root)
	sc3.RebuildFromSourcesWindowedInMemory(cache, "human", orchDir, "orch", 12)
	assertSessionMarkersExactly(t, sc3.Entries(),
		"e0", "e1", "e2", "e3", "e4", "e5", "e6", "e7", "e8", "e9", "e10", "e11")
	if !sc3.Complete() {
		t.Fatal("a window covering the whole history (including the JSONL prefix) must be Complete()")
	}
	sc3.Persist()
	if _, err := os.Stat(filepath.Join(humanDir, "logs", "session.jsonl")); err != nil {
		t.Fatalf("complete cache reaching the prefix must persist: %v", err)
	}
}

// TestWindowedExactEqualWindowResolvesToCompleteOnNextRequest proves finding D:
// a window that exactly equals the (fully indexed) history is conservatively
// reported partial (we cannot cheaply tell that no older row exists once the
// window fills), but the NEXT larger request must resolve to complete — never an
// endlessly-partial cache.
func TestWindowedExactEqualWindowResolvesToCompleteOnNextRequest(t *testing.T) {
	sqliteBin := requireSessionSQLite(t)
	root, humanDir, orchDir := newSessionTestDirs(t)
	buildWindowSQLiteEvents(t, sqliteBin, orchDir, 4) // history is exactly 4 events
	cache := NewMailCache(humanDir).Refresh()

	// Window exactly equal to history: fills to capacity → conservatively partial.
	scEqual := NewSessionCache(humanDir, root)
	scEqual.RebuildFromSourcesWindowedInMemory(cache, "human", orchDir, "orch", 4)
	assertSessionBodiesExactly(t, scEqual.Entries(), "e0", "e1", "e2", "e3")
	if scEqual.Complete() {
		t.Fatal("a window that exactly fills is conservatively partial (cannot prove no older row)")
	}

	// The next larger request (one page bigger) must resolve to complete.
	scNext := NewSessionCache(humanDir, root)
	scNext.RebuildFromSourcesWindowedInMemory(cache, "human", orchDir, "orch", 8)
	assertSessionBodiesExactly(t, scNext.Entries(), "e0", "e1", "e2", "e3")
	if !scNext.Complete() {
		t.Fatal("the request after an exact-equal window must resolve to complete — no endless partial")
	}
}

// TestWindowedRebuildKeepsOnlyNewestEventsButFullEOFOffset proves the O(window)
// first-frame contract: a windowed rebuild ingests only the newest `window`
// events, yet leaves eventsOff at the true EOF boundary so a later Refresh
// resumes from EOF (no re-ingest of the excluded older rows, no duplicates).
func TestWindowedRebuildKeepsOnlyNewestEventsButFullEOFOffset(t *testing.T) {
	sqliteBin := requireSessionSQLite(t)
	root, humanDir, orchDir := newSessionTestDirs(t)
	buildWindowSQLiteEvents(t, sqliteBin, orchDir, 10)

	sc := NewSessionCache(humanDir, root)
	cache := NewMailCache(humanDir).Refresh()
	sc.RebuildFromSourcesWindowedInMemory(cache, "human", orchDir, "orch", 3)

	// Only the newest 3 events (e7,e8,e9) are loaded.
	assertSessionBodiesExactly(t, sc.Entries(), "e7", "e8", "e9")
	if sc.Complete() {
		t.Fatal("windowed rebuild that truncated history must report Complete()==false")
	}

	// A new event lands; Refresh must pick up ONLY the new tail, resuming from
	// EOF — never re-ingesting the excluded older window (e0..e6) as duplicates.
	eventsPath := filepath.Join(orchDir, "logs", "events.jsonl")
	appendSessionTestFile(t, eventsPath, sessionEventJSONL(11.0, "text_output", "e10"))
	sc.Refresh(cache, "human", orchDir, "orch")
	assertSessionBodiesExactly(t, sc.Entries(), "e7", "e8", "e9", "e10")
}

// TestWindowedRebuildLargerThanHistoryIsComplete proves that when the window is
// at least as large as the whole event history, the cache is Complete() and may
// be persisted like an ordinary full rebuild.
func TestWindowedRebuildLargerThanHistoryIsComplete(t *testing.T) {
	sqliteBin := requireSessionSQLite(t)
	root, humanDir, orchDir := newSessionTestDirs(t)
	buildWindowSQLiteEvents(t, sqliteBin, orchDir, 4)

	sc := NewSessionCache(humanDir, root)
	cache := NewMailCache(humanDir).Refresh()
	sc.RebuildFromSourcesWindowedInMemory(cache, "human", orchDir, "orch", 2000)

	assertSessionBodiesExactly(t, sc.Entries(), "e0", "e1", "e2", "e3")
	if !sc.Complete() {
		t.Fatal("window >= history must report Complete()==true")
	}
}

// TestPersistRefusesPartialWindowedCache proves persistence safety: a partial
// (windowed) cache must NOT rewrite human/logs/session.jsonl as if complete.
func TestPersistRefusesPartialWindowedCache(t *testing.T) {
	sqliteBin := requireSessionSQLite(t)
	root, humanDir, orchDir := newSessionTestDirs(t)
	buildWindowSQLiteEvents(t, sqliteBin, orchDir, 10)

	sessionPath := filepath.Join(humanDir, "logs", "session.jsonl")

	sc := NewSessionCache(humanDir, root)
	cache := NewMailCache(humanDir).Refresh()
	sc.RebuildFromSourcesWindowedInMemory(cache, "human", orchDir, "orch", 3)
	sc.Persist()

	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Fatalf("partial windowed Persist must not create/overwrite session.jsonl; stat err = %v", err)
	}
}

// TestFreshCacheRefreshPersistsAsComplete proves finding A: a fresh cache built
// by NewSessionCache followed by a full Refresh (no windowed rebuild) represents
// complete-from-zero state, so Persist must still write session.jsonl. The
// windowing change added a `complete` gate on Persist; if `complete` zero-valued
// false, this ordinary complete-from-zero path would silently no-op and never
// write the operator's derived replay file.
func TestFreshCacheRefreshPersistsAsComplete(t *testing.T) {
	root, humanDir, orchDir := newSessionTestDirs(t)
	eventsPath := filepath.Join(orchDir, "logs", "events.jsonl")
	writeSessionTestFile(t, eventsPath,
		`{"ts":1781300001,"type":"text_output","text":"only"}`+"\n")

	sc := NewSessionCache(humanDir, root)
	cache := NewMailCache(humanDir).Refresh()
	// A plain Refresh reads the whole file from offset 0 — a full, complete load.
	sc.Refresh(cache, "human", orchDir, "orch")
	if !sc.Complete() {
		t.Fatal("a fresh cache loaded by full Refresh must be Complete()")
	}
	sc.Persist()

	sessionPath := filepath.Join(humanDir, "logs", "session.jsonl")
	data, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("fresh complete-from-zero Persist must write session.jsonl: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("session.jsonl was written empty; expected the ingested entry")
	}
}

// TestWindowedRebuildUnboundedMatchesLegacy proves window<=0 is identical to the
// existing complete rebuild.
func TestWindowedRebuildUnboundedMatchesLegacy(t *testing.T) {
	sqliteBin := requireSessionSQLite(t)
	root, humanDir, orchDir := newSessionTestDirs(t)
	buildWindowSQLiteEvents(t, sqliteBin, orchDir, 5)

	sc := NewSessionCache(humanDir, root)
	cache := NewMailCache(humanDir).Refresh()
	sc.RebuildFromSourcesWindowedInMemory(cache, "human", orchDir, "orch", 0)

	assertSessionBodiesExactly(t, sc.Entries(), "e0", "e1", "e2", "e3", "e4")
	if !sc.Complete() {
		t.Fatal("window<=0 must be a complete rebuild")
	}
}
