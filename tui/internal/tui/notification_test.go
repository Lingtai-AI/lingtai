package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestDefaultCommandsIncludesNotification(t *testing.T) {
	cmd, ok := findCommand("notification")
	if !ok {
		t.Fatal("DefaultCommands() missing notification command")
	}
	if cmd.Description != "palette.notification" || cmd.Detail != "cmd.notification" {
		t.Fatalf("notification command keys = (%q, %q), want (palette.notification, cmd.notification)", cmd.Description, cmd.Detail)
	}
}

func TestNotificationCommandOpensNotificationView(t *testing.T) {
	agentDir := t.TempDir()
	app := App{orchDir: agentDir, projectDir: t.TempDir()}
	model, _ := app.switchToView("notification")
	got := model.(App)
	if got.currentView != appViewNotification {
		t.Fatalf("switchToView(%q) currentView = %v, want appViewNotification", "notification", got.currentView)
	}
	if got.notification.agentDir != agentDir {
		t.Fatalf("notification.agentDir = %q, want %q", got.notification.agentDir, agentDir)
	}
}

// TestNotificationModelNoSQLite checks graceful degradation when sqlite sidecar
// is absent: the model initializes without panic and View() returns a message.
func TestNotificationModelNoSQLite(t *testing.T) {
	agentDir := t.TempDir()
	m := NewNotificationModel(agentDir)
	if m.agentDir != agentDir {
		t.Fatalf("agentDir = %q, want %q", m.agentDir, agentDir)
	}
	view := m.View()
	if view == "" {
		t.Fatal("View() returned empty string")
	}
	if !strings.Contains(view, "log.sqlite") {
		t.Fatalf("View() did not mention log.sqlite: %s", view)
	}
}

// TestNotificationModelNoSnapshots checks graceful display when sqlite exists
// but has no notification_block_injected rows. The fallback message must make
// old-log reality clear rather than silently showing nothing.
func TestNotificationModelNoSnapshots(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires sqlite3 binary (POSIX only)")
	}
	bin, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 not in PATH")
	}

	agentDir := t.TempDir()
	logsDir := filepath.Join(agentDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(logsDir, "log.sqlite")
	// Only legacy notification_pair_injected rows (no actual snapshots)
	sql := `CREATE TABLE events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		ts REAL NOT NULL,
		type TEXT NOT NULL,
		agent_address TEXT,
		fields_json TEXT NOT NULL DEFAULT '{}',
		source_file TEXT,
		source_offset INTEGER,
		source_line INTEGER,
		source_kind TEXT,
		scope TEXT,
		run_id TEXT,
		inserted_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
	);
	INSERT INTO events(ts,type,fields_json) VALUES(1000.0,'notification_pair_injected','{"sources":["email"],"summary":"old-style"}');`
	if out, err := exec.Command(bin, db, sql).CombinedOutput(); err != nil {
		t.Fatalf("createDB: %v\n%s", err, out)
	}

	m := NewNotificationModel(agentDir)
	if m.cursor != -1 {
		t.Fatalf("cursor should be -1 when no snapshots, got %d", m.cursor)
	}
	view := m.View()
	if !strings.Contains(view, "notification_block_injected") {
		t.Fatalf("View() should mention notification_block_injected in fallback: %s", view)
	}
	if !strings.Contains(view, "No persisted") {
		t.Fatalf("View() should show no-snapshots message: %s", view)
	}
}

// TestNotificationModelNoBlocks is a backward-compat alias for TestNotificationModelNoSnapshots.
func TestNotificationModelNoBlocks(t *testing.T) {
	TestNotificationModelNoSnapshots(t)
}

