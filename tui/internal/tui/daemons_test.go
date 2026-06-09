package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultCommandsIncludesDaemons(t *testing.T) {
	cmd, ok := findCommand("daemons")
	if !ok {
		t.Fatal("DefaultCommands() missing daemons command")
	}
	if cmd.Description != "palette.daemons" || cmd.Detail != "cmd.daemons" {
		t.Fatalf("daemons command keys = (%q, %q), want (palette.daemons, cmd.daemons)", cmd.Description, cmd.Detail)
	}
}

func TestDaemonsCommandOpensDaemonsView(t *testing.T) {
	app := App{orchDir: t.TempDir(), projectDir: t.TempDir()}
	model, _ := app.switchToView("daemons")
	got := model.(App)
	if got.currentView != appViewDaemons {
		t.Fatalf("switchToView(%q) currentView = %v, want appViewDaemons", "daemons", got.currentView)
	}
}

func TestLoadDaemonSummariesReadsMetadataEventsAndChats(t *testing.T) {
	agentDir := t.TempDir()
	daemonDir := filepath.Join(agentDir, "daemons", "em-7-20260609-010203-abcdef")
	if err := os.MkdirAll(filepath.Join(daemonDir, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(daemonDir, "history"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(daemonDir, "daemon.json"), `{
		"task":"Inspect daemon browser",
		"state":"done",
		"backend":"lingtai",
		"started_at":"2026-06-09T01:02:03Z",
		"turn":3,
		"max_turns":8
	}`)
	write(filepath.Join(daemonDir, "logs", "events.jsonl"), strings.Join([]string{
		`{"ts":"2026-06-09T01:02:04Z","event":"daemon_start"}`,
		`{"ts":"2026-06-09T01:02:05Z","event":"tool_call","name":"read"}`,
		`{"ts":"2026-06-09T01:02:06Z","event":"tool_result","name":"read","status":"ok"}`,
	}, "\n"))
	write(filepath.Join(daemonDir, "history", "chat_history.jsonl"), `{"role":"assistant","text":"task done","turn":3,"ts":"2026-06-09T01:02:07Z"}`)
	write(filepath.Join(daemonDir, "result.txt"), "full result")

	items, err := loadDaemonSummaries(agentDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d daemon summaries, want 1", len(items))
	}
	got := items[0]
	if got.Handle != "em-7" || got.State != "done" || got.Backend != "lingtai" {
		t.Fatalf("summary = %#v", got)
	}
	if got.Task != "Inspect daemon browser" || got.Turn != 3 || got.MaxTurns != 8 {
		t.Fatalf("metadata not parsed: %#v", got)
	}
	if got.EventCount != 3 || got.ToolCount != 2 || len(got.Events) != 3 {
		t.Fatalf("events not parsed: count=%d tools=%d events=%d", got.EventCount, got.ToolCount, len(got.Events))
	}
	if len(got.Chats) != 1 || got.Chats[0].Text != "task done" {
		t.Fatalf("chats not parsed: %#v", got.Chats)
	}
	if got.Result != "full result" {
		t.Fatalf("result = %q", got.Result)
	}
}
