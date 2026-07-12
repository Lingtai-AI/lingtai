package fs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthropics/lingtai-tui/i18n"
)

// helper: write lines to a file, each terminated by \n.
func writeLines(t *testing.T, path string, lines []string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range lines {
		f.WriteString(l + "\n")
	}
	f.Close()
}

// helper: append raw bytes (no trailing \n) to a file.
func appendRaw(t *testing.T, path string, data string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(data)
	f.Close()
}

// makeEntry creates a raw JSON event line matching the format agents write to
// events.jsonl: ts is a Unix float, text is the content field.
func makeEntry(ts float64, typ, text string) string {
	raw := map[string]interface{}{
		"ts":   ts,
		"type": typ,
		"text": text,
	}
	b, _ := json.Marshal(raw)
	return string(b)
}

func TestTailJSONLBasic(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "events.jsonl")

	lines := []string{
		makeEntry(1781258400, "thinking", "thought 1"),
		makeEntry(1781258460, "thinking", "thought 2"),
		makeEntry(1781258520, "thinking", "thought 3"),
	}
	writeLines(t, p, lines)

	sc := &SessionCache{}
	entries, off := sc.tailJSONL(p, 0, parseEvent)
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}

	info, _ := os.Stat(p)
	if off != info.Size() {
		t.Fatalf("offset = %d, want %d (file size)", off, info.Size())
	}
}

func TestTailJSONLPartialLine(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "events.jsonl")

	// Write one complete line, then a partial line (no \n).
	lines := []string{
		makeEntry(1781258400, "thinking", "complete"),
	}
	writeLines(t, p, lines)
	appendRaw(t, p, makeEntry(1781258460, "thinking", "partial"))

	sc := &SessionCache{}
	entries, off := sc.tailJSONL(p, 0, parseEvent)

	// Should only get the complete line.
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1 (partial should be skipped)", len(entries))
	}
	if entries[0].Body != "complete" {
		t.Fatalf("got body %q, want %q", entries[0].Body, "complete")
	}

	// Now complete the partial line.
	appendRaw(t, p, "\n")

	entries2, off2 := sc.tailJSONL(p, off, parseEvent)
	if len(entries2) != 1 {
		t.Fatalf("got %d entries on retry, want 1", len(entries2))
	}
	if entries2[0].Body != "partial" {
		t.Fatalf("got body %q, want %q", entries2[0].Body, "partial")
	}

	info, _ := os.Stat(p)
	if off2 != info.Size() {
		t.Fatalf("final offset = %d, want %d", off2, info.Size())
	}
}

func TestTailJSONLIncremental(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "events.jsonl")

	lines := []string{
		makeEntry(1781258400, "thinking", "first"),
		makeEntry(1781258460, "thinking", "second"),
	}
	writeLines(t, p, lines)

	sc := &SessionCache{}
	entries, off := sc.tailJSONL(p, 0, parseEvent)
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}

	// Append 3 more lines.
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	for _, body := range []string{"third", "fourth", "fifth"} {
		f.WriteString(makeEntry(1781258520, "thinking", body) + "\n")
	}
	f.Close()

	entries2, off2 := sc.tailJSONL(p, off, parseEvent)
	if len(entries2) != 3 {
		t.Fatalf("got %d entries on second read, want 3", len(entries2))
	}

	info, _ := os.Stat(p)
	if off2 != info.Size() {
		t.Fatalf("offset = %d, want %d", off2, info.Size())
	}
}

func TestTailJSONLTruncation(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "events.jsonl")

	lines := []string{
		makeEntry(1781258400, "thinking", "before truncation"),
	}
	writeLines(t, p, lines)

	sc := &SessionCache{}
	_, off := sc.tailJSONL(p, 0, parseEvent)

	// Truncate the file (simulates molt).
	os.WriteFile(p, []byte{}, 0o644)

	// Write new content.
	writeLines(t, p, []string{
		makeEntry(1781262000, "thinking", "after truncation"),
	})

	entries, off2 := sc.tailJSONL(p, off, parseEvent)
	if len(entries) != 1 {
		t.Fatalf("got %d entries after truncation, want 1", len(entries))
	}
	if entries[0].Body != "after truncation" {
		t.Fatalf("got body %q, want %q", entries[0].Body, "after truncation")
	}

	info, _ := os.Stat(p)
	if off2 != info.Size() {
		t.Fatalf("offset = %d, want %d", off2, info.Size())
	}
}