// TestNotificationModelWithSnapshots exercises snapshot loading and renders actual block content.
func TestNotificationModelWithSnapshots(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires sqlite3 binary (POSIX only)")
	}
	bin, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 not in PATH")
	}

	// Build snapshots with real payload shape the kernel emits
	snapshotFields := []string{
		`{"mode":"synthetic_notification_pair","call_id":"notif_abc","sources":["email"],"payload":{"_notification_guidance":"kernel guidance text","notifications":{"email":{"data":{"count":1},"_notification_guidance":"email channel guidance"}}},"meta":{"stamina_left_seconds":3600,"injection_seq":1}}`,
		`{"mode":"synthetic_notification_pair","call_id":"notif_def","sources":["soul"],"payload":{"_notification_guidance":"kernel guidance 2","notifications":{"soul":{"data":{"voices":[]},"_notification_guidance":"soul channel guidance"}}},"meta":{}}`,
		`{"mode":"active_tool_result","call_id":"","sources":["email","system"],"payload":{"_notification_guidance":"kernel guidance 3","notifications":{"email":{"data":{"count":2}},"system":{"events":[{"body":"ping"}]}}},"meta":{"injection_seq":2}}`,
	}
	agentDir := makeNotificationSnapshotDB(t, bin, snapshotFields)

	m := NewNotificationModel(agentDir)
	if len(m.snapshots) != 3 {
		t.Fatalf("expected 3 snapshots, got %d", len(m.snapshots))
	}
	// cursor=0 is newest (third snapshot = active_tool_result)
	if m.snapshots[0].Mode != "active_tool_result" {
		t.Fatalf("expected newest snapshot first (active_tool_result), got mode=%q", m.snapshots[0].Mode)
	}
	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", m.cursor)
	}

	m.width = 100
	m.height = 30
	view := m.View()

	// The visible viewport should expose the block picker in the left pane.
	for _, want := range []string{"all blocks", "overview", "notifications.email", "notifications.system", "←/→ snapshots"} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() should show sidebar/hint %q: %s", want, view)
		}
	}

	// Full snapshot content now lives in the scrollable MarkdownViewer entry.
	content := notificationTestEntryContent(t, m, "all blocks")
	for _, want := range []string{"email", "system", "kernel guidance 3", "snapshot 1 of 3"} {
		if !strings.Contains(content, want) {
			t.Fatalf("all blocks entry should contain %q: %s", want, content)
		}
	}
}

// TestNotificationModelWithBlocks delegates to TestNotificationModelWithSnapshots
// for backward compatibility with test names.
func TestNotificationModelWithBlocks(t *testing.T) {
	TestNotificationModelWithSnapshots(t)
}

// TestNotificationModelNavigation checks left/right key navigation among snapshots.
func TestNotificationModelNavigation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires sqlite3 binary (POSIX only)")
	}
	bin, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 not in PATH")
	}

	snapshotFields := []string{
		`{"mode":"synthetic_notification_pair","sources":["email"],"payload":{"_notification_guidance":"A","notifications":{"email":{}}}}`,
		`{"mode":"synthetic_notification_pair","sources":["soul"],"payload":{"_notification_guidance":"B","notifications":{"soul":{}}}}`,
		`{"mode":"active_tool_result","sources":["system"],"payload":{"_notification_guidance":"C","notifications":{"system":{}}}}`,
	}
	agentDir := makeNotificationSnapshotDB(t, bin, snapshotFields)

	m := NewNotificationModel(agentDir)
	// Start at newest (index 0 = "C" / active_tool_result)
	if m.snapshots[m.cursor].NotificationGuidance != "C" {
		t.Fatalf("expected guidance C at start, got %q", m.snapshots[m.cursor].NotificationGuidance)
	}

	// left → older (index 1 = "B")
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.snapshots[m.cursor].NotificationGuidance != "B" {
		t.Fatalf("after left: expected guidance B, got %q", m.snapshots[m.cursor].NotificationGuidance)
	}

	// left → older (index 2 = "A")
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.snapshots[m.cursor].NotificationGuidance != "A" {
		t.Fatalf("after second left: expected guidance A, got %q", m.snapshots[m.cursor].NotificationGuidance)
	}

	// left at end should stay on "A"
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.snapshots[m.cursor].NotificationGuidance != "A" {
		t.Fatalf("left at oldest should stay: got %q", m.snapshots[m.cursor].NotificationGuidance)
	}

	// right → newer (index 1 = "B")
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.snapshots[m.cursor].NotificationGuidance != "B" {
		t.Fatalf("after right: expected guidance B, got %q", m.snapshots[m.cursor].NotificationGuidance)
	}

	// right → newest (index 0 = "C")
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.snapshots[m.cursor].NotificationGuidance != "C" {
		t.Fatalf("after second right: expected guidance C, got %q", m.snapshots[m.cursor].NotificationGuidance)
	}

	// right at newest should stay
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.snapshots[m.cursor].NotificationGuidance != "C" {
		t.Fatalf("right at newest should stay: got %q", m.snapshots[m.cursor].NotificationGuidance)
	}
}

