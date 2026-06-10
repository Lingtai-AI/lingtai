package tui

import (
	"fmt"

	"github.com/anthropics/lingtai-tui/internal/fs"
)

// formatToolTimestamp renders a session timestamp as a short local "15:04"
// string for display beside a tool_call / tool_result line. It accepts both
// the whole-second RFC3339 form emitted into events.jsonl and the fractional
// RFC3339Nano form carried by mail-sourced entries (via fs.ParseSessionTs).
// An empty or unparseable timestamp yields an empty string so the caller can
// omit the stamp cleanly.
func formatToolTimestamp(ts string) string {
	if ts == "" {
		return ""
	}
	t := fs.ParseSessionTs(ts)
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("15:04")
}

// truncateToolBody trims a rendered tool_call / tool_result body to at most
// `limit` runes. A non-positive limit means "no truncation" — the body is
// returned verbatim (the default, so full tool call content is shown). When a
// finite limit is set and the body exceeds it, the body is cut deterministically
// at the rune boundary (never mid-codepoint) and a clear indicator reports how
// many characters were hidden.
func truncateToolBody(body string, limit int) string {
	if limit <= 0 {
		return body
	}
	runes := []rune(body)
	if len(runes) <= limit {
		return body
	}
	hidden := len(runes) - limit
	return string(runes[:limit]) + fmt.Sprintf("… (+%d chars)", hidden)
}

func isToolMessageType(t string) bool {
	return t == "tool_call" || t == "tool_result"
}

// toolGroupSeparatorBefore reports whether a blank separator line should be
// rendered before the current tool entry to visually group tool calls/results
// by the LLM API response that produced them.
func toolGroupSeparatorBefore(prev *ChatMessage, cur ChatMessage) bool {
	if prev == nil || !isToolMessageType(prev.Type) || !isToolMessageType(cur.Type) {
		return false
	}
	if prev.ApiCallID != "" || cur.ApiCallID != "" {
		return prev.ApiCallID != cur.ApiCallID
	}
	// Fallback for already-built session streams that lack API grouping
	// metadata entirely: a new tool_call immediately after a tool_result is
	// the best visible boundary hint available. Fresh sessions should get
	// either explicit api_call_id from the kernel or derived ids from hidden
	// llm_response markers before reaching the renderer.
	return prev.Type == "tool_result" && cur.Type == "tool_call"
}
