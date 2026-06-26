package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/i18n"
)

// End-to-end regression guard for the #441 home-telemetry clipping bug.
//
// PR #441 appended the telemetry row in View() without teaching
// syncViewportHeight about it. The rendered frame was then one line taller than
// the terminal, and the terminal's own scroll dropped the TOP line — but the
// user-visible symptom Jason reported was the BOTTOM status bar (the "ctrl+o
// soul" hint) vanishing, because the over-tall frame no longer fit. This test
// builds a real MailModel, forces the telemetry row to render, and asserts:
//
//  1. the rendered frame fits within the terminal height (no overflow), and
//  2. the "ctrl+o soul" hint is still present on the last rendered line.
//
// It is the integration counterpart to TestMailFooterHeightAccountsForTelemetryRow,
// which pins the arithmetic; this proves the arithmetic actually keeps the bar
// on-screen through the full View() path.
func newReadyMailModelWithTelemetry(t *testing.T, w, h int) MailModel {
	t.Helper()
	dir := t.TempDir()
	// Short baseDir ("~") so the status-bar path is narrow enough to leave room
	// for the right-aligned "ctrl+o soul" hint (a long temp path would push the
	// hint out by width budget — unrelated to the height-clipping regression).
	//
	// Force the telemetry row to have data by seeding a notification carrying a
	// context-usage fraction into the orchestrator's events.jsonl, then driving
	// the normal initialRebuild so it lands in the UNFILTERED session cache —
	// gatherHomeTelemetry reads the freshest notification context usage from
	// there (NOT the verbose-filtered m.messages), so this exercises the real
	// home-view path at verboseOff (no Ctrl+O).
	orchDir := filepath.Join(dir, "orch")
	logsDir := filepath.Join(orchDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	event := `{"type":"notification","ts":1782000000,"summary":"sync","meta":{"context":{"usage":0.73}}}` + "\n"
	if err := os.WriteFile(filepath.Join(logsDir, "events.jsonl"), []byte(event), 0o644); err != nil {
		t.Fatal(err)
	}
	humanDir := filepath.Join(dir, "human")
	if err := os.MkdirAll(humanDir, 0o755); err != nil {
		t.Fatal(err)
	}

	m := NewMailModel(humanDir, "human@local", "~", orchDir, "TestOrch", 50, dir, "en", false, 0)
	m, _ = m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	// Drive the deferred initial rebuild (the normal launch path) so the
	// notification populates the session cache. No Ctrl+O, verbose stays off.
	m, _ = m.Update(m.initialRebuild())
	// Re-sync height now that telemetry visibility flipped on.
	m.lastInputLines = -1
	m.syncViewportHeight()
	return m
}

func TestHomeViewKeepsStatusBarWhenTelemetryShows(t *testing.T) {
	const w, h = 100, 24

	// Baseline: same model, same size, and the SAME lifecycle (initialRebuild
	// clears the loading banner), but an empty orchestrator so there is no
	// telemetry data. This is the layout the user had before #441's row was added.
	dir := t.TempDir()
	baseOrch := filepath.Join(dir, "orch")
	if err := os.MkdirAll(filepath.Join(baseOrch, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	base := NewMailModel(dir, "human@local", "~", baseOrch, "TestOrch", 50, dir, "en", false, 0)
	base, _ = base.Update(tea.WindowSizeMsg{Width: w, Height: h})
	base, _ = base.Update(base.initialRebuild())
	if base.hasHomeTelemetry() {
		t.Skip("environment unexpectedly has session telemetry data; skipping the baseline comparison")
	}
	baseLines := len(strings.Split(base.View(), "\n"))

	m := newReadyMailModelWithTelemetry(t, w, h)
	if !m.hasHomeTelemetry() {
		t.Fatal("test setup failed: telemetry row should have data (context usage present)")
	}

	out := m.View()
	lines := strings.Split(out, "\n")

	// 1) THE FIX: showing the telemetry row must NOT grow the frame versus the
	//    no-telemetry baseline. Before the fix the additive row was unaccounted in
	//    syncViewportHeight, so the frame grew by one line and the terminal
	//    scrolled the bottom status bar off-screen. The row must be absorbed by a
	//    one-line-shorter viewport, leaving the total height unchanged.
	if len(lines) != baseLines {
		t.Errorf("telemetry changed the frame height: baseline=%d with-telemetry=%d (must be equal — the row is absorbed by the viewport, not added on top):\n%s",
			baseLines, len(lines), out)
	}

	// 2) The telemetry row must actually be present (proves we're exercising the
	//    additive path, not silently hiding it). The context segment leads with the
	//    localized "ctx" scope label and closes with the percentage on
	//    the right of the bar.
	ctxLabel := i18n.T("mail.telemetry_context")
	if !strings.Contains(out, ctxLabel) || !strings.Contains(out, "73%") {
		t.Errorf("telemetry row missing from rendered View:\n%s", out)
	}

	// 3) The "ctrl+o soul" hint (the affordance the regression ate) must survive,
	//    on the last non-empty rendered line — below the telemetry row.
	if !strings.Contains(out, "ctrl+o soul") {
		t.Fatalf("the 'ctrl+o soul' hint was clipped from the footer:\n%s", out)
	}
	lastNonEmpty := ""
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			lastNonEmpty = lines[i]
			break
		}
	}
	if !strings.Contains(lastNonEmpty, "ctrl+o soul") {
		t.Errorf("the status bar (ctrl+o soul hint) is not the last visible line; telemetry pushed it out of place:\nlast=%q\nfull:\n%s",
			lastNonEmpty, out)
	}
	// And the telemetry row must sit ABOVE the status bar, not replace or follow it.
	if idxTel, idxBar := strings.Index(out, ctxLabel), strings.LastIndex(out, "ctrl+o soul"); idxTel >= idxBar {
		t.Errorf("telemetry row must be ABOVE the status bar (tel=%d bar=%d):\n%s", idxTel, idxBar, out)
	}
}

// Without telemetry data the row is omitted and the layout is unchanged — the
// fix must not cost a viewport line when there's nothing to show.
func TestHomeViewNoTelemetryRowWhenNoData(t *testing.T) {
	const w, h = 100, 24
	dir := t.TempDir()
	m := NewMailModel(dir, "human@local", "~", dir, "TestOrch", 50, dir, "en", false, 0)
	m, _ = m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	// No messages, no ledger → no telemetry.
	if m.hasHomeTelemetry() {
		t.Skip("environment unexpectedly has session ledger data; skipping the no-data case")
	}
	out := m.View()
	if strings.Contains(out, "tok/api") {
		t.Errorf("telemetry row should be hidden with no data:\n%s", out)
	}
	if !strings.Contains(out, "ctrl+o soul") {
		t.Errorf("status bar hint missing even without telemetry:\n%s", out)
	}
}