func TestTailJSONLEmptyLines(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "events.jsonl")

	// Write with empty lines interspersed.
	f, _ := os.Create(p)
	f.WriteString(makeEntry(1781258400, "thinking", "one") + "\n")
	f.WriteString("\n")
	f.WriteString(makeEntry(1781258460, "thinking", "two") + "\n")
	f.WriteString("\n")
	f.Close()

	sc := &SessionCache{}
	entries, _ := sc.tailJSONL(p, 0, parseEvent)
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2 (empty lines should be skipped)", len(entries))
	}
}

func TestTailJSONLInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "events.jsonl")

	f, _ := os.Create(p)
	f.WriteString(makeEntry(1781258400, "thinking", "valid") + "\n")
	f.WriteString("not json at all\n")
	f.WriteString(makeEntry(1781258460, "thinking", "also valid") + "\n")
	f.Close()

	sc := &SessionCache{}
	entries, off := sc.tailJSONL(p, 0, parseEvent)

	// Should get the 2 valid entries; invalid line is skipped but offset advances past it.
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}

	info, _ := os.Stat(p)
	if off != info.Size() {
		t.Fatalf("offset = %d, want %d (should advance past invalid line)", off, info.Size())
	}
}

func TestTailJSONLNothingNew(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "events.jsonl")

	lines := []string{makeEntry(1781258400, "thinking", "one")}
	writeLines(t, p, lines)

	sc := &SessionCache{}
	_, off := sc.tailJSONL(p, 0, parseEvent)

	// Poll again with nothing new.
	entries, off2 := sc.tailJSONL(p, off, parseEvent)
	if len(entries) != 0 {
		t.Fatalf("got %d entries, want 0", len(entries))
	}
	if off2 != off {
		t.Fatalf("offset changed from %d to %d, should be unchanged", off, off2)
	}
}

// Issue #40: parseEvent must extract the kernel's `meta` block from
// notification_pair_injected events, populating SessionEntry.Meta.
func TestParseEventNotificationMeta(t *testing.T) {
	raw := map[string]interface{}{
		"ts":      1781258400.0,
		"type":    "notification_pair_injected",
		"sources": []interface{}{"email", "soul"},
		"summary": "[synthesized — kernel notification sync] 通知至：7 email，1 soul。",
		"meta": map[string]interface{}{
			"current_time": "2026-05-05T21:10:48-07:00",
			"context": map[string]interface{}{
				"system_tokens":  38398.0,
				"history_tokens": 109121.0,
				"usage":          0.147519,
			},
			"injection_seq": 2.0,
		},
	}
	line, _ := json.Marshal(raw)

	e := parseEvent(line)
	if e == nil {
		t.Fatal("parseEvent returned nil for notification_pair_injected")
	}
	if e.Type != "notification" {
		t.Fatalf("Type = %q, want %q", e.Type, "notification")
	}
	if e.Meta == nil {
		t.Fatal("Meta is nil; want extracted block")
	}
	if e.Meta.CurrentTime != "2026-05-05T21:10:48-07:00" {
		t.Errorf("CurrentTime = %q", e.Meta.CurrentTime)
	}
	if e.Meta.InjectionSeq != 2 {
		t.Errorf("InjectionSeq = %d", e.Meta.InjectionSeq)
	}
	if e.Meta.Context == nil {
		t.Fatal("Context is nil")
	}
	if e.Meta.Context.SystemTokens != 38398 {
		t.Errorf("SystemTokens = %d", e.Meta.Context.SystemTokens)
	}
	if e.Meta.Context.HistoryTokens != 109121 {
		t.Errorf("HistoryTokens = %d", e.Meta.Context.HistoryTokens)
	}
	if e.Meta.Context.Usage != 0.147519 {
		t.Errorf("Usage = %v", e.Meta.Context.Usage)
	}
}

