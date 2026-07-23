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

	m := NewMailModel(humanDir, "human", t.TempDir(), orchDir, "agent", 2000, "", "en", false, 0)
	if got := m.sessionCache.Len(); got != 0 {
		t.Fatalf("NewMailModel ingested %d session entries synchronously; expected 0 (rebuild must be deferred to Init)", got)
	}
}

// TestProjectMailStoreRunsRebuild verifies that the root store performs the deferred
// rebuild and that feeding its message into Update populates the message stream.
// This is the other half of the deferral: the work still happens, just off the
// synchronous launch path.
func TestProjectMailStoreRunsRebuild(t *testing.T) {
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

	m := NewMailModel(humanDir, "human", t.TempDir(), orchDir, "agent", 2000, "", "en", false, 0)
	m.verbose = verboseThinking

	// Run the initial rebuild command (the deferred heavy work).
	msg := acceptedInitialMailRefresh(m)
	if msg == nil {
		t.Fatal("project store initial refresh returned nil msg")
	}
	if got := m.sessionCache.Len(); got != 0 {
		t.Fatalf("project store rebuild mutated the installed session cache before acceptance; got %d entries", got)
	}
	rm, ok := msg.(mailRefreshMsg)
	if !ok || rm.sessionCache == nil || rm.sessionCache.Len() == 0 {
		t.Fatalf("project store rebuild did not return a populated command-local session cache: %#v", rm.sessionCache)
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

// TestAppInitRequestsProjectStoreRebuild verifies root Init schedules the store.
func TestAppInitRequestsProjectStoreRebuild(t *testing.T) {
	dir := t.TempDir()
	a := App{currentView: appViewMail}
	a.installMailModel(NewMailModel(dir, "human@local", dir, dir, "orch", 20, dir, "en", false, 0))
	if cmd := a.Init(); cmd == nil {
		t.Fatal("App.Init returned nil cmd; expected a project-store refresh request")
	}
	_ = tea.Batch // keep the bubbletea import meaningful even if Batch isn't referenced directly
}
