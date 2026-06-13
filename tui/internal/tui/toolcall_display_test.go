package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/lingtai-tui/internal/fs"
)

func TestTruncateToolBody_NoLimitReturnsFull(t *testing.T) {
	body := strings.Repeat("x", 5000)
	got := truncateToolBody(body, 0)
	if got != body {
		t.Errorf("limit 0 must not truncate: got %d chars, want %d", len([]rune(got)), len([]rune(body)))
	}
}

func TestTruncateToolBody_NegativeLimitReturnsFull(t *testing.T) {
	body := strings.Repeat("y", 1000)
	got := truncateToolBody(body, -1)
	if got != body {
		t.Errorf("negative limit must not truncate: got %d chars, want %d", len([]rune(got)), len([]rune(body)))
	}
}

func TestTruncateToolBody_ShorterThanLimitUnchanged(t *testing.T) {
	body := "echo(hello)"
	got := truncateToolBody(body, 200)
	if got != body {
		t.Errorf("body shorter than limit must be unchanged: got %q, want %q", got, body)
	}
}

func TestTruncateToolBody_TruncatesAtLimitWithIndicator(t *testing.T) {
	body := strings.Repeat("z", 300)
	got := truncateToolBody(body, 200)
	// The retained content must be exactly the first 200 runes.
	if !strings.HasPrefix(got, strings.Repeat("z", 200)) {
		t.Errorf("truncated output must begin with first 200 chars of body")
	}
	// The full body must NOT survive intact.
	if got == body {
		t.Errorf("body longer than limit must be truncated")
	}
	// Truncation must be indicated to the user.
	if !strings.Contains(got, "…") && !strings.Contains(got, "...") {
		t.Errorf("truncation must be indicated with an ellipsis, got %q", got)
	}
	// The indicator should communicate how many characters were hidden (100).
	if !strings.Contains(got, "100") {
		t.Errorf("truncation indicator should report hidden char count (100), got %q", got)
	}
}