// Older events.jsonl rows (pre-issue-#40 kernel) carry no `meta` key —
// SessionEntry.Meta must be nil so the renderer skips the footer instead
// of printing sentinel zeros.
func TestParseEventNotificationNoMeta(t *testing.T) {
	raw := map[string]interface{}{
		"ts":      1781258400.0,
		"type":    "notification_pair_injected",
		"sources": []interface{}{"email"},
		"summary": "[synthesized — kernel notification sync] 通知至：1 email。",
	}
	line, _ := json.Marshal(raw)

	e := parseEvent(line)
	if e == nil {
		t.Fatal("parseEvent returned nil")
	}
	if e.Meta != nil {
		t.Errorf("Meta = %+v; want nil for legacy events", e.Meta)
	}
}

func TestParseEventToolResultRendersToolErrorPayload(t *testing.T) {
	raw := map[string]interface{}{
		"ts":            1781258400.0,
		"type":          "tool_result",
		"tool_name":     "system",
		"status":        "error",
		"elapsed_ms":    12.0,
		"tool_trace_id": "trace-1",
		"result": map[string]interface{}{
			"status":    "error",
			"message":   "event_id is stale",
			"retryable": "unknown",
			"tool_args": map[string]interface{}{"action": "dismiss", "event_id": "old"},
			"arg_keys":  []interface{}{"action", "event_id"},
			"tool_error": map[string]interface{}{
				"reason":   "system failed during tool_returned_error: event_id is stale",
				"arg_keys": []interface{}{"action", "event_id"},
				"guidance": []interface{}{
					"Do not blindly retry the same tool call unchanged.",
					"If the failure depends on mutable external state, read the current state before retrying.",
				},
			},
		},
	}
	line, _ := json.Marshal(raw)

	e := parseEvent(line)
	if e == nil {
		t.Fatal("parseEvent returned nil")
	}
	if e.Type != "tool_result" {
		t.Fatalf("Type = %q, want tool_result", e.Type)
	}
	for _, want := range []string{
		"system → error 12ms",
		"tool_error: system failed during tool_returned_error: event_id is stale",
		"arg_keys: action, event_id",
		"guidance:",
		"- Do not blindly retry the same tool call unchanged.",
		"result: {",
	} {
		if !strings.Contains(e.Body, want) {
			t.Fatalf("Body missing %q:\n%s", want, e.Body)
		}
	}
}

func TestParseEventToolResultRendersScalarAndKeepsLongResult(t *testing.T) {
	long := strings.Repeat("界", 10005)
	raw := map[string]interface{}{
		"ts":         1781258400.0,
		"type":       "tool_result",
		"tool_name":  "bash",
		"status":     "ok",
		"elapsed_ms": 1.0,
		"result":     long,
	}
	line, _ := json.Marshal(raw)

	e := parseEvent(line)
	if e == nil {
		t.Fatal("parseEvent returned nil")
	}
	if !strings.Contains(e.Body, "bash → ok 1ms") {
		t.Fatalf("Body missing summary: %s", e.Body[:80])
	}
	if strings.Contains(e.Body, "truncated to") {
		t.Fatalf("Body should not be truncated before mail-view rendering: %s", e.Body[len(e.Body)-80:])
	}
	if got := strings.Count(e.Body, "界"); got != 10005 {
		t.Fatalf("rendered rune count = %d, want %d", got, 10005)
	}
}

