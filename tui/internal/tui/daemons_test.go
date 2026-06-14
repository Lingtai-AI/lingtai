package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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
		"group_id":"dg-20260609-010203-fedcba",
		"state":"done",
		"backend":"lingtai",
		"started_at":"2026-06-09T01:02:03Z",
		"finished_at":"2026-06-09T01:02:09Z",
		"elapsed_s":6.25,
		"turn":3,
		"max_turns":8
	}`)
	write(filepath.Join(daemonDir, "logs", "events.jsonl"), strings.Join([]string{
		`{"ts":"2026-06-09T01:02:04Z","event":"daemon_start"}`,
		`{"ts":"2026-06-09T01:02:05Z","event":"tool_call","name":"read"}`,
		`{"ts":"2026-06-09T01:02:06Z","event":"tool_result","name":"read","status":"ok"}`,
	}, "\n"))
	write(filepath.Join(daemonDir, "history", "chat_history.jsonl"), `{"role":"assistant","text":"task done","turn":3,"ts":"2026-06-09T01:02:07Z"}`)
	write(filepath.Join(daemonDir, "logs", "token_ledger.jsonl"), strings.Join([]string{
		`{"input":10,"output":4,"thinking":2,"cached":7}`,
		`{"input":3,"output":1,"thinking":0,"cached":2}`,
	}, "\n"))
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
	if got.GroupID != "dg-20260609-010203-fedcba" {
		t.Fatalf("group id = %q", got.GroupID)
	}
	if got.FinishedAt != "2026-06-09T01:02:09Z" || got.CompletedAt != got.FinishedAt || got.ElapsedS != 6.25 {
		t.Fatalf("terminal timing not parsed: %#v", got)
	}
	if got.EventCount != 3 || got.ToolCount != 2 || len(got.Events) != 3 || got.LastEventAt != "2026-06-09T01:02:06Z" {
		t.Fatalf("events not parsed: count=%d tools=%d events=%d last=%q", got.EventCount, got.ToolCount, len(got.Events), got.LastEventAt)
	}
	if got.Tokens.Calls != 2 || got.Tokens.Input != 13 || got.Tokens.Output != 5 || got.Tokens.Thinking != 2 || got.Tokens.Cached != 9 {
		t.Fatalf("tokens not parsed: %#v", got.Tokens)
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
			GroupID: "dg-20260609-010203-fedcba",
			State:   "done",
			Backend: "lingtai",
			Preset:  "deepseek-coder",
		}},
	}
	out := m.renderDetail(80)
	if !strings.Contains(out, "deepseek-coder") {
		t.Fatalf("renderDetail missing preset; got:\n%s", out)
	}
	if !strings.Contains(out, "dg-20260609-010203-fedcba") {
		t.Fatalf("renderDetail missing group id; got:\n%s", out)
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

func TestRenderDetailShowsDaemonTimingAndTokens(t *testing.T) {
	m := DaemonsModel{
		items: []daemonSummary{{
			Dir:         "/tmp/daemons/em-7",
			State:       "done",
			Backend:     "lingtai",
			StartedAt:   "2026-06-09T01:02:03Z",
			FinishedAt:  "2026-06-09T01:02:09Z",
			ElapsedS:    6.25,
			LastEventAt: "2026-06-09T01:02:06Z",
			Tokens:      daemonTokenSummary{Input: 13, Output: 5, Thinking: 2, Cached: 9, Calls: 2},
		}},
	}
	out := m.renderDetail(120)
	for _, want := range []string{"duration", "6s", "finished", "2026-06-09T01:02:09Z", "last event", "2026-06-09T01:02:06Z", "tokens", "2 calls / 20 tokens"} {
		if !strings.Contains(out, want) {
			t.Fatalf("renderDetail missing %q; got:\n%s", want, out)
		}
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

func TestDaemonsPaneScrollFocusIsIndependent(t *testing.T) {
	m := testDaemonsModelWithItems(t, 12, 8)
	if m.focused != daemonPaneDetail {
		t.Fatalf("initial focused pane = %v, want detail", m.focused)
	}

	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if m.detailVP.YOffset() == 0 {
		t.Fatalf("pgdown with detail focus did not scroll detail pane")
	}
	if m.listVP.YOffset() != 0 {
		t.Fatalf("pgdown with detail focus scrolled list pane to %d", m.listVP.YOffset())
	}
	detailOffset := m.detailVP.YOffset()

	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.focused != daemonPaneList {
		t.Fatalf("tab focused pane = %v, want list", m.focused)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if m.listVP.YOffset() == 0 {
		t.Fatalf("pgdown with list focus did not scroll list pane")
	}
	if m.detailVP.YOffset() != detailOffset {
		t.Fatalf("list-focused pgdown changed detail offset from %d to %d", detailOffset, m.detailVP.YOffset())
	}
}

func TestDaemonsSelectionKeepsListVisibleAndResetsDetailScroll(t *testing.T) {
	m := testDaemonsModelWithItems(t, 14, 7)
	m.detailVP.SetYOffset(10)
	for i := 0; i < 8; i++ {
		m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}

	if m.selected != 8 {
		t.Fatalf("selected = %d, want 8", m.selected)
	}
	if m.detailVP.YOffset() != 0 {
		t.Fatalf("selection change did not reset detail scroll; offset=%d", m.detailVP.YOffset())
	}
	if m.listVP.YOffset() == 0 {
		t.Fatalf("list viewport did not scroll to keep selected item visible")
	}
	row := m.selectedListRow()
	if row < m.listVP.YOffset() || row >= m.listVP.YOffset()+m.listVP.Height() {
		t.Fatalf("selected row %d not visible in list offset=%d height=%d", row, m.listVP.YOffset(), m.listVP.Height())
	}
}

func TestDaemonsLeftRightChooseFocusedPane(t *testing.T) {
	m := testDaemonsModelWithItems(t, 3, 8)
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.focused != daemonPaneList {
		t.Fatalf("left focused pane = %v, want list", m.focused)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.focused != daemonPaneDetail {
		t.Fatalf("right focused pane = %v, want detail", m.focused)
	}
}

func testDaemonsModelWithItems(t *testing.T, count, height int) DaemonsModel {
	t.Helper()
	agentDir := t.TempDir()
	m := NewDaemonsModel(filepath.Dir(agentDir), agentDir)
	items := make([]daemonSummary, count)
	for i := range items {
		items[i] = daemonSummary{
			Dir:     filepath.Join(agentDir, "daemons", fmt.Sprintf("em-%02d", i)),
			Handle:  fmt.Sprintf("em-%02d", i),
			State:   "running",
			Backend: "lingtai",
			Task:    fmt.Sprintf("daemon task %02d", i),
			Result:  strings.Repeat(fmt.Sprintf("detail line %02d ", i), 30),
			Events: []daemonEvent{
				{Event: "tool_call", Name: "read", Raw: strings.Repeat("event body ", 20)},
			},
		}
	}
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: height})
	m, _ = m.Update(daemonsLoadMsg{selectedDir: agentDir, items: items})
	return m
}