// TestNotificationModelEscBack checks that esc emits ViewChangeMsg{View:"mail"}.
func TestNotificationModelEscBack(t *testing.T) {
	m := NewNotificationModel(t.TempDir())

	var gotMsg tea.Msg
	m2, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	_ = m2
	if cmd == nil {
		t.Fatal("esc should return a cmd")
	}
	gotMsg = cmd()
	vc, ok := gotMsg.(ViewChangeMsg)
	if !ok {
		t.Fatalf("esc cmd returned %T, want ViewChangeMsg", gotMsg)
	}
	if vc.View != "mail" {
		t.Fatalf("ViewChangeMsg.View = %q, want mail", vc.View)
	}
}

// TestNotificationModelQBack checks that q also emits ViewChangeMsg{View:"mail"}.
func TestNotificationModelQBack(t *testing.T) {
	m := NewNotificationModel(t.TempDir())
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd == nil {
		t.Fatal("q should return a cmd")
	}
	msg := cmd()
	vc, ok := msg.(ViewChangeMsg)
	if !ok {
		t.Fatalf("q cmd returned %T, want ViewChangeMsg", msg)
	}
	if vc.View != "mail" {
		t.Fatalf("ViewChangeMsg.View = %q, want mail", vc.View)
	}
}

// TestNotificationModelBackspaceBack checks that backspace also emits ViewChangeMsg{View:"mail"}.
func TestNotificationModelBackspaceBack(t *testing.T) {
	m := NewNotificationModel(t.TempDir())
	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyBackspace}))
	if cmd == nil {
		t.Fatal("expected backspace to emit a ViewChangeMsg command")
	}
	msg := cmd()
	vc, ok := msg.(ViewChangeMsg)
	if !ok {
		t.Fatalf("backspace cmd returned %T, want ViewChangeMsg", msg)
	}
	if vc.View != "mail" {
		t.Fatalf("ViewChangeMsg.View = %q, want mail", vc.View)
	}
}

// TestNotificationModelLatest10Limit verifies only 10 snapshots loaded when more exist.
func TestNotificationModelLatest10Limit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires sqlite3 binary (POSIX only)")
	}
	bin, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 not in PATH")
	}

	fields := make([]string, 12)
	for i := range fields {
		fields[i] = fmt.Sprintf(
			`{"mode":"synthetic_notification_pair","sources":["email"],"payload":{"_notification_guidance":"guidance%d","notifications":{"email":{}}}}`,
			i,
		)
	}
	agentDir := makeNotificationSnapshotDB(t, bin, fields)

	m := NewNotificationModel(agentDir)
	if len(m.snapshots) != 10 {
		t.Fatalf("expected 10 snapshots (limit), got %d", len(m.snapshots))
	}
	// newest snapshot should be guidance11
	if m.snapshots[0].NotificationGuidance != "guidance11" {
		t.Fatalf("expected newest snapshot guidance11, got %q", m.snapshots[0].NotificationGuidance)
	}
}

// TestNotificationModelSnapshotRendersChannelContent checks that the render shows
// per-channel payload content and global guidance from actual notification_block_injected rows.
func TestNotificationModelSnapshotRendersChannelContent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires sqlite3 binary (POSIX only)")
	}
	bin, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 not in PATH")
	}

	fields := []string{
		`{"mode":"synthetic_notification_pair","call_id":"notif_xyz","sources":["email","system"],"payload":{"_notification_guidance":"global kernel guidance","notifications":{"email":{"data":{"count":3},"_notification_guidance":"email per-channel guidance"},"system":{"events":[{"body":"test event"}],"_notification_guidance":"system per-channel guidance"}}},"meta":{"injection_seq":5,"stamina_left_seconds":7200}}`,
	}
	agentDir := makeNotificationSnapshotDB(t, bin, fields)

	m := NewNotificationModel(agentDir)
	m.width = 120
	m.height = 40
	view := m.View()
	for _, want := range []string{"all blocks", "_meta.notification_guidance", "_meta.notifications.email", "_meta.notifications.system"} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() should show sidebar block %q: %s", want, view)
		}
	}

	content := notificationTestEntryContent(t, m, "all blocks")
	checks := []string{
		"global kernel guidance",
		"email",
		"system",
		"synthetic_notification_pair",
		"notif_xyz",
		"seq 5",
	}
	for _, want := range checks {
		if !strings.Contains(content, want) {
			t.Fatalf("all blocks entry should contain %q: %s", want, content)
		}
	}
}