func TestParseEventLLMResponseCarriesTokenUsage(t *testing.T) {
	// llm_response events carry the per-round token usage scalars directly,
	// alongside the api_call_id that groups the round. The TUI surfaces these
	// (input, cache miss, output, cache rate) at the bottom of the ctrl+o API
	// call group; here we only assert the parse layer captures the scalars.
	raw := map[string]interface{}{
		"ts":              1781258400.0,
		"type":            "llm_response",
		"api_call_id":     "api_4cd307b10902",
		"input_tokens":    181585.0,
		"output_tokens":   2275.0,
		"cached_tokens":   180224.0,
		"thinking_tokens": 516.0,
		"estimated":       false,
	}
	line, _ := json.Marshal(raw)

	e := parseEvent(line)
	if e == nil {
		t.Fatal("parseEvent returned nil")
	}
	if e.ApiCallID != "api_4cd307b10902" {
		t.Fatalf("ApiCallID = %q, want api_4cd307b10902", e.ApiCallID)
	}
	if e.TokenUsage == nil {
		t.Fatal("TokenUsage is nil; want populated scalars")
	}
	if e.TokenUsage.Input != 181585 {
		t.Errorf("TokenUsage.Input = %d, want 181585", e.TokenUsage.Input)
	}
	if e.TokenUsage.Output != 2275 {
		t.Errorf("TokenUsage.Output = %d, want 2275", e.TokenUsage.Output)
	}
	if e.TokenUsage.Cached != 180224 {
		t.Errorf("TokenUsage.Cached = %d, want 180224", e.TokenUsage.Cached)
	}
	if e.TokenUsage.Estimated {
		t.Errorf("TokenUsage.Estimated = true, want false")
	}
}

// Kernel PR #586 + #3833: the `summary=true` (a-priori) summary is logged as an
// `apriori_summary_generated` lifecycle event immediately after the raw
// tool_result. parseEvent must promote it to a first-class apriori_summary
// SessionEntry carrying the correlation id, char counts, and (as of #3833) the
// generated summary text from `generated_summary`.
func TestParseEventAprioriSummaryGeneratedLifecycle(t *testing.T) {
	raw := map[string]interface{}{
		"ts":                     1781258400.0,
		"type":                   "apriori_summary_generated",
		"api_call_id":            "api_abc",
		"tool_name":              "bash",
		"tool_call_id":           "call_77",
		"generated_summary":      "Build succeeded with 3 warnings.",
		"original_visible_chars": 48211.0,
		"summary_chars":          612.0,
	}
	line, _ := json.Marshal(raw)

	e := parseEvent(line)
	if e == nil {
		t.Fatal("parseEvent returned nil for apriori_summary_generated")
	}
	if e.Type != "apriori_summary" {
		t.Fatalf("Type = %q, want apriori_summary", e.Type)
	}
	if e.ApiCallID != "api_abc" {
		t.Errorf("ApiCallID = %q, want api_abc", e.ApiCallID)
	}
	if e.Summary == nil {
		t.Fatal("Summary is nil; want populated")
	}
	if e.Summary.Kind != "apriori_generated" {
		t.Errorf("Summary.Kind = %q, want apriori_generated", e.Summary.Kind)
	}
	if e.Summary.ToolCallID != "call_77" {
		t.Errorf("Summary.ToolCallID = %q, want call_77", e.Summary.ToolCallID)
	}
	if e.Summary.ToolName != "bash" {
		t.Errorf("Summary.ToolName = %q, want bash", e.Summary.ToolName)
	}
	if e.Summary.Text != "Build succeeded with 3 warnings." {
		t.Errorf("Summary.Text = %q, want the generated_summary text", e.Summary.Text)
	}
	if e.Summary.OriginalVisibleChars != 48211 {
		t.Errorf("Summary.OriginalVisibleChars = %d, want 48211", e.Summary.OriginalVisibleChars)
	}
	if e.Summary.SummaryChars != 612 {
		t.Errorf("Summary.SummaryChars = %d, want 612", e.Summary.SummaryChars)
	}
	if e.Summary.Unavailable {
		t.Errorf("Summary.Unavailable = true, want false for a generated summary")
	}
}

