package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/anthropics/lingtai-tui/i18n"
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

func firstRenderedLine(body string) string {
	if i := strings.IndexAny(body, "\r\n"); i >= 0 {
		return body[:i]
	}
	return body
}

const toolCallSummaryLimit = 200

// aprioriSummaryPreviewLimit caps the generated summary text shown in the FIRST
// Ctrl+O/detail layer. The deeper (verboseExtended) layer renders the full
// summary. Counted in runes, never split mid-codepoint.
const aprioriSummaryPreviewLimit = 200

// previewSummaryText returns the first aprioriSummaryPreviewLimit runes of the
// generated summary text with a trailing ellipsis when (and only when) the text
// is longer than the limit. A text at or under the limit is returned verbatim,
// so no misleading ellipsis is shown.
func previewSummaryText(text string) string {
	runes := []rune(text)
	if len(runes) <= aprioriSummaryPreviewLimit {
		return text
	}
	return string(runes[:aprioriSummaryPreviewLimit]) + "…"
}

// compactToolCallSummary keeps Ctrl+O level-1 tool_call entries short even
// when the first rendered line is a long single-line JSON payload. The limit is
// counted in runes from the TUI-rendered string perspective, not terminal visual
// width; the deeper Ctrl+O level still renders the full tool_call body.
func compactToolCallSummary(body string) string {
	line := firstRenderedLine(body)
	runes := []rune(line)
	if len(runes) <= toolCallSummaryLimit {
		return line
	}
	if toolCallSummaryLimit <= 1 {
		return "…"
	}
	return string(runes[:toolCallSummaryLimit-1]) + "…"
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

func isApiGroupedVerboseMessageType(t string) bool {
	switch t {
	case "thinking", "diary", "text_input", "text_output", "tool_call", "tool_result", "apriori_summary":
		return true
	default:
		return false
	}
}

// apiCallGroupSeparatorBefore reports whether a blank separator line should be
// rendered before cur in the ctrl+o verbose stream. Thinking/diary/text/tool
// entries that share an api_call_id came from the same LLM API round-trip and
// stay visually grouped; a non-empty api_call_id change starts a new group.
// Legacy tool streams without metadata keep the historical tool_result ->
// tool_call fallback so older transcripts still show a visible boundary.
func apiCallGroupSeparatorBefore(prev *ChatMessage, cur ChatMessage) bool {
	if prev == nil || !isApiGroupedVerboseMessageType(prev.Type) || !isApiGroupedVerboseMessageType(cur.Type) {
		return false
	}
	if prev.ApiCallID != "" || cur.ApiCallID != "" {
		return prev.ApiCallID != cur.ApiCallID
	}
	return toolGroupSeparatorBefore(prev, cur)
}

// renderAprioriSummaryBlock renders the model-visible `summary=true` (a-priori)
// tool-result summary as a distinct, clearly-labelled block — the thing the
// agent actually saw after the kernel replaced the raw tool payload. It is
// rendered right after the corresponding raw tool_result so the contrast is
// obvious: raw stdout above, the compressed model-visible summary below.
//
// `wrapWidth` is the available text width; the caller writes the returned lines
// verbatim (each already styled and indented). When the summary text is absent
// (the lifecycle-event path carries counts but not the generated text), the
// block still renders the label + metadata so it is clear a summary was shown.
//
// `preview` controls how much of the generated summary text is shown: the first
// Ctrl+O/detail layer (verboseThinking) passes true to render only a
// 200-character preview of the summary text; the deeper layer (verboseExtended)
// passes false to render the full summary. Only the generated summary text is
// previewed — the fallback no-text note and cap/error messages are always shown
// whole.
func renderAprioriSummaryBlock(s *fs.AprioriSummary, wrapWidth int, preview bool) []string {
	if s == nil {
		return nil
	}
	if wrapWidth < 20 {
		wrapWidth = 20
	}

	accent := ColorAccent
	body := ColorAgent
	if s.Unavailable {
		// Cap-refusal / fail-closed error: the agent did NOT get a summary.
		// Use the tool/distress palette so it does not read as a clean summary.
		accent = ColorTool
		body = ColorTool
	}
	labelStyle := lipgloss.NewStyle().Foreground(accent).Bold(true)
	bodyStyle := lipgloss.NewStyle().Foreground(body).Italic(true)
	footerStyle := lipgloss.NewStyle().Foreground(ColorTextFaint)

	var out []string

	label := i18n.T("mail.apriori_summary_label")
	if s.Unavailable {
		label = i18n.T("mail.apriori_summary_unavailable_label")
	}
	out = append(out, labelStyle.Render("  "+label))

	// Body: the generated summary text (success) or the kernel message
	// (cap/error). When neither is present (lifecycle-only event), show a
	// faint note pointing at where the full text lives.
	text := strings.TrimSpace(s.Text)
	if text == "" && !s.Unavailable {
		text = i18n.TF("mail.apriori_summary_no_text", formatComma(int64(s.SummaryChars)))
		bodyStyle = footerStyle
	} else if text != "" && preview {
		// First Ctrl+O layer: show only a 200-char preview of the generated
		// summary text. The deeper layer (preview=false) shows it in full.
		text = previewSummaryText(text)
	}
	if text != "" {
		wrapped := lipgloss.NewStyle().Width(wrapWidth).Render(text)
		for _, line := range strings.Split(wrapped, "\n") {
			out = append(out, bodyStyle.Render("    "+line))
		}
	}

	// Compact metadata footer: tool name, the compression, raw-preservation.
	tool := s.ToolName
	if tool == "" {
		tool = "tool"
	}
	var footer string
	if s.Unavailable {
		footer = i18n.TF("mail.apriori_summary_footer_unavailable", tool, formatComma(int64(s.OriginalVisibleChars)))
	} else {
		footer = i18n.TF("mail.apriori_summary_footer", tool,
			formatComma(int64(s.OriginalVisibleChars)), formatComma(int64(s.SummaryChars)))
	}
	out = append(out, footerStyle.Render("    "+footer))
	return out
}

func isTextOutputMessageType(t string) bool {
	return t == "text_output"
}

// textOutputGroupSeparatorBefore reports whether a blank separator line should
// be rendered before the current text_output entry to mirror tool-call grouping
// by the LLM API response that produced the assistant text.
func textOutputGroupSeparatorBefore(prev *ChatMessage, cur ChatMessage) bool {
	if prev == nil || !isTextOutputMessageType(prev.Type) || !isTextOutputMessageType(cur.Type) {
		return false
	}
	if prev.ApiCallID == "" && cur.ApiCallID == "" {
		return false
	}
	return prev.ApiCallID != cur.ApiCallID
}