// TestNotificationModelSnapshotRendersMetaEnvelopeBlocks checks the modern
// `_meta` envelope shape: tool_meta, agent_meta, guidance, and notifications /
// notification_guidance are rendered as readable `_meta.*` sections.
func TestNotificationModelSnapshotRendersMetaEnvelopeBlocks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires sqlite3 binary (POSIX only)")
	}
	bin, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 not in PATH")
	}

	fields := []string{
		`{"mode":"active_tool_result","call_id":"notif_tool","sources":["system"],"_meta":{"tool_meta":{"id":"call_read_123","timestamp":"2026-06-21T07:00:00Z","char_count":1200,"elapsed_ms":42},"agent_meta":{"current_time":"2026-06-21T07:00:00Z","stamina_left_seconds":1234,"active_turn_tool_calls":7,"context":{"usage":0.42,"history_tokens":12345}},"guidance":{"guidance_version":"1","meta_readme":{"tool_meta":"per-result"},"sections":[{"title":"Large result guidance","body":"Summarize the result after reading it."}]},"notification_guidance":"Handle channel payloads through their producer tools.","notifications":{"system":{"events":[{"body":"test event"}]}}}}`,
	}
	agentDir := makeNotificationSnapshotDB(t, bin, fields)

	m := NewNotificationModel(agentDir)
	m.width = 120
	m.height = 50
	view := m.View()
	for _, want := range []string{"all blocks", "_meta.tool_meta", "_meta.agent_meta", "_meta.guidance", "_meta.notification_guidance", "_meta.notifications.system"} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() should show sidebar block %q: %s", want, view)
		}
	}
	content := notificationTestEntryContent(t, m, "all blocks")

	checks := []string{
		"_meta.tool_meta",
		"call_read_123",
		"char_count",
		"_meta.agent_meta",
		"active_turn_tool_calls",
		"7",
		"_meta.guidance",
		"Large result guidance",
		"Summarize the result after reading it.",
		"meta_readme",
		"_meta.notification_guidance",
		"Handle channel payloads through their producer tools.",
		"_meta.notifications",
		"system",
	}
	for _, want := range checks {
		if !strings.Contains(content, want) {
			t.Fatalf("all blocks entry should contain %q: %s", want, content)
		}
	}
}

// TestNotificationModelSnapshotRendersLegacyPayloadBlocks checks that older
// rows carrying the pre-envelope `payload`/`meta` shape still render through
// the legacy fallback, mapped onto the same `_meta.*` display labels.
func TestNotificationModelSnapshotRendersLegacyPayloadBlocks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires sqlite3 binary (POSIX only)")
	}
	bin, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 not in PATH")
	}

	fields := []string{
		`{"mode":"active_tool_result","call_id":"notif_tool","sources":["system"],"payload":{"_tool":{"tool_name":"read","tool_call_id":"call_read_123","status":"ok","elapsed_ms":42},"_runtime":{"state":{"current_time":"2026-06-21T07:00:00Z","active_turn_tool_calls":7,"context":{"usage":0.42}},"guidance":{"title":"Large result guidance","body":"Summarize the result after reading it."}},"notification_guidance":"Handle channel payloads through their producer tools.","notifications":{"system":{"events":[{"body":"test event"}]}}},"meta":{"current_time":"2026-06-21T07:00:00Z","stamina_left_seconds":4321,"injection_seq":9}}`,
	}
	agentDir := makeNotificationSnapshotDB(t, bin, fields)

	m := NewNotificationModel(agentDir)
	m.width = 120
	m.height = 50
	view := m.View()
	for _, want := range []string{"all blocks", "_meta.tool_meta", "_meta.agent_meta", "_meta.guidance", "_meta.notification_guidance", "_meta.notifications.system"} {
		if !strings.Contains(view, want) {
			t.Fatalf("legacy View() should show sidebar block %q: %s", want, view)
		}
	}
	content := notificationTestEntryContent(t, m, "all blocks")

	checks := []string{
		"call_read_123",
		"active_turn_tool_calls",
		"Large result guidance",
		"Handle channel payloads through their producer tools.",
		"system",
		"seq 9",
	}
	for _, want := range checks {
		if !strings.Contains(content, want) {
			t.Fatalf("legacy all blocks entry should contain %q: %s", want, content)
		}
	}
}

