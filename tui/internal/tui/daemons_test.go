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

func TestReadDaemonSummaryParsesPreset(t *testing.T) {
	dir := t.TempDir()
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "daemon.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// preset_name present → used verbatim.
	write(`{"backend":"lingtai","preset_name":"deepseek-coder","preset_provider":"deepseek","preset_model":"deepseek-chat","model":"deepseek-chat"}`)
	got, err := readDaemonSummary(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Preset != "deepseek-coder" {
		t.Fatalf("preset = %q, want %q", got.Preset, "deepseek-coder")
	}

	// preset_name null → fall back to provider:model.
	write(`{"backend":"lingtai","preset_name":null,"preset_provider":"deepseek","preset_model":"deepseek-chat","model":"deepseek-chat"}`)
	got, err = readDaemonSummary(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Preset != "deepseek:deepseek-chat" {
		t.Fatalf("preset fallback = %q, want %q", got.Preset, "deepseek:deepseek-chat")
	}

	// only model present → fall back to model.
	write(`{"backend":"lingtai","model":"glm-4"}`)
	got, err = readDaemonSummary(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Preset != "glm-4" {
		t.Fatalf("preset model fallback = %q, want %q", got.Preset, "glm-4")
	}
}

func TestRenderDetailShowsPresetNearBackend(t *testing.T) {
	m := DaemonsModel{
		items: []daemonSummary{{
			Dir:     "/tmp/daemons/em-7",
			State:   "done",
			Backend: "lingtai",
			Preset:  "deepseek-coder",
		}},
	}
	out := m.renderDetail(80)
	if !strings.Contains(out, "deepseek-coder") {
		t.Fatalf("renderDetail missing preset; got:\n%s", out)
	}
	// preset row sits in the metadata block, right after backend.
	bi := strings.Index(out, "lingtai")
	pi := strings.Index(out, "deepseek-coder")
	if bi < 0 || pi < 0 {
		t.Fatalf("backend or preset missing; got:\n%s", out)
	}
	if pi < bi {
		t.Fatalf("preset rendered before backend; backend=%d preset=%d", bi, pi)
	}
}

func TestRenderDetailPutsEventsLast(t *testing.T) {
	m := DaemonsModel{
		items: []daemonSummary{{
			Dir:     "/tmp/daemons/em-7",
			State:   "done",
			Backend: "lingtai",
			Task:    "do the thing",
			Result:  "all done",
			Chats:   []daemonChat{{Role: "assistant", Text: "chat line"}},
			Events:  []daemonEvent{{Event: "tool_call", Name: "read", Raw: `{"event":"tool_call"}`}},
		}},
	}
	out := m.renderDetail(80)

	taskIdx := strings.Index(out, "do the thing")
	resultIdx := strings.Index(out, "all done")
	chatIdx := strings.Index(out, "chat line")
	eventsIdx := strings.Index(out, "tool_call")

	for name, idx := range map[string]int{"task": taskIdx, "result": resultIdx, "chat": chatIdx, "events": eventsIdx} {
		if idx < 0 {
			t.Fatalf("%s section missing from detail:\n%s", name, out)
		}
	}
	if !(taskIdx < eventsIdx && resultIdx < eventsIdx && chatIdx < eventsIdx) {
		t.Fatalf("events not last: task=%d result=%d chat=%d events=%d", taskIdx, resultIdx, chatIdx, eventsIdx)
	}
}
