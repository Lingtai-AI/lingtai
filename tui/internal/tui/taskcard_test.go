package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthropics/lingtai-tui/i18n"
)

func writeTaskCardStatus(t *testing.T, agentDir, raw string) {
	t.Helper()
	dir := filepath.Join(agentDir, "taskcard")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "status"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeTaskCardBody(t *testing.T, agentDir, raw string) {
	t.Helper()
	dir := filepath.Join(agentDir, "taskcard")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "taskcard.md"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
}

// /taskcard must be registered like every other command, and both view
// dispatchers (the initial palette entry and the ViewChangeMsg/switchToView
// re-entry path) must route to it.
func TestDefaultCommandsIncludesTaskCard(t *testing.T) {
	cmd, ok := findCommand("taskcard")
	if !ok {
		t.Fatal("DefaultCommands() missing taskcard command")
	}
	if cmd.Description != "palette.taskcard" || cmd.Detail != "cmd.taskcard" {
		t.Fatalf("taskcard command keys = (%q, %q), want (palette.taskcard, cmd.taskcard)", cmd.Description, cmd.Detail)
	}
}

func TestTaskCardCommandOpensView(t *testing.T) {
	agentDir := t.TempDir()
	writeTaskCardStatus(t, agentDir, "active")
	writeTaskCardBody(t, agentDir, "# Sprint\n\n- [ ] ship it\n")

	app := App{orchDir: agentDir, projectDir: t.TempDir()}
	updated, _ := app.handlePaletteCommand("taskcard", "")
	got := updated.(App)
	if got.currentView != appViewTaskCard {
		t.Fatalf("handlePaletteCommand(taskcard) currentView = %v, want appViewTaskCard", got.currentView)
	}

	model, _ := got.switchToView("taskcard")
	reentered := model.(App)
	if reentered.currentView != appViewTaskCard {
		t.Fatalf("switchToView(taskcard) currentView = %v, want appViewTaskCard", reentered.currentView)
	}
}

// The card body must reach the viewer byte-for-byte — no trimming, no
// re-rendering — when status is exactly "active".
func TestReadTaskCardBodyActiveFidelity(t *testing.T) {
	agentDir := t.TempDir()
	const body = "# Sprint\n\n- [ ] step one\n- [x] step two\n\ntrailing space kept   \n"
	writeTaskCardStatus(t, agentDir, "active")
	writeTaskCardBody(t, agentDir, body)

	got, status, ok := readTaskCardBody(agentDir)
	if !ok {
		t.Fatal("readTaskCardBody() ok = false, want true")
	}
	if status != "active" {
		t.Fatalf("status = %q, want %q", status, "active")
	}
	if got != body {
		t.Fatalf("body = %q, want unchanged %q", got, body)
	}

	entries := taskCardEntries(agentDir)
	if len(entries) != 1 || entries[0].Content != body {
		t.Fatalf("taskCardEntries() content = %q, want unchanged %q", entries[0].Content, body)
	}
}

// Anything other than an exact "active" status, or a blank body, must fall
// back to the small localized message instead of being treated as a card.
func TestReadTaskCardBodyUnavailablePaths(t *testing.T) {
	tests := []struct {
		name       string
		writeStat  bool
		status     string
		writeBody  bool
		body       string
		wantStatus string
	}{
		{name: "no taskcard directory at all", wantStatus: ""},
		{name: "missing status file", writeBody: true, body: "hello", wantStatus: ""},
		{name: "inactive status", writeStat: true, status: "inactive", writeBody: true, body: "hello", wantStatus: "inactive"},
		{name: "trailing newline is not exact", writeStat: true, status: "active\n", writeBody: true, body: "hello", wantStatus: "active\n"},
		{name: "wrong case is not exact", writeStat: true, status: "Active", writeBody: true, body: "hello", wantStatus: "Active"},
		{name: "unknown status value", writeStat: true, status: "pending", writeBody: true, body: "hello", wantStatus: "pending"},
		{name: "active but missing body file", writeStat: true, status: "active", wantStatus: "active"},
		{name: "active but blank body", writeStat: true, status: "active", writeBody: true, body: "   \n\t\n", wantStatus: "active"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agentDir := t.TempDir()
			if tt.writeStat {
				writeTaskCardStatus(t, agentDir, tt.status)
			}
			if tt.writeBody {
				writeTaskCardBody(t, agentDir, tt.body)
			}

			got, status, ok := readTaskCardBody(agentDir)
			if ok {
				t.Fatal("readTaskCardBody() ok = true, want false")
			}
			if got != "" {
				t.Fatalf("body = %q, want empty when unavailable", got)
			}
			if status != tt.wantStatus {
				t.Fatalf("status = %q, want %q", status, tt.wantStatus)
			}
		})
	}
}

// Distinguishing "inactive" from every other unavailable reason is cheap
// (rule: "inactive may be distinguished if it is nearly free") and must not
// require a second file read or persisted state.
func TestTaskCardEntriesFallbackDistinguishesInactive(t *testing.T) {
	inactiveDir := t.TempDir()
	writeTaskCardStatus(t, inactiveDir, "inactive")
	entries := taskCardEntries(inactiveDir)
	if len(entries) != 1 {
		t.Fatalf("taskCardEntries() len = %d, want 1", len(entries))
	}
	if !strings.Contains(entries[0].Content, i18n.T("taskcard.inactive")) {
		t.Fatalf("inactive fallback content = %q, want to contain %q", entries[0].Content, i18n.T("taskcard.inactive"))
	}

	missingDir := t.TempDir()
	entries = taskCardEntries(missingDir)
	if !strings.Contains(entries[0].Content, i18n.T("taskcard.unavailable")) {
		t.Fatalf("missing-card fallback content = %q, want to contain %q", entries[0].Content, i18n.T("taskcard.unavailable"))
	}
}
