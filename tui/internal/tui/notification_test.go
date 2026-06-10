package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestBuildNotificationEntriesShowsOverviewAndFiles(t *testing.T) {
	agentDir := t.TempDir()
	notifDir := filepath.Join(agentDir, ".notification")
	if err := os.MkdirAll(notifDir, 0o755); err != nil {
		t.Fatal(err)
	}
	telegramPath := filepath.Join(notifDir, "mcp.telegram.json")
	if err := os.WriteFile(telegramPath, []byte(`{"header":"1 new event","data":{"count":1}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(notifDir, "ignored.txt"), []byte("ignore"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries := buildNotificationEntries(agentDir)
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want overview + one json file", len(entries))
	}
	if entries[0].Label != "block" || !strings.Contains(entries[0].Content, "mcp.telegram") {
		t.Fatalf("overview entry missing block/mcp.telegram: %#v", entries[0])
	}
	if entries[1].Label != "mcp.telegram" || !strings.Contains(entries[1].Content, "1 new event") {
		t.Fatalf("file entry not rendered: %#v", entries[1])
	}
}
