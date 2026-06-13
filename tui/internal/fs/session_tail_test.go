package fs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
			"stamina_left_seconds": 35884.5,
			"injection_seq":        2.0,
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
	if e.Meta.StaminaLeftSeconds != 35884.5 {
		t.Errorf("StaminaLeftSeconds = %v", e.Meta.StaminaLeftSeconds)
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

func TestParseEventToolResultRendersScalarAndCapsLongResult(t *testing.T) {
	long := strings.Repeat("界", maxToolResultRenderChars+5)
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
	if !strings.Contains(e.Body, "truncated to 10000 chars") {
		t.Fatalf("Body missing truncation marker")
	}
	if got := strings.Count(e.Body, "界"); got != maxToolResultRenderChars {
		t.Fatalf("rendered rune count = %d, want %d", got, maxToolResultRenderChars)
	}
}
