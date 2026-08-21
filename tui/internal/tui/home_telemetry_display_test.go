package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/config"
)

// homeTelemetryDisplayFixture is a snapshot where every allowlisted fragment has
// something to render, so a test can tell "this fragment was deselected" apart
// from "this fragment had no data".
func homeTelemetryDisplayFixture() homeTelemetry {
	return homeTelemetry{
		provider: "zhipu", model: "glm-5.2",
		apiCalls: 42, sessionTokens: 181585, inputTokens: 181585, cached: 180224,
		contextUsed: 182500, contextLimit: 250000, contextUsage: 0.73,
	}
}

// The on-disk vocabulary (config.AllowedHomeTelemetryDisplay, which is what
// LoadTUIConfig/SaveTUIConfig accept) and the row's built-in default expression
// are one declaration, so an accepted name always has a fragment to render and
// the default order is the persisted order. Pin that they agree exactly.
func TestHomeTelemetryDefaultExpressionIsTheConfigVocabulary(t *testing.T) {
	want := []homeTelemetrySegment{
		homeTelemetrySegSession,
		homeTelemetrySegLLM,
		homeTelemetrySegAPI,
		homeTelemetrySegTokens,
		homeTelemetrySegCache,
		homeTelemetrySegContext,
	}
	if len(defaultHomeTelemetryDisplay) != len(want) {
		t.Fatalf("default expression = %v, want %v", defaultHomeTelemetryDisplay, want)
	}
	for i, seg := range want {
		if defaultHomeTelemetryDisplay[i] != seg {
			t.Fatalf("default expression = %v, want %v", defaultHomeTelemetryDisplay, want)
		}
		if config.AllowedHomeTelemetryDisplay[i] != string(seg) {
			t.Fatalf("config vocabulary = %v, want %v", config.AllowedHomeTelemetryDisplay, want)
		}
	}
}

// The persistent default is the row users already have: rendering the named
// default expression explicitly, and rendering with no expression at all, must
// produce the same bytes — including the segment order and the right-aligned
// /kanban hint.
func TestHomeTelemetryDefaultExpressionRendersByteIdenticalRow(t *testing.T) {
	tel := homeTelemetryDisplayFixture()
	for _, width := range []int{30, 120} {
		byDefault := formatHomeTelemetry(tel, width, nil)
		explicit := formatHomeTelemetry(tel, width, defaultHomeTelemetryDisplay)
		if byDefault != explicit {
			t.Fatalf("width %d: default expression drifted from the no-preference row:\n nil = %q\nexpr = %q", width, byDefault, explicit)
		}
		if byDefault == "" {
			t.Fatalf("width %d: fixture rendered nothing", width)
		}
	}

	// Order is part of the default: scope label, LLM pair, api, tok(+miss),
	// cache, ctx. Pin it so a reordering of the fragment table is a test failure
	// rather than a silent change to every install.
	got := formatHomeTelemetry(tel, 120, nil)
	var at []int
	for _, key := range []string{"mail.telemetry_session", "mail.telemetry_model", "mail.telemetry_api", "mail.telemetry_tok", "mail.telemetry_cache", "mail.telemetry_context"} {
		idx := strings.Index(got, i18n.T(key))
		if idx < 0 {
			t.Fatalf("default row %q is missing fragment %s", got, key)
		}
		at = append(at, idx)
	}
	for i := 1; i < len(at); i++ {
		if at[i] <= at[i-1] {
			t.Fatalf("default fragment order broke at position %d in %q (offsets %v)", i, got, at)
		}
	}
}

// A persisted expression is a selection/reordering of the SAME fragments. Every
// invalid shape falls back wholesale to the default rather than rendering a
// partially honored row.
func TestHomeTelemetryDisplayFromConfigFailsClosed(t *testing.T) {
	valid := []string{"context", "session"}
	if got := homeTelemetryDisplayFromConfig(valid); len(got) != 2 || got[0] != homeTelemetrySegContext || got[1] != homeTelemetrySegSession {
		t.Fatalf("valid expression %v = %v, want [context session]", valid, got)
	}

	for _, raw := range [][]string{
		nil,
		{},                     // empty
		{""},                   // empty name
		{"ctx"},                // unknown (near-miss of "context")
		{"session", "SESSION"}, // unknown (case is part of the vocabulary)
		{"session", "session"}, // duplicate
		{"session", "llm", "api", "tokens", "cache", "context", "session"}, // too long
		{"session", "kanban_hint"}, // renderer/layout behavior is not expression content
	} {
		if got := homeTelemetryDisplayFromConfig(raw); got != nil {
			t.Errorf("invalid expression %v = %v, want nil (fall back to default)", raw, got)
		}
	}

	// Falling back means rendering the default row, not a blank one.
	tel := homeTelemetryDisplayFixture()
	if got, want := formatHomeTelemetry(tel, 120, homeTelemetryDisplayFromConfig([]string{"ctx"})), formatHomeTelemetry(tel, 120, nil); got != want {
		t.Fatalf("invalid expression rendered %q, want the default row %q", got, want)
	}
}

