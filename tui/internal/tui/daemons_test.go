package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	// Pin the zone so the localized timestamps below are deterministic.
	origLocal := time.Local
	t.Cleanup(func() { time.Local = origLocal })
	time.Local = time.FixedZone("test", -7*60*60)

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
	for _, want := range []string{"duration", "6s", "finished", "2026-06-08 18:02:09 U-7:00", "last event", "2026-06-08 18:02:06 U-7:00", "tokens", "2 calls / 20 tokens"} {
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

func TestDaemonTimestampRendersLocalOffset(t *testing.T) {
	origLocal := time.Local
	t.Cleanup(func() { time.Local = origLocal })
	time.Local = time.FixedZone("test", -7*60*60)

	got := formatDaemonTimestamp("2026-06-13T03:00:05Z")
	want := "2026-06-12 20:00:05 U-7:00"
	if got != want {
		t.Fatalf("formatDaemonTimestamp() = %q, want %q", got, want)
	}
}

func TestDaemonTimestampKeepsInvalidLegacyCompact(t *testing.T) {
	// Non-parseable legacy strings fall back to the old compact trim.
	got := formatDaemonTimestamp("2026-06-13T03:00:05-no-zone-here")
	want := "2026-06-13 03:00:05"
	if got != want {
		t.Fatalf("formatDaemonTimestamp() = %q, want legacy compact %q", got, want)
	}
	if got := formatDaemonTimestamp(""); got != "" {
		t.Fatalf("formatDaemonTimestamp(\"\") = %q, want empty", got)
	}
}

func TestRenderDetailTimestampsUseLocalOffset(t *testing.T) {
	origLocal := time.Local
	t.Cleanup(func() { time.Local = origLocal })
	time.Local = time.FixedZone("test", -7*60*60)

	m := DaemonsModel{
		items: []daemonSummary{{
			Dir:         "/tmp/daemons/em-7",
			State:       "done",
			Backend:     "lingtai",
			StartedAt:   "2026-06-09T01:02:03Z",
			FinishedAt:  "2026-06-09T01:02:09Z",
			LastEventAt: "2026-06-09T01:02:06Z",
			Chats: []daemonChat{
				{Role: "assistant", Kind: "reply", Text: "chat line", TS: "2026-06-09T01:02:07Z"},
			},
			Events: []daemonEvent{
				{Event: "tool_call", Name: "read", Raw: `{"event":"tool_call"}`, TS: "2026-06-09T01:02:05Z"},
			},
		}},
	}
	out := m.renderDetail(120)
	// Metadata + row timestamps all rendered in local time with a U-offset
	// marker. The raw RFC3339 "Z" form must not survive into the view.
	for _, want := range []string{
		"2026-06-08 18:02:03 U-7:00", // started
		"2026-06-08 18:02:09 U-7:00", // finished
		"2026-06-08 18:02:06 U-7:00", // last event
		"2026-06-08 18:02:07 U-7:00", // chat row
		"2026-06-08 18:02:05 U-7:00", // event row
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("renderDetail missing local-offset time %q; got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "2026-06-09T01:02") {
		t.Fatalf("renderDetail leaked raw RFC3339 timestamp; got:\n%s", out)
	}
}

func TestReadDaemonTokensSupportsClaudeNativeFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token_ledger.jsonl")
	// Claude Code / claude-p style usage: Anthropic Messages-API field names
	// (input_tokens/output_tokens/cache_read_input_tokens/cache_creation_input_tokens
	// + reasoning_tokens), mixed with one canonical-schema row.
	body := strings.Join([]string{
		`{"input_tokens":100,"output_tokens":40,"reasoning_tokens":7,"cache_read_input_tokens":60,"cache_creation_input_tokens":20}`,
		`{"input":5,"output":3,"thinking":1,"cached":2}`,
	}, "\n")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got := readDaemonTokens(path)
	// input = 100 + 5; output = 40 + 3; thinking = 7 + 1;
	// cached = (cache_read 60 + cache_creation 20) + 2.
	if got.Calls != 2 || got.Input != 105 || got.Output != 43 || got.Thinking != 8 || got.Cached != 82 {
		t.Fatalf("claude-native tokens not summed: %#v", got)
	}
}

func TestReadDaemonSummaryFallsBackToDaemonJSONTokens(t *testing.T) {
	dir := t.TempDir()
	// claude-p / CLI backends never write a token_ledger.jsonl, but the
	// kernel keeps a running tokens block in daemon.json. Surface that when
	// the ledger is absent so CLI-backend usage still shows.
	body := `{"backend":"claude-p","state":"done","tokens":{"input":1200,"output":340,"thinking":15,"cached":900}}`
	if err := os.WriteFile(filepath.Join(dir, "daemon.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readDaemonSummary(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Tokens.Input != 1200 || got.Tokens.Output != 340 || got.Tokens.Thinking != 15 || got.Tokens.Cached != 900 {
		t.Fatalf("daemon.json tokens fallback not applied: %#v", got.Tokens)
	}
	// Calls is unknown from the daemon.json snapshot; daemonTokenText must
	// still render when only the block totals are present.
	// total = input + output + thinking = 1200 + 340 + 15 = 1555.
	txt := daemonTokenText(got.Tokens)
	if !strings.Contains(txt, "1,555 tokens") {
		t.Fatalf("daemonTokenText(%#v) = %q, want a token total", got.Tokens, txt)
	}
}

func TestReadDaemonSummaryFallsBackToCLITokens(t *testing.T) {
	dir := t.TempDir()
	body := `{"backend":"claude-p","state":"done","tokens":{"input":0,"output":0,"thinking":0,"cached":0},"cli_tokens":{"input":6950,"output":4,"thinking":0,"cached":18689,"calls":1}}`
	if err := os.WriteFile(filepath.Join(dir, "daemon.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readDaemonSummary(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Tokens.Input != 6950 || got.Tokens.Output != 4 || got.Tokens.Cached != 18689 || got.Tokens.Calls != 1 {
		t.Fatalf("cli_tokens fallback not applied: %#v", got.Tokens)
	}
	txt := daemonTokenText(got.Tokens)
	if !strings.Contains(txt, "1 calls") || !strings.Contains(txt, "6,954 tokens") || !strings.Contains(txt, "cached 18,689") {
		t.Fatalf("daemonTokenText(%#v) = %q, want cli token details", got.Tokens, txt)
	}
}

func TestReadDaemonSummaryPrefersLedgerOverDaemonJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"backend":"lingtai","tokens":{"input":1,"output":1,"thinking":1,"cached":1}}`
	if err := os.WriteFile(filepath.Join(dir, "daemon.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "logs", "token_ledger.jsonl"),
		[]byte(`{"input":50,"output":20,"thinking":3,"cached":10}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readDaemonSummary(dir)
	if err != nil {
		t.Fatal(err)
	}
	// The per-call ledger is authoritative when present; the daemon.json
	// block is only a fallback.
	if got.Tokens.Calls != 1 || got.Tokens.Input != 50 || got.Tokens.Output != 20 {
		t.Fatalf("ledger should win over daemon.json block: %#v", got.Tokens)
	}
}
