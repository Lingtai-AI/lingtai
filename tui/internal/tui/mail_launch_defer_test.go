package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestNewMailModelDefersSessionRebuild guards the launch-performance contract:
// NewMailModel must NOT read and parse the full events.jsonl / soul_inquiry.jsonl
// / soul_flow.jsonl history synchronously inside the constructor. That work runs
// on the synchronous launch path (NewApp -> before tea.Program.Run), so on
// content-heavy projects it blocks the first frame for as long as it takes to
// parse the entire log. The rebuild is deferred to a command driven by Init().
//
// The observable contract: immediately after construction the session cache is
// empty (no historical ingest has happened yet).
func TestNewMailModelDefersSessionRebuild(t *testing.T) {
	humanDir := t.TempDir()
	orchDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(orchDir, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	events := strings.Join([]string{
		`{"ts":1781300000,"type":"llm_call","api_call_id":"api_one"}`,
		`{"ts":1781300001,"type":"text_output","text":"answer"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(orchDir, "logs", "events.jsonl"), []byte(events), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewMailModel(humanDir, "human", t.TempDir(), orchDir, "agent", unlimitedPageSize, "", "en", false, 0)
	if got := m.sessionCache.Len(); got != 0 {
		t.Fatalf("NewMailModel ingested %d session entries synchronously; expected 0 (rebuild must be deferred to Init)", got)
	}
}

// TestMailInitRunsRebuild verifies that Init()'s command performs the deferred
// rebuild and that feeding its message into Update populates the message stream.
// This is the other half of the deferral: the work still happens, just off the
// synchronous launch path.
func TestMailInitRunsRebuild(t *testing.T) {
	humanDir := t.TempDir()
	orchDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(orchDir, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	events := strings.Join([]string{
		`{"ts":1781300000,"type":"llm_response","api_call_id":"api_one"}`,
		`{"ts":1781300001,"type":"text_output","text":"deferred answer"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(orchDir, "logs", "events.jsonl"), []byte(events), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewMailModel(humanDir, "human", t.TempDir(), orchDir, "agent", unlimitedPageSize, "", "en", false, 0)
	m.verbose = verboseThinking

	// Run the initial rebuild command (the deferred heavy work).
	msg := m.initialRebuild()
	if msg == nil {
		t.Fatal("initialRebuild returned nil msg")
	}
	if got := m.sessionCache.Len(); got != 0 {
		t.Fatalf("initialRebuild mutated the installed session cache before acceptance; got %d entries", got)
	}
	rm, ok := msg.(mailRefreshMsg)
	if !ok || rm.sessionCache == nil || rm.sessionCache.Len() == 0 {
		t.Fatalf("initialRebuild did not return a populated command-local session cache: %#v", rm.sessionCache)
	}

	// Feed the resulting message through Update — acceptance installs the rebuilt
	// cache and the view should now build.
	updated, _ := m.Update(msg)
	found := false
	for _, cm := range updated.messages {
		if cm.Type == "text_output" && strings.Contains(cm.Body, "deferred answer") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the deferred answer in built messages after Init rebuild; got %d messages", len(updated.messages))
	}
}

// TestMailInitIncludesRebuildCmd verifies Init() actually schedules the rebuild.
func TestMailInitIncludesRebuildCmd(t *testing.T) {
	dir := t.TempDir()
	m := NewMailModel(dir, "human@local", dir, dir, "orch", 20, dir, "en", false, 0)
	if cmd := m.Init(); cmd == nil {
		t.Fatal("MailModel.Init returned nil cmd; expected at least the rebuild + refresh batch")
	}
	_ = tea.Batch // keep the bubbletea import meaningful even if Batch isn't referenced directly
}
