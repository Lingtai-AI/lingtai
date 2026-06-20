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

// TestNotificationModelNoBlocks checks graceful display when sqlite exists
// but has no notification_pair_injected rows.
func TestNotificationModelNoBlocks(t *testing.T) {
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
	// Only non-block notification rows
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
	INSERT INTO events(ts,type,fields_json) VALUES(1000.0,'email_notification_published','{"count":1}');`
	if out, err := exec.Command(bin, db, sql).CombinedOutput(); err != nil {
		t.Fatalf("createDB: %v\n%s", err, out)
	}

	m := NewNotificationModel(agentDir)
	if m.cursor != -1 {
		t.Fatalf("cursor should be -1 when no blocks, got %d", m.cursor)
	}
	view := m.View()
	if !strings.Contains(view, "No notification") {
		t.Fatalf("View() should show no-blocks message: %s", view)
	}
}

// TestNotificationModelWithBlocks exercises block loading and navigation.
func TestNotificationModelWithBlocks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires sqlite3 binary (POSIX only)")
	}
	bin, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 not in PATH")
	}

	agentDir := makeNotificationDB(t, bin, []string{
		`{"sources":["email"],"summary":"first block"}`,
		`{"sources":["soul"],"summary":"second block"}`,
		`{"sources":["email","soul"],"summary":"third block","meta":{"stamina_left_seconds":3600,"injection_seq":1}}`,
	})

	m := NewNotificationModel(agentDir)
	if len(m.blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(m.blocks))
	}
	// cursor=0 is newest (third block)
	if !strings.Contains(m.blocks[0].Summary, "third") {
		t.Fatalf("expected newest block first, got summary=%q", m.blocks[0].Summary)
	}
	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", m.cursor)
	}

	// View shows body text
	m.width = 100
	m.height = 30
	view := m.View()
	if !strings.Contains(view, "third block") {
		t.Fatalf("View() does not show summary: %s", view)
	}
	if !strings.Contains(view, "block 1 of 3") {
		t.Fatalf("View() does not show block counter: %s", view)
	}
}

// TestNotificationModelNavigation checks left/right key navigation among blocks.
func TestNotificationModelNavigation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires sqlite3 binary (POSIX only)")
	}
	bin, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 not in PATH")
	}

	agentDir := makeNotificationDB(t, bin, []string{
		`{"summary":"block A"}`,
		`{"summary":"block B"}`,
		`{"summary":"block C"}`,
	})

	m := NewNotificationModel(agentDir)
	// Start at newest (index 0 = block C)
	if m.blocks[m.cursor].Summary != "block C" {
		t.Fatalf("expected block C at start, got %q", m.blocks[m.cursor].Summary)
	}

	// left → older (index 1 = block B)
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.blocks[m.cursor].Summary != "block B" {
		t.Fatalf("after left: expected block B, got %q", m.blocks[m.cursor].Summary)
	}

	// left → older (index 2 = block A)
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.blocks[m.cursor].Summary != "block A" {
		t.Fatalf("after second left: expected block A, got %q", m.blocks[m.cursor].Summary)
	}

	// left at end should stay on block A
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.blocks[m.cursor].Summary != "block A" {
		t.Fatalf("left at oldest should stay: got %q", m.blocks[m.cursor].Summary)
	}

	// right → newer (index 1 = block B)
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.blocks[m.cursor].Summary != "block B" {
		t.Fatalf("after right: expected block B, got %q", m.blocks[m.cursor].Summary)
	}

	// right → newest (index 0 = block C)
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.blocks[m.cursor].Summary != "block C" {
		t.Fatalf("after second right: expected block C, got %q", m.blocks[m.cursor].Summary)
	}

	// right at newest should stay
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.blocks[m.cursor].Summary != "block C" {
		t.Fatalf("right at newest should stay: got %q", m.blocks[m.cursor].Summary)
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

// TestNotificationModelLatest10Limit verifies only 10 blocks loaded when more exist.
func TestNotificationModelLatest10Limit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires sqlite3 binary (POSIX only)")
	}
	bin, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 not in PATH")
	}

	summaries := make([]string, 12)
	for i := range summaries {
		summaries[i] = fmt.Sprintf(`{"summary":"msg%d"}`, i)
	}
	agentDir := makeNotificationDB(t, bin, summaries)

	m := NewNotificationModel(agentDir)
	if len(m.blocks) != 10 {
		t.Fatalf("expected 10 blocks (limit), got %d", len(m.blocks))
	}
	// newest block should be msg11
	if m.blocks[0].Summary != "msg11" {
		t.Fatalf("expected newest block msg11, got %q", m.blocks[0].Summary)
	}
}

// makeNotificationDB is a test helper that inserts notification_pair_injected
// rows with the given fields_json strings and returns the agent dir.
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
		// Use single quotes escaped; fj must not contain single quotes for simplicity
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