func TestNotificationModelMarkdownViewerScrolls(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires sqlite3 binary (POSIX only)")
	}
	bin, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 not in PATH")
	}

	longBody := strings.Repeat("scroll-line\n", 80)
	fields := fmt.Sprintf(`{"mode":"active_tool_result","call_id":"notif_scroll","payload":{"_notification_guidance":%q,"notifications":{"system":{"events":[{"body":%q}]}}},"meta":{"injection_seq":1}}`, longBody, longBody)
	agentDir := makeNotificationSnapshotDB(t, bin, []string{fields})

	m := NewNotificationModel(agentDir)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 10})
	if !strings.Contains(m.View(), "pgup/pgdn scroll") {
		t.Fatalf("notification view should advertise scrolling: %s", m.View())
	}

	before := m.viewer.rightVP.YOffset()
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if got := m.viewer.rightVP.YOffset(); got <= before {
		t.Fatalf("expected page down to scroll right markdown viewport, before=%d after=%d", before, got)
	}
}

func notificationTestEntryContent(t *testing.T, m NotificationModel, label string) string {
	t.Helper()
	for _, entry := range m.viewer.entries {
		if entry.Label == label {
			return entry.Content
		}
	}
	t.Fatalf("notification entry %q not found; entries=%v", label, m.viewer.entries)
	return ""
}

// makeNotificationSnapshotDB is a test helper that inserts notification_block_injected
// rows with the given fields_json strings and returns the agent dir.
func makeNotificationSnapshotDB(t *testing.T, bin string, fieldsJSONs []string) string {
	t.Helper()
	agentDir := t.TempDir()
	logsDir := filepath.Join(agentDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(logsDir, "log.sqlite")
	sql := `CREATE TABLE events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		ts REAL NOT NULL,
		type TEXT NOT NULL,
		agent_address TEXT,
		fields_json TEXT NOT NULL DEFAULT '{}',
		source_file TEXT,
		source_offset INTEGER,
		source_line INTEGER,
		source_kind TEXT,
		scope TEXT,
		run_id TEXT,
		inserted_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
	);`
	for i, fj := range fieldsJSONs {
		sql += fmt.Sprintf(
			"\nINSERT INTO events(ts,type,fields_json) VALUES(%d.0,'notification_block_injected','%s');",
			1000+i, fj,
		)
	}
	if out, err := exec.Command(bin, db, sql).CombinedOutput(); err != nil {
		t.Fatalf("makeNotificationSnapshotDB: %v\n%s", err, out)
	}
	return agentDir
}

// makeNotificationDB is a legacy helper retained for existing tests that
// insert notification_pair_injected rows to test the query layer directly.
func makeNotificationDB(t *testing.T, bin string, fieldsJSONs []string) string {
	t.Helper()
	agentDir := t.TempDir()
	logsDir := filepath.Join(agentDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(logsDir, "log.sqlite")
	sql := `CREATE TABLE events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		ts REAL NOT NULL,
		type TEXT NOT NULL,
		agent_address TEXT,
		fields_json TEXT NOT NULL DEFAULT '{}',
		source_file TEXT,
		source_offset INTEGER,
		source_line INTEGER,
		source_kind TEXT,
		scope TEXT,
		run_id TEXT,
		inserted_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
	);`
	for i, fj := range fieldsJSONs {
		sql += fmt.Sprintf(
			"\nINSERT INTO events(ts,type,fields_json) VALUES(%d.0,'notification_pair_injected','%s');",
			1000+i, fj,
		)
	}
	if out, err := exec.Command(bin, db, sql).CombinedOutput(); err != nil {
		t.Fatalf("makeNotificationDB: %v\n%s", err, out)
	}
	return agentDir
}
