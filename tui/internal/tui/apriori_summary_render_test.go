package tui

import (
	"strings"
	"testing"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/fs"
)

// The raw tool_result must still render in full, and the model-visible
// summary=true block must render immediately AFTER it with a clear label —
// the core of Jason's request (kernel PR #586).
func TestRenderMessages_SummaryBlockAfterRawToolResult(t *testing.T) {
	m := MailModel{width: 100}
	out := m.renderMessages([]ChatMessage{
		{Type: "tool_call", Body: "bash({})", ApiCallID: "api_1", Timestamp: "2026-06-08T07:08:26Z"},
		{Type: "tool_result", Body: "bash → ok\nRAWSTDOUT line one", ApiCallID: "api_1", Timestamp: "2026-06-08T07:08:27Z"},
		{Type: "apriori_summary", ApiCallID: "api_1", Timestamp: "2026-06-08T07:08:27Z", Summary: &fs.AprioriSummary{
			Kind:                 "apriori_generated",
			ToolCallID:           "call_1",
			ToolName:             "bash",
			Text:                 "Build succeeded with 3 warnings.",
			OriginalVisibleChars: 48211,
			SummaryChars:         32,
		}},
	})

	// Raw stays intact.
	if !strings.Contains(out, "RAWSTDOUT line one") {
		t.Fatalf("raw tool_result body missing — raw display must be kept intact:\n%s", out)
	}
	// Clear label that this is the model-visible summary.
	label := i18n.T("mail.apriori_summary_label")
	if !strings.Contains(out, label) {
		t.Fatalf("summary label %q missing:\n%s", label, out)
	}
	// The generated summary text the agent actually saw.
	if !strings.Contains(out, "Build succeeded with 3 warnings.") {
		t.Fatalf("generated summary text missing:\n%s", out)
	}
	// Compact metadata footer (tool + compression).
	if !strings.Contains(out, "bash") || !strings.Contains(out, "48,211") || !strings.Contains(out, "32") {
		t.Fatalf("summary metadata footer missing tool/char counts:\n%s", out)
	}
	// Ordering: the summary label must appear AFTER the raw body.
	if strings.Index(out, "RAWSTDOUT line one") > strings.Index(out, label) {
		t.Fatalf("summary block must appear AFTER the raw tool_result:\n%s", out)
	}
}

// Tool results without a summary are unchanged — no summary block, no label.
func TestRenderMessages_NonSummaryToolResultUnchanged(t *testing.T) {
	m := MailModel{width: 100}
	out := m.renderMessages([]ChatMessage{
		{Type: "tool_result", Body: "bash → ok\nplain output", ApiCallID: "api_1", Timestamp: "2026-06-08T07:08:27Z"},
	})
	if !strings.Contains(out, "plain output") {
		t.Fatalf("plain tool_result body missing:\n%s", out)
	}
	if strings.Contains(out, i18n.T("mail.apriori_summary_label")) ||
		strings.Contains(out, i18n.T("mail.apriori_summary_unavailable_label")) {
		t.Fatalf("non-summary tool_result must not render a summary block:\n%s", out)
	}
}

// The defensive secondary shape: a tool_result whose result IS the artifact.
// The summary block must render right after that same entry's raw body.
func TestRenderMessages_SummaryAttachedToToolResultArtifact(t *testing.T) {
	m := MailModel{width: 100}
	out := m.renderMessages([]ChatMessage{
		{Type: "tool_result", Body: "bash → ok", ApiCallID: "api_1", Timestamp: "2026-06-08T07:08:27Z", Summary: &fs.AprioriSummary{
			Kind:                 "apriori_generated",
			ToolName:             "bash",
			Text:                 "Inline-artifact summary text.",
			OriginalVisibleChars: 12000,
			SummaryChars:         29,
		}},
	})
	if !strings.Contains(out, "bash → ok") {
		t.Fatalf("raw tool_result body missing:\n%s", out)
	}
	if !strings.Contains(out, i18n.T("mail.apriori_summary_label")) {
		t.Fatalf("summary label missing for artifact-in-result shape:\n%s", out)
	}
	if !strings.Contains(out, "Inline-artifact summary text.") {
		t.Fatalf("artifact summary text missing:\n%s", out)
	}
}

