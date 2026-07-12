package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/fs"
)

// The llm_response event carries the per-round token scalars and sits at the
// TOP of its api_call_id group in the event stream (llm_call → llm_response →
// tool_call/tool_result). Jason wants the usage line at the BOTTOM of the API
// call group, so renderMessages defers the footer to the end of the group.
func TestRenderMessages_TokenFooterAtBottomOfApiCallGroup(t *testing.T) {
	m := MailModel{width: 120, verbose: verboseThinking}
	out := m.renderMessages([]ChatMessage{
		// group api_one: the llm_response carrier comes first in stream order
		{Type: "llm_response", ApiCallID: "api_one", TokenUsage: &fs.TokenUsage{Input: 181585, Output: 2275, Cached: 180224}},
		{Type: "tool_call", Body: "bash({})", ApiCallID: "api_one", Timestamp: "2026-06-08T07:08:26Z"},
		{Type: "tool_result", Body: "bash → ok", ApiCallID: "api_one", Timestamp: "2026-06-08T07:08:27Z"},
		// group api_two
		{Type: "tool_call", Body: "read({})", ApiCallID: "api_two", Timestamp: "2026-06-08T07:08:28Z"},
	})

	// The footer must appear, and AFTER the tool lines of its group (bottom).
	footerNeedles := []string{"181.6k", "1.4k", "2.3k", "99.3%"}
	for _, n := range footerNeedles {
		if !strings.Contains(out, n) {
			t.Fatalf("output missing token footer fragment %q:\n%s", n, out)
		}
	}
	idxResult := strings.Index(out, "bash → ok")
	idxFooter := strings.Index(out, "99.3%")
	idxNextGroup := strings.Index(out, "read({})")
	if idxResult < 0 || idxFooter < 0 || idxNextGroup < 0 {
		t.Fatalf("missing markers: result=%d footer=%d next=%d\n%s", idxResult, idxFooter, idxNextGroup, out)
	}
	if !(idxResult < idxFooter) {
		t.Errorf("token footer should be BELOW the tool_result of its group (result=%d footer=%d):\n%s", idxResult, idxFooter, out)
	}
	if !(idxFooter < idxNextGroup) {
		t.Errorf("token footer should be ABOVE the next api group (footer=%d next=%d):\n%s", idxFooter, idxNextGroup, out)
	}
	// The llm_response carrier must not render as a raw "[llm_response]" block.
	if strings.Contains(out, "[llm_response]") {
		t.Errorf("llm_response must not render as a raw block:\n%s", out)
	}
}

// At verboseOff (normal mail) nothing verbose shows, including the footer.
func TestRenderMessages_TokenFooterHiddenAtVerboseOff(t *testing.T) {
	m := MailModel{width: 120, verbose: verboseOff}
	out := m.renderMessages([]ChatMessage{
		{Type: "llm_response", ApiCallID: "api_one", TokenUsage: &fs.TokenUsage{Input: 1000, Output: 200, Cached: 800}},
		{Type: "tool_call", Body: "bash({})", ApiCallID: "api_one"},
	})
	if strings.Contains(out, "cache rate") || strings.Contains(out, "80.0%") {
		t.Errorf("token footer should be hidden at verboseOff:\n%s", out)
	}
}

// The footer also shows at the second verbose layer (verboseExtended), since it
// is gated at verboseThinking and above.
func TestRenderMessages_TokenFooterShowsAtVerboseExtended(t *testing.T) {
	m := MailModel{width: 120, verbose: verboseExtended}
	out := m.renderMessages([]ChatMessage{
		{Type: "llm_response", ApiCallID: "api_one", TokenUsage: &fs.TokenUsage{Input: 1000, Output: 200, Cached: 800}},
		{Type: "tool_result", Body: "bash → ok", ApiCallID: "api_one"},
	})
	if !strings.Contains(out, "80.0%") {
		t.Errorf("token footer should show at verboseExtended:\n%s", out)
	}
}

// End-to-end through buildMessages: an llm_response with token fields surfaces a
// footer at the bottom of its group, while the noisy _meta hint behavior from
// PR #440 is preserved and no _meta envelope leaks into the replay.
func TestBuildMessages_LLMResponseTokenFooterAndMetaStillHidden(t *testing.T) {
	humanDir := t.TempDir()
	orchDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(orchDir, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	events := strings.Join([]string{
		`{"ts":1781300000,"type":"llm_call","api_call_id":"api_one"}`,
		`{"ts":1781300001,"type":"llm_response","api_call_id":"api_one","input_tokens":1000,"output_tokens":200,"cached_tokens":800,"estimated":false}`,
		`{"ts":1781300002,"type":"tool_call","api_call_id":"api_one","tool_name":"bash","tool_args":"{}"}`,
		`{"ts":1781300003,"type":"tool_result","api_call_id":"api_one","tool_name":"bash","status":"ok","elapsed_ms":5,"result":{"stdout":"done"},"_runtime":{"guidance":{"guidance_version":"0.3.0"}}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(orchDir, "logs", "events.jsonl"), []byte(events), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewMailModel(humanDir, "human", t.TempDir(), orchDir, "agent", 2000, "", "en", false, 0)
	m.verbose = verboseThinking
	m.width = 120
	m.buildMessages()
	out := m.renderMessages(m.messages)

	// Token footer present.
	if !strings.Contains(out, "80.0%") {
		t.Fatalf("expected token footer (cache rate 80.0%%) in replay:\n%s", out)
	}
	// #440 behavior preserved: the meta hint shows, full meta does NOT leak.
	if strings.Contains(out, "guidance_version") || strings.Contains(out, "_runtime.guidance:") {
		t.Errorf("the _meta envelope must stay hidden (#440):\n%s", out)
	}
}
