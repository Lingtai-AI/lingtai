package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/fs"
)

// Regression guard for the "context bar only after Ctrl+O" bug (Jason, follow-up
// to PR #442).
//
// Root cause: gatherHomeTelemetry resolved the live context-usage fraction by
// scanning m.messages — the VERBOSE-FILTERED display list. shouldShow() gates
// "notification" entries behind verbose >= verboseThinking, so at the normal
// home view (verboseOff) notifications are absent from m.messages and the
// "ctx N% ▓▓▓░░" segment never renders. Pressing Ctrl+O cycles verbose up,
// notifications enter m.messages, and only THEN does the bar appear.
//
// The fix sources context usage from the UNFILTERED session-cache entries (the
// same data buildMessages reads before filtering), so the bar is independent of
// verbose/Ctrl+O state.

// latestContextUsage is the pure resolver under test: it must return the
// freshest notification context-usage fraction from raw session entries, blind
// to any verbose filtering.
func TestLatestContextUsageFindsFreshestNotification(t *testing.T) {
	entries := []fs.SessionEntry{
		{Type: "notification", Ts: "2026-06-26T04:00:00Z",
			Meta: &fs.NotificationMeta{Context: &fs.NotificationMetaContext{Usage: 0.40}}},
		{Type: "text_output", Ts: "2026-06-26T04:00:30Z"},
		{Type: "notification", Ts: "2026-06-26T04:01:00Z",
			Meta: &fs.NotificationMeta{Context: &fs.NotificationMetaContext{Usage: 0.73}}},
		{Type: "tool_call", Ts: "2026-06-26T04:01:30Z"},
	}
	if got := latestContextUsage(entries); got != 0.73 {
		t.Fatalf("latestContextUsage = %v, want 0.73 (the freshest notification's usage)", got)
	}
}

func TestLatestContextUsageNoNotification(t *testing.T) {
	entries := []fs.SessionEntry{
		{Type: "text_output", Ts: "2026-06-26T04:00:00Z"},
		{Type: "notification", Ts: "2026-06-26T04:01:00Z", Meta: nil}, // pre-#40, no meta
	}
	if got := latestContextUsage(entries); got != -1 {
		t.Fatalf("latestContextUsage = %v, want -1 (no usable context block)", got)
	}
}

// The integration guard: a MailModel at verboseOff (no Ctrl+O) whose session
// cache holds a notification with context usage must report telemetry. Before
// the fix this returned false because gatherHomeTelemetry scanned the empty
// verbose-filtered m.messages; after the fix it reads the session cache.
func TestHomeTelemetryContextVisibleWithoutCtrlO(t *testing.T) {
	dir := t.TempDir()

	// An orchestrator dir with an events.jsonl carrying one notification that
	// reports context usage. No SQLite index → ingest falls back to JSONL.
	orchDir := filepath.Join(dir, "orch")
	logsDir := filepath.Join(orchDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// ts is unix seconds (parseEvent reads "ts" as a float). text is required or
	// the event is dropped; "summary" supplies it for a notification.
	event := `{"type":"notification","ts":1782000000,"summary":"sync","meta":{"context":{"usage":0.42}}}` + "\n"
	if err := os.WriteFile(filepath.Join(logsDir, "events.jsonl"), []byte(event), 0o644); err != nil {
		t.Fatal(err)
	}

	humanDir := filepath.Join(dir, "human")
	if err := os.MkdirAll(humanDir, 0o755); err != nil {
		t.Fatal(err)
	}

	m := NewMailModel(humanDir, "human@local", "~", orchDir, "TestOrch", 50, dir, "en", false, 0)
	// Drive the deferred initial rebuild so the session cache is populated from
	// events.jsonl — exactly the normal launch path, no Ctrl+O.
	msg := m.initialRebuild()
	m, _ = m.Update(msg)

	if m.verbose != verboseOff {
		t.Fatalf("test precondition: model should be at verboseOff, got %v", m.verbose)
	}

	tel := m.gatherHomeTelemetry()
	if tel.contextUsage < 0 {
		t.Fatalf("context usage not found at verboseOff: gatherHomeTelemetry must read the unfiltered session cache, not the verbose-filtered m.messages (contextUsage=%v)", tel.contextUsage)
	}
	if !m.hasHomeTelemetry() {
		t.Fatal("hasHomeTelemetry() is false at verboseOff despite a context-bearing notification in the session cache — the bar would be hidden until Ctrl+O")
	}
}

// Jason (msg 3217): the row must make its scope explicit with a localized label
// (e.g. "Current Session" / "当前会话"), via the i18n system — never hard-coded.
// The label leads the row so the user knows the metrics are session-scoped.
func TestFormatHomeTelemetryShowsLocalizedSessionLabel(t *testing.T) {
	tel := homeTelemetry{
		apiCalls: 42, sessionTokens: 181585, inputTokens: 181585,
		cached: 180224, contextLimit: 200000, contextUsage: 0.73,
	}
	got := formatHomeTelemetry(tel, 120)

	// The localized label must be present and must not leak the raw i18n key.
	label := i18n.T("mail.telemetry_session")
	if label == "mail.telemetry_session" {
		t.Fatal("i18n key mail.telemetry_session is missing a translation")
	}
	if !strings.Contains(got, label) {
		t.Errorf("telemetry row %q is missing the session-scope label %q", got, label)
	}
	if strings.Contains(got, "mail.telemetry_session") {
		t.Errorf("telemetry row %q leaked the raw i18n key", got)
	}
	// The label must lead the row (before the metrics) so the scope is read first.
	if li, ti := strings.Index(got, label), strings.Index(got, i18n.T("mail.telemetry_api")); li < 0 || (ti >= 0 && li > ti) {
		t.Errorf("session label must precede the metrics in %q (label@%d api@%d)", got, li, ti)
	}
}

// And the data must NOT regress to depending on verbose state: it is present at
// verboseOff and stays present (does not toggle) when verbose changes. This pins
// the decoupling so a future change can't re-tie the bar to Ctrl+O.
func TestHomeTelemetryContextStableAcrossVerbose(t *testing.T) {
	dir := t.TempDir()
	orchDir := filepath.Join(dir, "orch")
	logsDir := filepath.Join(orchDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	event := `{"type":"notification","ts":1782000000,"summary":"sync","meta":{"context":{"usage":0.55}}}` + "\n"
	if err := os.WriteFile(filepath.Join(logsDir, "events.jsonl"), []byte(event), 0o644); err != nil {
		t.Fatal(err)
	}
	humanDir := filepath.Join(dir, "human")
	if err := os.MkdirAll(humanDir, 0o755); err != nil {
		t.Fatal(err)
	}

	m := NewMailModel(humanDir, "human@local", "~", orchDir, "TestOrch", 50, dir, "en", false, 0)
	m, _ = m.Update(m.initialRebuild())
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})

	atOff := m.gatherHomeTelemetry().contextUsage

	// Simulate Ctrl+O (verbose up) + its refresh; the context value must be the
	// same — Ctrl+O must neither reveal nor change it.
	m.verbose = verboseThinking
	m.buildMessages()
	atThinking := m.gatherHomeTelemetry().contextUsage

	if atOff < 0 {
		t.Fatalf("context usage absent at verboseOff (%v)", atOff)
	}
	if atOff != atThinking {
		t.Fatalf("context usage changed with verbose: off=%v thinking=%v (must be identical — decoupled from Ctrl+O)", atOff, atThinking)
	}
}