// Backward compatibility: pre-#3833 logs emit the generated lifecycle event
// without `generated_summary`. parseEvent must still promote it, leaving Text
// empty so the renderer falls back to the metadata-only note.
func TestParseEventAprioriSummaryGeneratedLifecycleNoText(t *testing.T) {
	raw := map[string]interface{}{
		"ts":                     1781258400.0,
		"type":                   "apriori_summary_generated",
		"tool_name":              "bash",
		"tool_call_id":           "call_77",
		"original_visible_chars": 48211.0,
		"summary_chars":          612.0,
	}
	line, _ := json.Marshal(raw)

	e := parseEvent(line)
	if e == nil || e.Summary == nil {
		t.Fatal("parseEvent returned nil/no summary for text-less generated lifecycle")
	}
	if e.Summary.Kind != "apriori_generated" {
		t.Errorf("Summary.Kind = %q, want apriori_generated", e.Summary.Kind)
	}
	if e.Summary.Text != "" {
		t.Errorf("Summary.Text = %q, want empty for a pre-#3833 log", e.Summary.Text)
	}
	if e.Summary.Unavailable {
		t.Errorf("Summary.Unavailable = true, want false for a generated summary")
	}
}

func TestParseEventAprioriSummaryCapRefusedLifecycle(t *testing.T) {
	raw := map[string]interface{}{
		"ts":                     1781258400.0,
		"type":                   "apriori_summary_cap_refused",
		"tool_name":              "read",
		"tool_call_id":           "call_big",
		"original_visible_chars": 600000.0,
		"cap_chars":              500000.0,
	}
	line, _ := json.Marshal(raw)

	e := parseEvent(line)
	if e == nil || e.Summary == nil {
		t.Fatal("parseEvent returned nil/no summary for cap_refused")
	}
	if e.Summary.Kind != "apriori_cap_refused" {
		t.Errorf("Summary.Kind = %q, want apriori_cap_refused", e.Summary.Kind)
	}
	if !e.Summary.Unavailable {
		t.Errorf("Summary.Unavailable = false, want true for a cap refusal")
	}
}

func TestParseEventAprioriSummaryFailedLifecycle(t *testing.T) {
	for _, evType := range []string{"apriori_summary_failed", "apriori_summary_empty", "apriori_summary_no_summarizer"} {
		raw := map[string]interface{}{
			"ts":           1781258400.0,
			"type":         evType,
			"tool_name":    "grep",
			"tool_call_id": "call_err",
		}
		line, _ := json.Marshal(raw)
		e := parseEvent(line)
		if e == nil || e.Summary == nil {
			t.Fatalf("parseEvent returned nil/no summary for %s", evType)
		}
		if e.Summary.Kind != "apriori_error" {
			t.Errorf("%s: Summary.Kind = %q, want apriori_error", evType, e.Summary.Kind)
		}
		if !e.Summary.Unavailable {
			t.Errorf("%s: Summary.Unavailable = false, want true", evType)
		}
	}
}

// Defensive secondary shape: when a tool_result event's `result` IS the
// kernel artifact dict (visible/summary payload logged on the event), parseEvent
// attaches it to the tool_result entry so the renderer can append the labelled
// summary section after the raw block.
func TestParseEventToolResultCarryingAprioriArtifact(t *testing.T) {
	raw := map[string]interface{}{
		"ts":           1781258400.0,
		"type":         "tool_result",
		"tool_name":    "bash",
		"tool_call_id": "call_art",
		"status":       "ok",
		"elapsed_ms":   30.0,
		"result": map[string]interface{}{
			"artifact":               "lingtai_apriori_tool_result_summary",
			"summary_kind":           "apriori_generated",
			"tool_call_id":           "call_art",
			"tool_name":              "bash",
			"generated_summary":      "The build succeeded; 3 warnings, no errors.",
			"summary_chars":          43.0,
			"original_visible_chars": 50123.0,
			"raw_preserved":          true,
		},
	}
	line, _ := json.Marshal(raw)

	e := parseEvent(line)
	if e == nil {
		t.Fatal("parseEvent returned nil")
	}
	if e.Type != "tool_result" {
		t.Fatalf("Type = %q, want tool_result", e.Type)
	}
	if e.Summary == nil {
		t.Fatal("Summary is nil; want detected from artifact in result")
	}
	if e.Summary.Text != "The build succeeded; 3 warnings, no errors." {
		t.Errorf("Summary.Text = %q", e.Summary.Text)
	}
	if e.Summary.ToolCallID != "call_art" {
		t.Errorf("Summary.ToolCallID = %q, want call_art", e.Summary.ToolCallID)
	}
	if e.Summary.OriginalVisibleChars != 50123 {
		t.Errorf("Summary.OriginalVisibleChars = %d, want 50123", e.Summary.OriginalVisibleChars)
	}
}