// The replacement must reach a real MailModel view: a shortened, reordered
// expression threaded through NewMailModel changes what View() prints.
func TestMailViewHonorsReorderedHomeTelemetryDisplay(t *testing.T) {
	dir := t.TempDir()
	orchDir := filepath.Join(dir, "orch")
	writeHomeAgentRecord(t, orchDir, `{"schema": "lingtai.agent_record/v1",
	  "model": {"provider": "zhipu", "model": "glm-5.2"},
	  "usage": {"input_tokens": 181585, "output_tokens": 0, "cached_tokens": 180224, "api_calls": 42,
	            "context_used_tokens": 182500, "context_limit_tokens": 250000, "context_usage_pct": 73}}`)
	humanDir := filepath.Join(dir, "human")
	if err := os.MkdirAll(humanDir, 0o755); err != nil {
		t.Fatal(err)
	}

	render := func(display []string) string {
		m := NewMailModel(humanDir, "human@local", "~", orchDir, "TestOrch", 50, dir, "en", false, 0)
		// The app applies the validated expression at App.installMailModel; a
		// focused display test sets the same package-private field directly so it
		// keeps rendering a real MailModel without a constructor argument.
		m.homeTelemetryDisplay = homeTelemetryDisplayFromConfig(display)
		m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
		m, _ = m.Update(m.initialRebuild())
		m, _ = m.Update(m.fetchHomeTelemetry())
		if !m.hasHomeTelemetry() {
			t.Fatal("test setup failed: the seeded Agent Record should produce telemetry")
		}
		m.lastInputLines = -1
		m.syncViewportHeight()
		return m.View()
	}

	out := render([]string{"context", "session", "api"})
	ctx, session, api := i18n.T("mail.telemetry_context"), i18n.T("mail.telemetry_session"), i18n.T("mail.telemetry_api")
	ci, si, ai := strings.Index(out, ctx), strings.Index(out, session), strings.Index(out, api)
	if ci < 0 || si < 0 || ai < 0 || !(ci < si && si < ai) {
		t.Fatalf("reordered expression not honored in View (ctx=%d session=%d api=%d):\n%s", ci, si, ai, out)
	}
	// Deselected fragments are gone even though their data is present.
	for _, key := range []string{"mail.telemetry_model", "mail.telemetry_cache"} {
		if strings.Contains(out, i18n.T(key)) {
			t.Errorf("View still shows deselected fragment %s:\n%s", key, out)
		}
	}
	// The row is still a row: the /kanban affordance and the status bar below it
	// are renderer/layout behavior no expression can move.
	if !strings.Contains(out, i18n.T("mail.telemetry_kanban_hint")) {
		t.Errorf("the /kanban affordance disappeared under a user expression:\n%s", out)
	}
	if !strings.Contains(out, "ctrl+o to expand") {
		t.Errorf("the status bar was clipped under a user expression:\n%s", out)
	}
}

// Width-dependent layout stays renderer behavior under every expression: the
// bar is dropped below homeTelemetryBarMinWidth (the numbers stay), and the
// right-aligned hint is dropped when there is no room for it.
func TestHomeTelemetryDisplayKeepsWidthLayoutBehavior(t *testing.T) {
	tel := homeTelemetryDisplayFixture()
	ctxOnly := homeTelemetryDisplayFromConfig([]string{"context"})
	if ctxOnly == nil {
		t.Fatal(`["context"] should be a valid expression`)
	}

	wide := formatHomeTelemetry(tel, 120, ctxOnly)
	if !strings.Contains(wide, "▓") || !strings.Contains(wide, "73%") {
		t.Errorf("wide context-only row lost the gauge: %q", wide)
	}
	if !strings.Contains(wide, i18n.T("mail.telemetry_kanban_hint")) {
		t.Errorf("wide context-only row lost the right-aligned hint: %q", wide)
	}

	narrow := formatHomeTelemetry(tel, 30, ctxOnly)
	if strings.Contains(narrow, "▓") || strings.Contains(narrow, "░") {
		t.Errorf("narrow context-only row must drop the bar: %q", narrow)
	}
	if !strings.Contains(narrow, "73%") {
		t.Errorf("narrow context-only row must keep the numbers: %q", narrow)
	}
	if strings.Contains(narrow, i18n.T("mail.telemetry_kanban_hint")) {
		t.Errorf("narrow row must drop the hint rather than clip the metrics: %q", narrow)
	}
}