func TestCompactToolCallSummary_TruncatesLongSingleLineTo100Runes(t *testing.T) {
	body := "tool({\"args\":\"" + strings.Repeat("字", 140) + "\"})"
	got := compactToolCallSummary(body)
	if n := len([]rune(got)); n != toolCallSummaryLimit {
		t.Fatalf("compactToolCallSummary length = %d runes, want %d; got %q", n, toolCallSummaryLimit, got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("compactToolCallSummary should mark truncation with ellipsis, got %q", got)
	}
	if strings.Contains(got, strings.Repeat("字", 120)) {
		t.Fatalf("compactToolCallSummary retained more than the 100-rune cap: %q", got)
	}
}

func TestCompactToolCallSummary_TakesFirstLineBefore100RuneCap(t *testing.T) {
	body := "read({})\n" + strings.Repeat("x", 200)
	got := compactToolCallSummary(body)
	if got != "read({})" {
		t.Fatalf("compactToolCallSummary should preserve short first line only, got %q", got)
	}
}

func TestFormatToolTimestamp_EmptyIsEmpty(t *testing.T) {
	if got := formatToolTimestamp(""); got != "" {
		t.Errorf("empty timestamp must format to empty string, got %q", got)
	}
}

func TestFormatToolTimestamp_RFC3339WholeSeconds(t *testing.T) {
	// events.jsonl uses whole-second RFC3339.
	ts := "2026-06-08T07:08:26Z"
	got := formatToolTimestamp(ts)
	want := time.Date(2026, 6, 8, 7, 8, 26, 0, time.UTC).Local().Format("15:04")
	if got != want {
		t.Errorf("formatToolTimestamp(%q) = %q, want %q", ts, got, want)
	}
}

func TestFormatToolTimestamp_RFC3339Nano(t *testing.T) {
	// mail-sourced entries can carry fractional seconds.
	ts := "2026-06-08T07:08:26.1279Z"
	got := formatToolTimestamp(ts)
	want := time.Date(2026, 6, 8, 7, 8, 26, 0, time.UTC).Local().Format("15:04")
	if got != want {
		t.Errorf("formatToolTimestamp(%q) = %q, want %q", ts, got, want)
	}
}

func TestFormatToolTimestamp_GarbageIsEmpty(t *testing.T) {
	if got := formatToolTimestamp("not-a-timestamp"); got != "" {
		t.Errorf("unparseable timestamp must format to empty string, got %q", got)
	}
}

func TestToolGroupSeparatorBefore_ExplicitApiCallIDChange(t *testing.T) {
	prev := &ChatMessage{Type: "tool_result", ApiCallID: "api_one"}
	cur := ChatMessage{Type: "tool_call", ApiCallID: "api_two"}
	if !toolGroupSeparatorBefore(prev, cur) {
		t.Errorf("tool entries from different api_call_id values should be separated")
	}
}

func TestToolGroupSeparatorBefore_ExplicitApiCallIDSameGroup(t *testing.T) {
	prev := &ChatMessage{Type: "tool_result", ApiCallID: "api_one"}
	cur := ChatMessage{Type: "tool_call", ApiCallID: "api_one"}
	if toolGroupSeparatorBefore(prev, cur) {
		t.Errorf("tool entries from the same api_call_id should stay grouped")
	}
}

func TestToolGroupSeparatorBefore_ConsecutiveCallsStayGrouped(t *testing.T) {
	prev := &ChatMessage{Type: "tool_call"}
	cur := ChatMessage{Type: "tool_call"}
	if toolGroupSeparatorBefore(prev, cur) {
		t.Errorf("consecutive tool_calls without grouping metadata should stay grouped")
	}
}

func TestToolGroupSeparatorBefore_ResultAfterCallStaysGrouped(t *testing.T) {
	prev := &ChatMessage{Type: "tool_call"}
	cur := ChatMessage{Type: "tool_result"}
	if toolGroupSeparatorBefore(prev, cur) {
		t.Errorf("tool_result following its tool_call should not be separated")
	}
}

func TestToolGroupSeparatorBefore_LegacyNewCallAfterResultFallback(t *testing.T) {
	prev := &ChatMessage{Type: "tool_result"}
	cur := ChatMessage{Type: "tool_call"}
	if !toolGroupSeparatorBefore(prev, cur) {
		t.Errorf("legacy tool_call after tool_result should get a fallback separator")
	}
}

func TestToolGroupSeparatorBefore_FirstToolHasNoSeparator(t *testing.T) {
	if toolGroupSeparatorBefore(nil, ChatMessage{Type: "tool_call"}) {
		t.Errorf("first tool entry must not get a leading separator")
	}
	if toolGroupSeparatorBefore(nil, ChatMessage{Type: "tool_result"}) {
		t.Errorf("first tool entry must not get a leading separator")
	}
}

func TestToolGroupSeparatorBefore_NonToolBoundariesNoSeparator(t *testing.T) {
	cases := []struct{ prev, cur string }{
		{"thinking", "tool_call"},
		{"text_output", "tool_call"},
		{"mail", "tool_call"},
		{"tool_result", "thinking"},
		{"tool_call", "mail"},
		{"thinking", "thinking"},
	}
	for _, c := range cases {
		prev := &ChatMessage{Type: c.prev, ApiCallID: "api_one"}
		cur := ChatMessage{Type: c.cur, ApiCallID: "api_two"}
		if toolGroupSeparatorBefore(prev, cur) {
			t.Errorf("non-tool boundary (%q→%q) must not get a tool-group separator", c.prev, c.cur)
		}
	}
}

func TestTruncateToolBody_DeterministicByRune(t *testing.T) {
	// Multi-byte runes must not be split mid-codepoint.
	body := strings.Repeat("世", 300) // each is 3 bytes, 1 rune
	got := truncateToolBody(body, 100)
	// Must be valid UTF-8 — no broken codepoints.
	for _, r := range got {
		if r == '�' {
			t.Fatalf("truncation split a multi-byte rune (got replacement char)")
		}
	}
	// First 100 runes retained.
	if !strings.HasPrefix(got, strings.Repeat("世", 100)) {
		t.Errorf("must retain first 100 runes intact")
	}
}

func TestRenderMessages_InsertsBlankLineBetweenApiCallGroups(t *testing.T) {
	m := MailModel{width: 100}
	out := m.renderMessages([]ChatMessage{
		{Type: "tool_call", Body: "read({})", ApiCallID: "api_one", Timestamp: "2026-06-08T07:08:26Z"},
		{Type: "tool_result", Body: "read → ok", ApiCallID: "api_one", Timestamp: "2026-06-08T07:08:27Z"},
		{Type: "tool_call", Body: "bash({})", ApiCallID: "api_two", Timestamp: "2026-06-08T07:08:28Z"},
	})
	if !strings.Contains(out, "read → ok") || !strings.Contains(out, "bash({})") {
		t.Fatalf("rendered output missing tool bodies: %q", out)
	}
	if !strings.Contains(out, "read → ok") || !strings.Contains(out, "\n\n") {
		t.Fatalf("expected a blank separator line between api groups, got %q", out)
	}
}

func TestRenderMessages_DoesNotSeparateSameApiCallGroup(t *testing.T) {
	m := MailModel{width: 100}
	out := m.renderMessages([]ChatMessage{
		{Type: "tool_call", Body: "read({})", ApiCallID: "api_one", Timestamp: "2026-06-08T07:08:26Z"},
		{Type: "tool_result", Body: "read → ok", ApiCallID: "api_one", Timestamp: "2026-06-08T07:08:27Z"},
		{Type: "tool_call", Body: "bash({})", ApiCallID: "api_one", Timestamp: "2026-06-08T07:08:28Z"},
	})
	if strings.Contains(out, "\n\n") {
		t.Fatalf("same api_call_id should render as one group without blank separator: %q", out)
	}
}

func TestTextOutputGroupSeparatorBefore_ExplicitApiCallIDChange(t *testing.T) {
	prev := &ChatMessage{Type: "text_output", ApiCallID: "api_one"}
	cur := ChatMessage{Type: "text_output", ApiCallID: "api_two"}
	if !textOutputGroupSeparatorBefore(prev, cur) {
		t.Errorf("text_output entries from different api_call_id values should be separated")
	}
}

func TestTextOutputGroupSeparatorBefore_ExplicitApiCallIDSameGroup(t *testing.T) {
	prev := &ChatMessage{Type: "text_output", ApiCallID: "api_one"}
	cur := ChatMessage{Type: "text_output", ApiCallID: "api_one"}
	if textOutputGroupSeparatorBefore(prev, cur) {
		t.Errorf("text_output entries from the same api_call_id should stay grouped")
	}
}

func TestTextOutputGroupSeparatorBefore_NoMetadataStaysGrouped(t *testing.T) {
	prev := &ChatMessage{Type: "text_output"}
	cur := ChatMessage{Type: "text_output"}
	if textOutputGroupSeparatorBefore(prev, cur) {
		t.Errorf("legacy text_output entries without grouping metadata should stay grouped")
	}
}

func TestTextOutputGroupSeparatorBefore_NonTextBoundariesNoSeparator(t *testing.T) {
	cases := []struct{ prev, cur string }{
		{"thinking", "text_output"},
		{"text_output", "thinking"},
		{"tool_result", "text_output"},
		{"text_output", "tool_call"},
	}
	for _, c := range cases {
		prev := &ChatMessage{Type: c.prev, ApiCallID: "api_one"}
		cur := ChatMessage{Type: c.cur, ApiCallID: "api_two"}
		if textOutputGroupSeparatorBefore(prev, cur) {
			t.Errorf("non-text-output boundary (%q→%q) must not get a text-output separator", c.prev, c.cur)
		}
	}
}

func TestRenderMessages_InsertsBlankLineBetweenTextOutputApiCallGroups(t *testing.T) {
	m := MailModel{width: 100}
	out := m.renderMessages([]ChatMessage{
		{Type: "text_output", Body: "first answer", ApiCallID: "api_one"},
		{Type: "text_output", Body: "second answer", ApiCallID: "api_two"},
	})
	if !strings.Contains(out, "first answer") || !strings.Contains(out, "second answer") {
		t.Fatalf("rendered output missing text_output bodies: %q", out)
	}
	if !strings.Contains(out, "\n\n") {
		t.Fatalf("expected a blank separator line between text_output api groups, got %q", out)
	}
}

func TestRenderMessages_DoesNotSeparateSameTextOutputApiCallGroup(t *testing.T) {
	m := MailModel{width: 100}
	out := m.renderMessages([]ChatMessage{
		{Type: "text_output", Body: "first chunk", ApiCallID: "api_one"},
		{Type: "text_output", Body: "second chunk", ApiCallID: "api_one"},
	})
	if strings.Contains(out, "\n\n") {
		t.Fatalf("same text_output api_call_id should render as one group without blank separator: %q", out)
	}
}

func TestBuildMessagesAssignsApiCallIDToTextOutput(t *testing.T) {
	humanDir := t.TempDir()
	orchDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(orchDir, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	events := strings.Join([]string{
		`{"ts":1781300000,"type":"llm_response","api_call_id":"api_one"}`,
		`{"ts":1781300001,"type":"text_output","text":"first answer"}`,
		`{"ts":1781300002,"type":"llm_call","api_call_id":"api_two"}`,
		`{"ts":1781300003,"type":"llm_response","api_call_id":"api_two"}`,
		`{"ts":1781300004,"type":"text_output","text":"second answer"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(orchDir, "logs", "events.jsonl"), []byte(events), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewMailModel(humanDir, "human", t.TempDir(), orchDir, "agent", unlimitedPageSize, "", "en", false, 0)
	m.verbose = verboseThinking
	m.buildMessages()

	var textOutputs []ChatMessage
	for _, msg := range m.messages {
		if msg.Type == "text_output" {
			textOutputs = append(textOutputs, msg)
		}
	}
	if len(textOutputs) != 2 {
		t.Fatalf("got %d text_output messages, want 2: %#v", len(textOutputs), textOutputs)
	}
	if textOutputs[0].ApiCallID != "api_one" || textOutputs[1].ApiCallID != "api_two" {
		t.Fatalf("text_output api_call_id values = %q, %q; want api_one, api_two", textOutputs[0].ApiCallID, textOutputs[1].ApiCallID)
	}
}

func TestRenderMessages_InsertsBlankLineBetweenDiaryApiCallGroups(t *testing.T) {
	m := MailModel{width: 100}
	out := m.renderMessages([]ChatMessage{
		{Type: "diary", Body: "first diary", ApiCallID: "api_one"},
		{Type: "diary", Body: "second diary", ApiCallID: "api_two"},
	})
	if !strings.Contains(out, "first diary") || !strings.Contains(out, "second diary") {
		t.Fatalf("rendered output missing diary bodies: %q", out)
	}
	if !strings.Contains(out, "\n\n") {
		t.Fatalf("expected a blank separator line between diary api groups, got %q", out)
	}
}

func TestRenderMessages_KeepsMixedVerboseEntriesInSameApiCallGroup(t *testing.T) {
	m := MailModel{width: 100}
	out := m.renderMessages([]ChatMessage{
		{Type: "diary", Body: "reasoning", ApiCallID: "api_one"},
		{Type: "text_output", Body: "answer", ApiCallID: "api_one"},
		{Type: "tool_call", Body: "read({})", ApiCallID: "api_one", Timestamp: "2026-06-08T07:08:26Z"},
		{Type: "tool_result", Body: "read ok", ApiCallID: "api_one", Timestamp: "2026-06-08T07:08:27Z"},
	})
	if strings.Contains(out, "\n\n") {
		t.Fatalf("mixed verbose entries from the same api_call_id should stay grouped, got %q", out)
	}
}

func TestRenderMessages_SeparatesMixedVerboseApiCallGroups(t *testing.T) {
	m := MailModel{width: 100}
	out := m.renderMessages([]ChatMessage{
		{Type: "diary", Body: "first diary", ApiCallID: "api_one"},
		{Type: "tool_call", Body: "read({})", ApiCallID: "api_one", Timestamp: "2026-06-08T07:08:26Z"},
		{Type: "diary", Body: "second diary", ApiCallID: "api_two"},
	})
	if !strings.Contains(out, "first diary") || !strings.Contains(out, "second diary") {
		t.Fatalf("rendered output missing mixed verbose bodies: %q", out)
	}
	if !strings.Contains(out, "\n\n") {
		t.Fatalf("expected a blank separator line before the new api group, got %q", out)
	}
}

func TestBuildMessagesAssignsApiCallIDToDiaryThinkingAndTextInput(t *testing.T) {
	humanDir := t.TempDir()
	orchDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(orchDir, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	events := strings.Join([]string{
		`{"ts":1781300000,"type":"llm_response","api_call_id":"api_one"}`,
		`{"ts":1781300001,"type":"diary","text":"first diary"}`,
		`{"ts":1781300002,"type":"thinking","text":"first thinking"}`,
		`{"ts":1781300003,"type":"text_input","text":"first input"}`,
		`{"ts":1781300004,"type":"llm_call","api_call_id":"api_two"}`,
		`{"ts":1781300005,"type":"llm_response","api_call_id":"api_two"}`,
		`{"ts":1781300006,"type":"diary","text":"second diary"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(orchDir, "logs", "events.jsonl"), []byte(events), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewMailModel(humanDir, "human", t.TempDir(), orchDir, "agent", unlimitedPageSize, "", "en", false, 0)
	m.verbose = verboseThinking
	m.buildMessages()

	got := map[string][]string{}
	for _, msg := range m.messages {
		switch msg.Type {
		case "diary", "thinking", "text_input":
			got[msg.Type] = append(got[msg.Type], msg.ApiCallID)
		}
	}
	if ids := got["diary"]; len(ids) != 2 || ids[0] != "api_one" || ids[1] != "api_two" {
		t.Fatalf("diary api_call_id values = %#v, want [api_one api_two]", ids)
	}
	if ids := got["thinking"]; len(ids) != 1 || ids[0] != "api_one" {
		t.Fatalf("thinking api_call_id values = %#v, want [api_one]", ids)
	}
	if ids := got["text_input"]; len(ids) != 1 || ids[0] != "api_one" {
		t.Fatalf("text_input api_call_id values = %#v, want [api_one]", ids)
	}
}

func TestShouldShowToolEntriesAtVerboseThinkingOnlyAsSummaries(t *testing.T) {
	toolResult := fs.SessionEntry{Type: "tool_result", Body: "read → ok\nresult: full payload"}
	toolCall := fs.SessionEntry{Type: "tool_call", Body: "read({})\nmore args"}
	m := MailModel{verbose: verboseOff}
	if m.shouldShow(toolResult) || m.shouldShow(toolCall) {
		t.Fatalf("tool entries should be hidden when Ctrl+O is off")
	}
	m.verbose = verboseThinking
	if !m.shouldShow(toolResult) || !m.shouldShow(toolCall) {
		t.Fatalf("tool_call/tool_result first-line summaries should show at Ctrl+O level 1")
	}
	m.verbose = verboseExtended
	if !m.shouldShow(toolResult) || !m.shouldShow(toolCall) {
		t.Fatalf("tool_call/tool_result should both show full bodies at Ctrl+O level 2")
	}
}

func TestRenderMessages_TruncatesToolEntriesToFirstLineAtVerboseThinking(t *testing.T) {
	longToolCall := "read({\"file_path\":\"/tmp/a\",\"payload\":\"" + strings.Repeat("x", 160) + "\"})\nmore args"
	m := MailModel{width: 260, verbose: verboseThinking}
	out := m.renderMessages([]ChatMessage{
		{Type: "tool_call", Body: longToolCall, ApiCallID: "api_one", Timestamp: "2026-06-08T07:08:26Z"},
		{Type: "tool_result", Body: "read → ok 12ms\nresult: {\n  \"large\": true\n}", ApiCallID: "api_one", Timestamp: "2026-06-08T07:08:27Z"},
	})
	if !strings.Contains(out, "read({") || !strings.Contains(out, "read → ok 12ms") {
		t.Fatalf("rendered output missing first lines: %q", out)
	}
	if strings.Contains(out, "more args") || strings.Contains(out, "large") || strings.Contains(out, "result:") {
		t.Fatalf("Ctrl+O level 1 should render only compact tool_call/tool_result summaries, got %q", out)
	}
	if strings.Contains(out, strings.Repeat("x", 120)) {
		t.Fatalf("Ctrl+O level 1 should cap long single-line tool_call summaries to 100 chars, got %q", out)
	}
	if !strings.Contains(out, "…") {
		t.Fatalf("Ctrl+O level 1 should mark truncated long tool_call summaries, got %q", out)
	}
}

func TestRenderMessages_ShowsFullToolEntriesAtVerboseExtended(t *testing.T) {
	m := MailModel{width: 100, verbose: verboseExtended}
	out := m.renderMessages([]ChatMessage{
		{Type: "tool_call", Body: "read({\"file_path\":\"/tmp/a\"})\nmore args", ApiCallID: "api_one", Timestamp: "2026-06-08T07:08:26Z"},
		{Type: "tool_result", Body: "read → ok 12ms\nresult: {\n  \"large\": true\n}", ApiCallID: "api_one", Timestamp: "2026-06-08T07:08:27Z"},
	})
	if !strings.Contains(out, "read → ok 12ms") || !strings.Contains(out, "large") || !strings.Contains(out, "result:") || !strings.Contains(out, "more args") {
		t.Fatalf("Ctrl+O level 2 should render full tool_call/tool_result bodies, got %q", out)
	}
}