// Cap-refusal / fail-closed variants: no summary text was shown to the agent,
// so the block uses the "no summary shown" label and surfaces that the raw is
// preserved.
func TestRenderMessages_SummaryUnavailableUsesDistinctLabel(t *testing.T) {
	m := MailModel{width: 100}
	out := m.renderMessages([]ChatMessage{
		{Type: "apriori_summary", ApiCallID: "api_1", Timestamp: "2026-06-08T07:08:27Z", Summary: &fs.AprioriSummary{
			Kind:                 "apriori_cap_refused",
			ToolName:             "read",
			OriginalVisibleChars: 600000,
			Unavailable:          true,
		}},
	})
	if !strings.Contains(out, i18n.T("mail.apriori_summary_unavailable_label")) {
		t.Fatalf("cap-refusal must use the unavailable label:\n%s", out)
	}
	if strings.Contains(out, i18n.T("mail.apriori_summary_label")) {
		t.Fatalf("cap-refusal must not use the generated-summary label:\n%s", out)
	}
	if !strings.Contains(out, "600,000") {
		t.Fatalf("cap-refusal footer missing original char count:\n%s", out)
	}
}

// Lifecycle-event path WITHOUT text (pre-#3833 logs): counts are present but the
// generated text is not. The block still renders the label, a faint note, and
// the metadata so it is clear a summary was shown.
func TestRenderMessages_SummaryLifecycleNoTextStillRenders(t *testing.T) {
	m := MailModel{width: 100}
	out := m.renderMessages([]ChatMessage{
		{Type: "apriori_summary", ApiCallID: "api_1", Timestamp: "2026-06-08T07:08:27Z", Summary: &fs.AprioriSummary{
			Kind:                 "apriori_generated",
			ToolName:             "grep",
			OriginalVisibleChars: 30000,
			SummaryChars:         500,
		}},
	})
	if !strings.Contains(out, i18n.T("mail.apriori_summary_label")) {
		t.Fatalf("label missing for text-less lifecycle summary:\n%s", out)
	}
	if !strings.Contains(out, "500") {
		t.Fatalf("char counts missing for text-less lifecycle summary:\n%s", out)
	}
}

// Lifecycle-event path WITH text (kernel #3833): the generated lifecycle event
// now carries `generated_summary`, so the block must render the actual summary
// text the agent saw — not the "summary text is not in the event log" fallback.
func TestRenderMessages_SummaryLifecycleWithTextRendersActualSummary(t *testing.T) {
	m := MailModel{width: 100}
	out := m.renderMessages([]ChatMessage{
		{Type: "apriori_summary", ApiCallID: "api_1", Timestamp: "2026-06-08T07:08:27Z", Summary: &fs.AprioriSummary{
			Kind:                 "apriori_generated",
			ToolName:             "grep",
			Text:                 "Found 4 TODO markers across 3 files.",
			OriginalVisibleChars: 30000,
			SummaryChars:         500,
		}},
	})
	if !strings.Contains(out, i18n.T("mail.apriori_summary_label")) {
		t.Fatalf("label missing for lifecycle summary with text:\n%s", out)
	}
	if !strings.Contains(out, "Found 4 TODO markers across 3 files.") {
		t.Fatalf("generated summary text missing for lifecycle path:\n%s", out)
	}
	// The metadata-only fallback note must NOT appear when text is present.
	if strings.Contains(out, i18n.TF("mail.apriori_summary_no_text", "500")) {
		t.Fatalf("fallback no-text note must not render when summary text is present:\n%s", out)
	}
}

// shouldShow: the summary follows tool-result verbosity — hidden at verboseOff,
// shown from verboseThinking up.
func TestShouldShow_AprioriSummaryFollowsToolVerbosity(t *testing.T) {
	e := fs.SessionEntry{Type: "apriori_summary", Summary: &fs.AprioriSummary{Kind: "apriori_generated"}}

	m := MailModel{verbose: verboseOff}
	if m.shouldShow(e) {
		t.Errorf("apriori_summary should be hidden at verboseOff")
	}
	m.verbose = verboseThinking
	if !m.shouldShow(e) {
		t.Errorf("apriori_summary should be shown at verboseThinking")
	}
	m.verbose = verboseExtended
	if !m.shouldShow(e) {
		t.Errorf("apriori_summary should be shown at verboseExtended")
	}
}

// The summary shares the raw result's api_call_id, so it must NOT introduce a
// blank separator between the raw result and its summary.
func TestRenderMessages_SummaryStaysGroupedWithItsToolResult(t *testing.T) {
	m := MailModel{width: 100}
	out := m.renderMessages([]ChatMessage{
		{Type: "tool_result", Body: "bash → ok", ApiCallID: "api_1", Timestamp: "2026-06-08T07:08:27Z"},
		{Type: "apriori_summary", ApiCallID: "api_1", Timestamp: "2026-06-08T07:08:27Z", Summary: &fs.AprioriSummary{
			Kind: "apriori_generated", ToolName: "bash", Text: "ok summary", SummaryChars: 10,
		}},
	})
	if strings.Contains(out, "\n\n") {
		t.Fatalf("summary sharing the tool_result api_call_id must stay grouped (no blank separator):\n%q", out)
	}
}