// The #919 one-record boundary is expression-independent: when
// `system/agent_record.json` is absent or malformed there is nothing to select
// from, so every expression renders no row — never a reconstruction from
// .status.json, the manifest, the ledger, or a fabricated zero.
func TestHomeTelemetryUnavailableAgentRecordHidesRowUnderEveryExpression(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string // "" = write no record at all
	}{
		{name: "absent"},
		{name: "malformed json", body: `{"schema": "lingtai.agent_record/v1", "usage":`},
		{name: "unrecognized schema", body: `{"schema": "lingtai.agent_record/v99", "usage": {"api_calls": 42, "input_tokens": 100}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.body != "" {
				writeHomeAgentRecord(t, dir, tc.body)
			}
			// Decoys the row must ignore: a rich .status.json and an events log.
			if err := os.MkdirAll(filepath.Join(dir, "logs"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, ".status.json"), []byte(`{"window_size": 250000, "api_calls": 42}`), 0o644); err != nil {
				t.Fatal(err)
			}

			tel := (MailModel{orchestrator: dir}).gatherHomeTelemetry()
			if tel.hasData() {
				t.Fatalf("unavailable Agent Record produced data: %+v", tel)
			}
			for _, display := range [][]homeTelemetrySegment{
				nil,
				defaultHomeTelemetryDisplay,
				homeTelemetryDisplayFromConfig([]string{"context", "session"}),
			} {
				if got := formatHomeTelemetry(tel, 120, display); got != "" {
					t.Errorf("expression %v rendered %q, want the row hidden", display, got)
				}
			}
		})
	}
}

// contextOnlyAgentRecord is a snapshot where the ONLY fragment with data is
// `context`: no model pair, no api calls, no tokens. Selecting anything else is
// a valid expression that this snapshot cannot fill.
const contextOnlyAgentRecord = `{"schema": "lingtai.agent_record/v1",
  "usage": {"context_used_tokens": 182500, "context_limit_tokens": 250000, "context_usage_pct": 73}}`

// newDisplayMailModel builds a ready Mail model over the given Agent Record with
// the expression applied the way App.installMailModel applies it, then lands one
// telemetry fetch and syncs the layout.
func newDisplayMailModel(t *testing.T, record string, display []string) MailModel {
	t.Helper()
	dir := t.TempDir()
	orchDir := filepath.Join(dir, "orch")
	writeHomeAgentRecord(t, orchDir, record)
	humanDir := filepath.Join(dir, "human")
	if err := os.MkdirAll(humanDir, 0o755); err != nil {
		t.Fatal(err)
	}
	m := NewMailModel(humanDir, "human@local", "~", orchDir, "TestOrch", 50, dir, "en", false, 0)
	m.homeTelemetryDisplay = homeTelemetryDisplayFromConfig(display)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m, _ = m.Update(m.initialRebuild())
	m, _ = m.Update(m.fetchHomeTelemetry())
	m.lastInputLines = -1
	m.syncViewportHeight()
	return m
}

// A VALID expression that selects only fragments the current snapshot has no
// data for renders no row — and must not reserve a footer line for the row it
// did not render. Reserving one leaves a blank line and shortens the viewport;
// the #441 mismatch in the other direction clipped the status bar. Both come
// from the one predicate, so this pins them together.
func TestHomeTelemetryExpressionWithNoAvailableFragmentReservesNoRow(t *testing.T) {
	// Every selected fragment is unavailable in a context-only record.
	hidden := newDisplayMailModel(t, contextOnlyAgentRecord, []string{"llm", "api", "tokens", "cache"})
	if !hidden.homeTelemetryLoaded {
		t.Fatal("test setup failed: the telemetry fetch did not land")
	}
	if !hidden.homeTelemetry.hasData() {
		t.Fatal("test setup failed: the context-only record should still carry data")
	}
	if got := formatHomeTelemetry(hidden.homeTelemetry, hidden.width, hidden.homeTelemetryDisplay); got != "" {
		t.Fatalf("expression selecting only unavailable fragments rendered %q, want no row", got)
	}
	if hidden.hasHomeTelemetry() {
		t.Fatal("the visibility predicate still reports a row the renderer does not draw")
	}
	if hidden.lastTelemetryRows != 0 {
		t.Fatalf("reserved %d telemetry rows for a row that is not rendered", hidden.lastTelemetryRows)
	}

	// Same record, default expression: the context fragment IS available, so
	// exactly one row is rendered and exactly one line is reserved for it. The
	// hidden model must therefore own one more viewport line, not the same
	// height with a blank row in it.
	shown := newDisplayMailModel(t, contextOnlyAgentRecord, nil)
	if !shown.hasHomeTelemetry() || shown.lastTelemetryRows != 1 {
		t.Fatalf("default expression over a context-only record: visible=%v reserved=%d, want true/1", shown.hasHomeTelemetry(), shown.lastTelemetryRows)
	}
	if got, want := hidden.viewport.Height(), shown.viewport.Height()+1; got != want {
		t.Fatalf("hidden-row viewport height = %d, want %d (the unreserved line goes back to the transcript)", got, want)
	}

	// The frame still fits and the status bar is still the last thing in it —
	// neither clipped off the bottom nor pushed down by a blank telemetry line.
	for _, tc := range []struct {
		name string
		m    MailModel
	}{{"no available fragment", hidden}, {"default expression", shown}} {
		out := tc.m.View()
		lines := strings.Split(out, "\n")
		if len(lines) > 24 {
			t.Errorf("%s: View rendered %d lines into a 24-line frame:\n%s", tc.name, len(lines), out)
		}
		last := lines[len(lines)-1]
		if !strings.Contains(last, "ctrl+o to expand") {
			t.Errorf("%s: last frame line is not the status bar: %q", tc.name, last)
		}
	}
	if strings.Contains(hidden.View(), i18n.T("mail.telemetry_kanban_hint")) {
		t.Errorf("no fragment was available, so no row — including its affordance — should render:\n%s", hidden.View())
	}
}

// Returning from /settings re-reads tui_config.json and re-applies the
// expression to the PRESERVED mail model. When that flips the row's visibility
// the reserved height has to be recomputed right there; leaving it stale until
// some later event resyncs is the same mismatch as blocker #441.
func TestReturningFromSettingsRecomputesTelemetryHeightForNewExpression(t *testing.T) {
	globalDir := t.TempDir()
	projectDir := t.TempDir()
	orchDir := filepath.Join(t.TempDir(), "orch")
	writeHomeAgentRecord(t, orchDir, contextOnlyAgentRecord)
	humanDir := filepath.Join(projectDir, "human")
	if err := os.MkdirAll(humanDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Start on the default expression: the context fragment renders, one line
	// is reserved.
	if err := config.SaveTUIConfig(globalDir, config.DefaultTUIConfig()); err != nil {
		t.Fatal(err)
	}
	a := App{
		currentView: appViewSettings,
		globalDir:   globalDir,
		projectDir:  projectDir,
		orchDir:     orchDir,
		orchName:    "agent",
		tuiConfig:   config.LoadTUIConfig(globalDir),
	}
	a.installMailModel(NewMailModel(humanDir, "human", "~", orchDir, "agent", 200, globalDir, "en", false, 0))
	a.mail, _ = a.mail.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	a.mail, _ = a.mail.Update(a.mail.initialRebuild())
	a.mail, _ = a.mail.Update(a.mail.fetchHomeTelemetry())
	a.mail.lastInputLines = -1
	a.mail.syncViewportHeight()
	if !a.mail.hasHomeTelemetry() || a.mail.lastTelemetryRows != 1 {
		t.Fatalf("precondition: visible=%v reserved=%d, want true/1", a.mail.hasHomeTelemetry(), a.mail.lastTelemetryRows)
	}
	beforeHeight := a.mail.viewport.Height()

	// The user hand-edits the preference to a valid expression this snapshot
	// cannot fill, then leaves /settings.
	cfg := config.LoadTUIConfig(globalDir)
	cfg.HomeTelemetryDisplay = config.HomeTelemetryDisplay{"llm", "api", "tokens", "cache"}
	if err := config.SaveTUIConfig(globalDir, cfg); err != nil {
		t.Fatal(err)
	}

	model, _ := a.switchToView("mail")
	got, ok := model.(App)
	if !ok {
		t.Fatalf("switchToView returned %T, want App", model)
	}
	if got.mail.hasHomeTelemetry() {
		t.Fatal("returning from settings kept the row visible under an expression with no available fragment")
	}
	if got.mail.lastTelemetryRows != 0 {
		t.Fatalf("returning from settings left %d telemetry rows reserved for a hidden row", got.mail.lastTelemetryRows)
	}
	if h := got.mail.viewport.Height(); h != beforeHeight+1 {
		t.Fatalf("viewport height after settings return = %d, want %d (recomputed, not stale)", h, beforeHeight+1)
	}
	out := got.mail.View()
	lines := strings.Split(out, "\n")
	if len(lines) > 24 {
		t.Fatalf("View rendered %d lines into a 24-line frame:\n%s", len(lines), out)
	}
	if last := lines[len(lines)-1]; !strings.Contains(last, "ctrl+o to expand") {
		t.Fatalf("status bar is not the last frame line after settings return: %q", last)
	}
}
