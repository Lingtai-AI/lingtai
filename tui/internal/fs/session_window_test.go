package fs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
	sc := NewSessionCache(humanDir, root, MainAggregateWriter)
	sc.RebuildFromSourcesWindowedInMemory(cache, "human", orchDir, "orch", 3)
	assertSessionMarkersExactly(t, sc.Entries(), "e9", "e10", "e11")
	if sc.Complete() {
		t.Fatal("newest-window over a larger history must be partial")
	}

	// (2) A larger explicit older request (window 8) exhausts the 4 indexed tail
	// rows and must reach the un-indexed JSONL prefix — no duplicates, no order
	// break. Newest 8 events are e4..e11.
	sc2 := NewSessionCache(humanDir, root, MainAggregateWriter)
	sc2.RebuildFromSourcesWindowedInMemory(cache, "human", orchDir, "orch", 8)
	assertSessionMarkersExactly(t, sc2.Entries(),
		"e4", "e5", "e6", "e7", "e8", "e9", "e10", "e11")

	// (3) A window >= whole history (12) reaches the whole prefix → complete and
	// persistable.
	sc3 := NewSessionCache(humanDir, root, MainAggregateWriter)
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

// TestWindowedExactEqualWindowIsComplete proves that the authoritative backward
// JSONL scan can distinguish an exactly-full window from a truncated one: after
// selecting the oldest record its scan boundary is zero, so completeness is
// proven immediately.
func TestWindowedExactEqualWindowIsComplete(t *testing.T) {
	sqliteBin := requireSessionSQLite(t)
	root, humanDir, orchDir := newSessionTestDirs(t)
	buildWindowSQLiteEvents(t, sqliteBin, orchDir, 4) // history is exactly 4 events
	cache := NewMailCache(humanDir).Refresh()

	// Window exactly equal to history reaches byte offset zero and is complete.
	scEqual := NewSessionCache(humanDir, root, MainAggregateWriter)
	scEqual.RebuildFromSourcesWindowedInMemory(cache, "human", orchDir, "orch", 4)
	assertSessionBodiesExactly(t, scEqual.Entries(), "e0", "e1", "e2", "e3")
	if !scEqual.Complete() {
		t.Fatal("an exact authoritative window reaching offset zero must be complete")
	}

	// A larger request remains complete and stable.
	scNext := NewSessionCache(humanDir, root, MainAggregateWriter)
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

	sc := NewSessionCache(humanDir, root, MainAggregateWriter)
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

func TestWindowedRebuildWithoutSQLitePreservesLegacyGroupBoundary(t *testing.T) {
	root, humanDir, orchDir := newSessionTestDirs(t)
	eventsPath := filepath.Join(orchDir, "logs", "events.jsonl")
	content := "" +
		`{"type":"llm_response","ts":1,"model":"m"}` + "\n" +
		`{"type":"tool_call","ts":2,"tool_name":"read","tool_args":"{}"}` + "\n" +
		`{"type":"tool_result","ts":3,"tool_name":"read","status":"ok"}` + "\n" +
		`{"type":"tool_call","ts":4,"tool_name":"grep","tool_args":"{}"}` + "\n" +
		`{"type":"tool_result","ts":5,"tool_name":"grep","status":"ok"}` + "\n"
	writeSessionTestFile(t, eventsPath, content)

	sc := NewSessionCache(humanDir, root, MainAggregateWriter)
	sc.RebuildFromSourcesWindowedInMemory(NewMailCache(humanDir).Refresh(), "human", orchDir, "orch", 3)
	entries := sc.Entries()
	if len(entries) != 4 || entries[0].Type != "llm_response" {
		t.Fatalf("no-index group extension = %#v, want one hidden boundary plus newest 3 bodies", entries)
	}
	if entries[1].Type != "tool_result" || entries[2].Type != "tool_call" || entries[3].Type != "tool_result" {
		t.Fatalf("no-index window retained older group bodies: %#v", entries)
	}
	if sc.Complete() {
		t.Fatal("group back-extension must not falsely mark a newest-3 window complete")
	}
}

func TestWindowedRebuildWithoutSQLiteRetainsOnlyNewestContentAndCountsAll(t *testing.T) {
	root, humanDir, orchDir := newSessionTestDirs(t)
	eventsPath := filepath.Join(orchDir, "logs", "events.jsonl")
	var content string
	for i := 0; i < 10; i++ {
		content += sessionEventJSONL(float64(i+1), "text_output", bodyForIndex(i))
	}
	writeSessionTestFile(t, eventsPath, content)
	cache := NewMailCache(humanDir).Refresh()

	sc := NewSessionCache(humanDir, root, MainAggregateWriter)
	sc.RebuildFromSourcesWindowedInMemory(cache, "human", orchDir, "orch", 3)
	assertSessionBodiesExactly(t, sc.Entries(), "e7", "e8", "e9")
	if sc.Complete() {
		t.Fatal("no-index newest-3 content window over 10 events must be partial")
	}
	if stats := sc.HistoryStats(); stats != (SessionHistoryStats{}) {
		t.Fatalf("content path synchronously installed count metadata: %+v", stats)
	}
	stats, identity, err := sc.ExactHistoryStats()
	if err != nil || identity == "" || stats.Detailed != 10 || stats.Insights != 0 {
		t.Fatalf("async exact stats = %+v identity=%q err=%v, want Detailed=10", stats, identity, err)
	}
	sc.SetHistoryStats(stats)

	// The bounded content scan leaves the parser-proven EOF boundary, so incremental
	// Refresh sees only a new tail record rather than re-ingesting excluded rows.
	appendSessionTestFile(t, eventsPath, sessionEventJSONL(11, "text_output", "e10"))
	sc.Refresh(cache, "human", orchDir, "orch")
	assertSessionBodiesExactly(t, sc.Entries(), "e7", "e8", "e9", "e10")
	if stats := sc.HistoryStats(); stats.Detailed != 11 {
		t.Fatalf("stats after tail refresh = %+v, want Detailed=11", stats)
	}

	complete := NewSessionCache(humanDir, root, MainAggregateWriter)
	complete.RebuildFromSourcesWindowedInMemory(cache, "human", orchDir, "orch", 20)
	if !complete.Complete() || complete.Len() != 11 {
		t.Fatalf("larger no-index window = complete %v len %d, want true/11", complete.Complete(), complete.Len())
	}
}

func TestJSONLWindowSkipsHugeOlderBodyAndCountsItFromBoundedMetadata(t *testing.T) {
	root, humanDir, orchDir := newSessionTestDirs(t)
	eventsPath := filepath.Join(orchDir, "logs", "events.jsonl")
	huge := strings.Repeat("x", 2*1024*1024)
	content := `{"type":"tool_result","ts":1,"tool_name":"read","result":"` + huge + `"}` + "\n" +
		sessionEventJSONL(2, "text_output", "e1") +
		sessionEventJSONL(3, "text_output", "e2") +
		sessionEventJSONL(4, "text_output", "e3")
	writeSessionTestFile(t, eventsPath, content)

	sc := NewSessionCache(humanDir, root, MainAggregateWriter)
	sc.RebuildFromSourcesWindowedInMemory(NewMailCache(humanDir).Refresh(), "human", orchDir, "orch", 2)
	assertSessionBodiesExactly(t, sc.Entries(), "e2", "e3")
	stats, _, err := sc.ExactHistoryStats()
	if err != nil || stats.Detailed != 4 {
		t.Fatalf("bounded exact count = %+v err=%v, want four renderable entries", stats, err)
	}
}

func TestWindowedRebuildSparseInteriorIndexUsesCanonicalJSONL(t *testing.T) {
	sqliteBin := requireSessionSQLite(t)
	root, humanDir, orchDir := newSessionTestDirs(t)
	eventsPath := filepath.Join(orchDir, "logs", "events.jsonl")
	var content string
	offsets := make([]int64, 10)
	var off int64
	for i := 0; i < 10; i++ {
		offsets[i] = off
		line := sessionEventJSONL(float64(i+1), "text_output", bodyForIndex(i))
		content += line
		off += int64(len(line))
	}
	writeSessionTestFile(t, eventsPath, content)
	rootSource := canonicalSessionTestPath(t, eventsPath)
	var inserts string
	for i := 0; i < 10; i++ {
		// Leave two canonical, renderable rows absent inside the accepted endpoint
		// range. Endpoint-only SQLite coverage must not hide e4 or e7.
		if i != 4 && i != 7 {
			inserts += sessionSQLiteInsert(float64(i+1), "text_output", bodyForIndex(i), rootSource, offsets[i], "agent_events", "agent")
		}
	}
	createSessionSQLite(t, sqliteBin, orchDir, inserts)
	cache := NewMailCache(humanDir).Refresh()

	newest := NewSessionCache(humanDir, root, MainAggregateWriter)
	newest.RebuildFromSourcesWindowedInMemory(cache, "human", orchDir, "orch", 3)
	assertSessionBodiesExactly(t, newest.Entries(), "e7", "e8", "e9")
	if newest.Complete() {
		t.Fatal("newest sparse-index window must remain partial while older canonical rows remain")
	}
	stats, _, err := newest.ExactHistoryStats()
	if err != nil || stats != (SessionHistoryStats{Detailed: 10}) {
		t.Fatalf("canonical sparse-index stats = %+v err=%v, want 10 detailed", stats, err)
	}

	older := NewSessionCache(humanDir, root, MainAggregateWriter)
	older.RebuildFromSourcesWindowedInMemory(cache, "human", orchDir, "orch", 6)
	assertSessionBodiesExactly(t, older.Entries(), "e4", "e5", "e6", "e7", "e8", "e9")
	if older.Complete() {
		t.Fatal("interior holes are incorporated, but older canonical history still remains")
	}

	all := NewSessionCache(humanDir, root, MainAggregateWriter)
	all.RebuildFromSourcesWindowedInMemory(cache, "human", orchDir, "orch", 10)
	assertSessionBodiesExactly(t, all.Entries(), "e0", "e1", "e2", "e3", "e4", "e5", "e6", "e7", "e8", "e9")
	if !all.Complete() {
		t.Fatal("cache becomes complete only after every canonical row, including interior holes, is loaded")
	}
}

func TestSessionMetadataStructuralLateKeysAndNestedDecoys(t *testing.T) {
	root, humanDir, orchDir := newSessionTestDirs(t)
	eventsPath := filepath.Join(orchDir, "logs", "events.jsonl")
	huge := strings.Repeat("x", 256*1024)
	content := `{"nested":{"type":"insight","text":"nested decoy","input_tokens":99},"padding":"` + huge + `","text":"late text","ts":1,"type":"text_output"}` + "\n" +
		`{"nested":{"type":"text_output","text":"nested decoy","cached_tokens":77},"padding":"` + huge + `","ts":2,"type":"llm_response","input_tokens":2,"output_tokens":3,"cached_tokens":1}` + "\n" +
		`{"nested":{"type":"text_output","text":"nested only","input_tokens":5},"padding":"` + huge + `","type":"not_session","ts":3}` + "\n"
	writeSessionTestFile(t, eventsPath, content)

	sc := NewSessionCache(humanDir, root, MainAggregateWriter)
	sc.RebuildFromSourcesWindowedInMemory(NewMailCache(humanDir).Refresh(), "human", orchDir, "orch", 10)
	entries := sc.Entries()
	if len(entries) != 2 || entries[0].Type != "text_output" || entries[0].Body != "late text" || entries[1].Type != "llm_response" {
		t.Fatalf("late top-level metadata / nested decoys produced %#v", entries)
	}
	if entries[1].TokenUsage == nil || entries[1].TokenUsage.Input != 2 || entries[1].TokenUsage.Output != 3 || entries[1].TokenUsage.Cached != 1 {
		t.Fatalf("late top-level token metadata = %#v, want input=2 output=3 cached=1", entries[1].TokenUsage)
	}
	stats, _, err := sc.ExactHistoryStats()
	if err != nil || stats != (SessionHistoryStats{Detailed: 2}) {
		t.Fatalf("late-key exact stats = %+v err=%v, want two detailed and no nested decoys", stats, err)
	}
}

func TestWindowedRebuildSkipsNonRenderableTextMetadata(t *testing.T) {
	root, humanDir, orchDir := newSessionTestDirs(t)
	eventsPath := filepath.Join(orchDir, "logs", "events.jsonl")
	content := strings.Join([]string{
		`{"ts":1,"type":"text_output","text":"old"}`,
		`{"ts":2,"type":"text_output"}`,
		`{"ts":3,"type":"llm_call","model":"m"}`,
		`{"ts":4,"type":"text_output","text":7}`,
		`{"ts":5,"type":"text_output","text":"middle"}`,
		`{"ts":6,"type":"insight","text":""}`,
		`{"ts":7,"type":"llm_response","input_tokens":0,"output_tokens":0,"cached_tokens":0}`,
		`{"ts":8,"type":"insight","text":"idea"}`,
		`{"ts":9,"type":"text_output","text":""}`,
		`{"ts":10,"type":"text_output","text":"new"}`,
	}, "\n") + "\n"
	writeSessionTestFile(t, eventsPath, content)

	sc := NewSessionCache(humanDir, root, MainAggregateWriter)
	sc.RebuildFromSourcesWindowedInMemory(NewMailCache(humanDir).Refresh(), "human", orchDir, "orch", 5)
	entries := sc.Entries()
	wantTypes := []string{"llm_call", "text_output", "llm_response", "insight", "text_output"}
	if len(entries) != len(wantTypes) {
		t.Fatalf("window contains %d parser-produced entries, want exactly %d: %#v", len(entries), len(wantTypes), entries)
	}
	for i, want := range wantTypes {
		if entries[i].Type != want {
			t.Fatalf("entry[%d].Type = %q, want %q; entries=%#v", i, entries[i].Type, want, entries)
		}
	}
	if entries[1].Body != "middle" || entries[3].Body != "idea" || entries[4].Body != "new" {
		t.Fatalf("renderable text order = %q / %q / %q, want middle / idea / new", entries[1].Body, entries[3].Body, entries[4].Body)
	}
	if sc.Complete() {
		t.Fatal("five-entry window must remain partial while one older renderable entry remains")
	}
	stats, _, err := sc.ExactHistoryStats()
	if err != nil || stats != (SessionHistoryStats{Detailed: 3, Insights: 1}) {
		t.Fatalf("parser-equivalent exact stats = %+v err=%v, want three detailed plus one insight", stats, err)
	}
}

func TestSessionMetadataCanonicalNumericFallback(t *testing.T) {
	root, humanDir, orchDir := newSessionTestDirs(t)
	eventsPath := filepath.Join(orchDir, "logs", "events.jsonl")
	longFiniteExponent := "1e" + strings.Repeat("0", 2000) + "1"
	finiteIgnored := `{"junk":` + longFiniteExponent + `,"type":"text_output","text":"finite ignored number"}` + "\n"
	overflowIgnored := `{"junk":1e999,"type":"text_output","text":"canonical decoder rejects this record"}` + "\n"
	finiteTarget := `{"type":"llm_response","input_tokens":` + longFiniteExponent + `}` + "\n"
	writeSessionTestFile(t, eventsPath, finiteIgnored+overflowIgnored+finiteTarget)

	f, err := os.Open(eventsPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cases := []struct {
		name  string
		start int64
		line  string
		ok    bool
		want  sessionEventCountMetadata
	}{
		{name: "long finite irrelevant number", line: finiteIgnored, ok: true, want: sessionEventCountMetadata{Type: "text_output", Text: true}},
		{name: "overflowing irrelevant number", start: int64(len(finiteIgnored)), line: overflowIgnored, ok: false},
		{name: "long finite target number", start: int64(len(finiteIgnored) + len(overflowIgnored)), line: finiteTarget, ok: true, want: sessionEventCountMetadata{Type: "llm_response", InputTokens: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := readSessionEventMetadataRange(f, tc.start, int64(len(tc.line)))
			if ok != tc.ok || ok && got != tc.want {
				t.Fatalf("metadata = %+v ok=%v, want %+v ok=%v", got, ok, tc.want, tc.ok)
			}
		})
	}

	sc := NewSessionCache(humanDir, root, MainAggregateWriter)
	sc.RebuildFromSourcesWindowedInMemory(NewMailCache(humanDir).Refresh(), "human", orchDir, "orch", 10)
	entries := sc.Entries()
	if len(entries) != 2 || entries[0].Body != "finite ignored number" || entries[1].Type != "llm_response" {
		t.Fatalf("canonical numeric fallback entries = %#v, want text_output plus llm_response", entries)
	}
	if entries[1].TokenUsage == nil || entries[1].TokenUsage.Input != 10 {
		t.Fatalf("long finite target token usage = %#v, want input=10", entries[1].TokenUsage)
	}
	stats, _, err := sc.ExactHistoryStats()
	if err != nil || stats != (SessionHistoryStats{Detailed: 2}) {
		t.Fatalf("canonical numeric fallback stats = %+v err=%v, want two detailed", stats, err)
	}
}

func TestSessionMetadataCanonicalDepthLimit(t *testing.T) {
	root, humanDir, orchDir := newSessionTestDirs(t)
	eventsPath := filepath.Join(orchDir, "logs", "events.jsonl")
	lineAtDepth := func(nestedArrays int, text string) string {
		return `{"junk":` + strings.Repeat("[", nestedArrays) + "0" + strings.Repeat("]", nestedArrays) +
			`,"type":"text_output","text":"` + text + `"}` + "\n"
	}
	valid := lineAtDepth(9999, "root plus 9999 arrays")
	invalid := lineAtDepth(10000, "root plus 10000 arrays")
	var canonical map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(valid)), &canonical); err != nil {
		t.Fatalf("canonical decoder rejected valid depth boundary: %v", err)
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(invalid)), &canonical); err == nil {
		t.Fatal("canonical decoder accepted a container beyond its depth limit")
	}
	writeSessionTestFile(t, eventsPath, valid+invalid)

	f, err := os.Open(eventsPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if got, ok := readSessionEventMetadataRange(f, 0, int64(len(valid))); !ok || got != (sessionEventCountMetadata{Type: "text_output", Text: true}) {
		t.Fatalf("valid depth-boundary metadata = %+v ok=%v", got, ok)
	}
	if got, ok := readSessionEventMetadataRange(f, int64(len(valid)), int64(len(invalid))); ok {
		t.Fatalf("over-depth metadata unexpectedly accepted: %+v", got)
	}

	sc := NewSessionCache(humanDir, root, MainAggregateWriter)
	sc.RebuildFromSourcesWindowedInMemory(NewMailCache(humanDir).Refresh(), "human", orchDir, "orch", 10)
	entries := sc.Entries()
	if len(entries) != 1 || entries[0].Body != "root plus 9999 arrays" {
		t.Fatalf("depth-boundary entries = %#v, want only canonical valid row", entries)
	}
	stats, _, err := sc.ExactHistoryStats()
	if err != nil || stats != (SessionHistoryStats{Detailed: 1}) {
		t.Fatalf("depth-boundary exact stats = %+v err=%v, want one detailed", stats, err)
	}
}