// A plain tool_result without the artifact must not get a Summary — default
// behavior is unchanged.
func TestParseEventToolResultWithoutArtifactHasNoSummary(t *testing.T) {
	raw := map[string]interface{}{
		"ts":           1781258400.0,
		"type":         "tool_result",
		"tool_name":    "bash",
		"tool_call_id": "call_plain",
		"status":       "ok",
		"elapsed_ms":   5.0,
		"result":       map[string]interface{}{"stdout": "hello", "status": "ok"},
	}
	line, _ := json.Marshal(raw)
	e := parseEvent(line)
	if e == nil {
		t.Fatal("parseEvent returned nil")
	}
	if e.Summary != nil {
		t.Fatalf("Summary = %+v, want nil for a non-summary tool_result", e.Summary)
	}
}

func TestParseEventToolResultHidesMetaBlocksBehindNotificationHint(t *testing.T) {
	raw := map[string]interface{}{
		"ts":            1781258400.0,
		"type":          "tool_result",
		"tool_name":     "bash",
		"tool_call_id":  "call_meta",
		"tool_trace_id": "trace_meta",
		"status":        "ok",
		"elapsed_ms":    42.0,
		"result": map[string]interface{}{
			"status": "ok",
			"stdout": "done",
			"stderr": "",
			"_runtime_pending": map[string]interface{}{
				"current_time": "2026-06-21T00:40:00-07:00",
				"context": map[string]interface{}{
					"usage": 0.4,
				},
			},
		},
		"_runtime": map[string]interface{}{
			"guidance": map[string]interface{}{
				"guidance_version": "0.3.0",
				"sections": []interface{}{
					map[string]interface{}{"title": "Summarize and molt deliberately"},
				},
			},
		},
		"notifications": map[string]interface{}{
			"mcp.telegram": map[string]interface{}{"header": "1 new event"},
		},
		"_notification_guidance": "read the producer channel first",
	}
	line, _ := json.Marshal(raw)

	e := parseEvent(line)
	if e == nil {
		t.Fatal("parseEvent returned nil")
	}
	// The `_meta` envelope blocks are too noisy for the ctrl+o chat replay, so
	// instead of expanding them inline we emit a single hint pointing the user
	// at `/notification`. The non-meta tool summary and result body still show.
	for _, want := range []string{
		"bash → ok 42ms",
		i18n.T("mail.meta_hidden_hint"),
		"result: {",
		`"stdout": "done"`,
	} {
		if !strings.Contains(e.Body, want) {
			t.Fatalf("Body missing %q:\n%s", want, e.Body)
		}
	}
	wantBody := strings.Join([]string{
		"bash → ok 42ms",
		i18n.T("mail.meta_hidden_hint"),
		"result: {",
		`  "status": "ok",`,
		`  "stderr": "",`,
		`  "stdout": "done"`,
		"}",
	}, "\n")
	if e.Body != wantBody {
		t.Fatalf("Body changed formatting:\ngot:\n%s\nwant:\n%s", e.Body, wantBody)
	}
	// None of the expanded meta blocks should leak into the replay body.
	for _, notWant := range []string{
		"_tool:",
		`"trace_id": "trace_meta"`,
		"_runtime.state:",
		"2026-06-21T00:40:00-07:00",
		"_runtime.guidance:",
		"guidance_version",
		"notifications:",
		"mcp.telegram",
		"_notification_guidance:",
		"read the producer channel first",
		"_runtime_pending",
	} {
		if strings.Contains(e.Body, notWant) {
			t.Fatalf("Body should not contain hidden meta block %q:\n%s", notWant, e.Body)
		}
	}
}
