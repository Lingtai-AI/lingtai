package tui

import (
	"strings"
	"testing"
)

// First Ctrl+O layer (verboseThinking): a thinking block longer than 150 runes
// is rendered as a 150-char preview with a clear ellipsis indicator, not in
// full. The deeper layer (verboseExtended) keeps the full thinking body.
func TestRenderMessages_ThinkingBlockPreviewedInFirstLayer(t *testing.T) {
	long := strings.Repeat("A", 200) + "TAIL_MARKER"
	msgs := []ChatMessage{
		{Type: "thinking", ApiCallID: "api_1", Timestamp: "2026-06-08T07:08:27Z", Body: long},
	}

	// First layer: preview only (first 150 chars, ellipsis, no tail).
	mFirst := MailModel{width: 100, verbose: verboseThinking}
	first := mFirst.renderMessages(msgs)
	if strings.Contains(first, "TAIL_MARKER") {
		t.Fatalf("first Ctrl+O layer must not render the full thinking tail:\n%s", first)
	}
	if !strings.Contains(first, "…") {
		t.Fatalf("first-layer preview of a >150-char thinking block must show an ellipsis indicator:\n%s", first)
	}

	// Deeper layer: full text.
	mDeep := MailModel{width: 100, verbose: verboseExtended}
	deep := mDeep.renderMessages(msgs)
	if !strings.Contains(deep, "TAIL_MARKER") {
		t.Fatalf("deeper Ctrl+O layer must render the full thinking body:\n%s", deep)
	}
}

// A thinking block at or under 150 runes is never truncated and gets no
// misleading ellipsis in the first layer.
func TestRenderMessages_ShortThinkingNotTruncatedInFirstLayer(t *testing.T) {
	short := "Should we use the cheaper preset for this batch?"
	mFirst := MailModel{width: 100, verbose: verboseThinking}
	out := mFirst.renderMessages([]ChatMessage{
		{Type: "thinking", ApiCallID: "api_1", Timestamp: "2026-06-08T07:08:27Z", Body: short},
	})
	if !strings.Contains(out, short) {
		t.Fatalf("short thinking must render in full in the first layer:\n%s", out)
	}
	if strings.Contains(out, "…") {
		t.Fatalf("short thinking must not show a misleading ellipsis in the first layer:\n%s", out)
	}
}

// Diary and text blocks are intentionally NOT preview-truncated at level 1:
// only thinking blocks carry the 150-char preview.
func TestRenderMessages_DiaryAndTextNotPreviewTruncatedInFirstLayer(t *testing.T) {
	long := strings.Repeat("B", 200) + "DIARY_TAIL_MARKER"
	for _, typ := range []string{"diary", "text_input", "text_output"} {
		mFirst := MailModel{width: 100, verbose: verboseThinking}
		out := mFirst.renderMessages([]ChatMessage{
			{Type: typ, ApiCallID: "api_1", Timestamp: "2026-06-08T07:08:27Z", Body: long},
		})
		if !strings.Contains(out, "DIARY_TAIL_MARKER") {
			t.Fatalf("%s must render in full at first layer (not preview-truncated):\n%s", typ, out)
		}
	}
}