// TestWindowedRebuildLargerThanHistoryIsComplete proves that when the window is
// at least as large as the whole event history, the cache is Complete() and may
// be persisted like an ordinary full rebuild.
func TestWindowedRebuildLargerThanHistoryIsComplete(t *testing.T) {
	sqliteBin := requireSessionSQLite(t)
	root, humanDir, orchDir := newSessionTestDirs(t)
	buildWindowSQLiteEvents(t, sqliteBin, orchDir, 4)

	sc := NewSessionCache(humanDir, root, MainAggregateWriter)
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

	sc := NewSessionCache(humanDir, root, MainAggregateWriter)
	cache := NewMailCache(humanDir).Refresh()
	sc.RebuildFromSourcesWindowedInMemory(cache, "human", orchDir, "orch", 3)
	sc.Persist()

	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Fatalf("partial windowed Persist must not create/overwrite session.jsonl; stat err = %v", err)
	}
}

func TestPartialWindowRefreshDoesNotAppendSessionFile(t *testing.T) {
	root, humanDir, orchDir := newSessionTestDirs(t)
	eventsPath := filepath.Join(orchDir, "logs", "events.jsonl")
	writeSessionTestFile(t, eventsPath,
		sessionEventJSONL(1, "text_output", "e0")+
			sessionEventJSONL(2, "text_output", "e1")+
			sessionEventJSONL(3, "text_output", "e2")+
			sessionEventJSONL(4, "text_output", "e3"))
	cache := NewMailCache(humanDir).Refresh()
	sessionPath := filepath.Join(humanDir, "logs", "session.jsonl")

	partial := NewSessionCache(humanDir, root, MainAggregateWriter)
	partial.RebuildFromSourcesWindowedInMemory(cache, "human", orchDir, "orch", 2)
	if partial.Complete() {
		t.Fatal("two-entry window over four events must be partial")
	}
	f, err := os.OpenFile(eventsPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(sessionEventJSONL(5, "text_output", "e4")); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	partial.Refresh(cache, "human", orchDir, "orch")
	assertSessionBodiesExactly(t, partial.Entries(), "e2", "e3", "e4")
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Fatalf("partial EOF refresh must leave session.jsonl absent; stat err = %v", err)
	}

	complete := NewSessionCache(humanDir, root, MainAggregateWriter)
	complete.Refresh(cache, "human", orchDir, "orch")
	complete.Persist()
	if data, err := os.ReadFile(sessionPath); err != nil || len(data) == 0 {
		t.Fatalf("complete cache acceptance/persistence must write session.jsonl: bytes=%d err=%v", len(data), err)
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

	sc := NewSessionCache(humanDir, root, MainAggregateWriter)
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

	sc := NewSessionCache(humanDir, root, MainAggregateWriter)
	cache := NewMailCache(humanDir).Refresh()
	sc.RebuildFromSourcesWindowedInMemory(cache, "human", orchDir, "orch", 0)

	assertSessionBodiesExactly(t, sc.Entries(), "e0", "e1", "e2", "e3", "e4")
	if !sc.Complete() {
		t.Fatal("window<=0 must be a complete rebuild")
	}
}
