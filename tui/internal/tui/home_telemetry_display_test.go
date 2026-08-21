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

// The expression is a selection/reordering of the row's OWN fragments and
// nothing else: no preference renders exactly the row every install already
// has, an invalid preference falls back to that same row wholesale, and a valid
// one changes which of those fragments render and in what order.
func TestHomeTelemetryDisplayExpressionSelectsFragments(t *testing.T) {
	// A snapshot where every allowlisted fragment has something to render, so
	// "this fragment was deselected" is distinguishable from "this fragment had
	// no data".
	tel := homeTelemetry{
		provider: "zhipu", model: "glm-5.2",
		apiCalls: 42, sessionTokens: 181585, inputTokens: 181585, cached: 180224,
		contextUsed: 182500, contextLimit: 250000, contextUsage: 0.73,
	}

	// The built-in default expression IS the on-disk vocabulary, in order, so an
	// accepted name always has a fragment to render and the persisted order is
	// the default order.
	if len(defaultHomeTelemetryDisplay) != len(config.AllowedHomeTelemetryDisplay) {
		t.Fatalf("default expression %v, want the config vocabulary %v", defaultHomeTelemetryDisplay, config.AllowedHomeTelemetryDisplay)
	}
	for i, name := range config.AllowedHomeTelemetryDisplay {
		if string(defaultHomeTelemetryDisplay[i]) != name {
			t.Fatalf("default expression %v, want the config vocabulary %v", defaultHomeTelemetryDisplay, config.AllowedHomeTelemetryDisplay)
		}
	}

	// No preference, the default spelled out, and an invalid preference all
	// render the same bytes — including the right-aligned /kanban hint and the
	// width-dependent layout the renderer owns.
	for _, width := range []int{30, 120} {
		want := formatHomeTelemetry(tel, width, nil)
		if want == "" {
			t.Fatalf("width %d: the fixture rendered nothing", width)
		}
		for _, tt := range []struct {
			name    string
			display []homeTelemetrySegment
		}{
			{"explicit default", defaultHomeTelemetryDisplay},
			// `ctx` is a near-miss of `context`: an invalid expression must fall
			// back wholesale, never render a partially honored row.
			{"invalid expression", homeTelemetryDisplayFromConfig([]string{"context", "ctx"})},
		} {
			if got := formatHomeTelemetry(tel, width, tt.display); got != want {
				t.Errorf("width %d: %s rendered %q, want the default row %q", width, tt.name, got, want)
			}
		}
	}

	// Fragment order follows the expression, and deselected fragments are gone
	// even though this snapshot has their data.
	for _, tt := range []struct {
		name    string
		display []string // nil = no preference
		order   []string // i18n label suffixes, in the order they must appear
		absent  []string
	}{
		{
			name:  "default",
			order: []string{"session", "model", "api", "tok", "cache", "context"},
		},
		{
			name:    "selected and reordered",
			display: []string{"context", "session", "api"},
			order:   []string{"context", "session", "api"},
			absent:  []string{"model", "tok", "cache"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := formatHomeTelemetry(tel, 120, homeTelemetryDisplayFromConfig(tt.display))
			prev := -1
			for _, frag := range tt.order {
				at := strings.Index(got, i18n.T("mail.telemetry_"+frag))
				if at <= prev {
					t.Fatalf("fragment %s missing or out of order (offset %d, previous %d) in %q", frag, at, prev, got)
				}
				prev = at
			}
			for _, frag := range tt.absent {
				if strings.Contains(got, i18n.T("mail.telemetry_"+frag)) {
					t.Errorf("deselected fragment %s still rendered in %q", frag, got)
				}
			}
		})
	}
}

// A VALID expression that selects only fragments the current snapshot has no
// data for renders no row — and must not reserve a footer line for the row it
// did not render. Returning from /settings re-reads tui_config.json and applies
// the new expression to the PRESERVED mail model, so the reserved height has to
// be recomputed right there: a reserved-but-unrendered row leaves a blank line
// and shortens the viewport, the mismatch in the other direction (#441) clipped
// the status bar. Both come from the one predicate, so this pins them together.
func TestHomeTelemetryExpressionWithNoAvailableFragmentReservesNoRow(t *testing.T) {
	// The ONLY fragment with data here is `context`: no model pair, no api
	// calls, no tokens.
	const contextOnlyAgentRecord = `{"schema": "lingtai.agent_record/v1",
	  "usage": {"context_used_tokens": 182500, "context_limit_tokens": 250000, "context_usage_pct": 73}}`

	globalDir := t.TempDir()
	projectDir := t.TempDir()
	orchDir := filepath.Join(t.TempDir(), "orch")
	writeHomeAgentRecord(t, orchDir, contextOnlyAgentRecord)
	humanDir := filepath.Join(projectDir, "human")
	if err := os.MkdirAll(humanDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Start on the default expression: the context fragment renders, so exactly
	// one row is drawn and exactly one line is reserved for it.
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
	if !a.mail.homeTelemetry.hasData() {
		t.Fatal("test setup failed: the context-only record should carry data")
	}
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

	if row := formatHomeTelemetry(got.mail.homeTelemetry, got.mail.width, got.mail.homeTelemetryDisplay); row != "" {
		t.Fatalf("expression selecting only unavailable fragments rendered %q, want no row", row)
	}
	if got.mail.hasHomeTelemetry() {
		t.Fatal("the visibility predicate still reports a row the renderer does not draw")
	}
	if got.mail.lastTelemetryRows != 0 {
		t.Fatalf("returning from settings left %d telemetry rows reserved for a hidden row", got.mail.lastTelemetryRows)
	}
	if h := got.mail.viewport.Height(); h != beforeHeight+1 {
		t.Fatalf("viewport height after settings return = %d, want %d (recomputed, not stale)", h, beforeHeight+1)
	}

	// The frame still fits and the status bar is still the last thing in it —
	// neither clipped off the bottom nor pushed down by a blank telemetry line.
	out := got.mail.View()
	lines := strings.Split(out, "\n")
	if len(lines) > 24 {
		t.Fatalf("View rendered %d lines into a 24-line frame:\n%s", len(lines), out)
	}
	if last := lines[len(lines)-1]; !strings.Contains(last, "ctrl+o to expand") {
		t.Fatalf("status bar is not the last frame line after settings return: %q", last)
	}
	if strings.Contains(out, i18n.T("mail.telemetry_kanban_hint")) {
		t.Errorf("no fragment was available, so no row — including its affordance — should render:\n%s", out)
	}
}
