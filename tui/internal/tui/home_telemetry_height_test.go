package tui

import "testing"

// The home telemetry row is an ADDITIVE line below the input box and above the
// path/shortcut status bar. PR #441 appended it in View() without teaching
// syncViewportHeight about it, so the rendered frame was one line too tall and
// the bottom status bar (the "ctrl+o to expand" hint) was clipped off-screen.
//
// mailFooterHeight is the single source of truth for how many terminal rows the
// footer block occupies. These tests pin the invariant: the telemetry line adds
// exactly one row, and nothing else changes.
func TestMailFooterHeightAccountsForTelemetryRow(t *testing.T) {
	const palette, input = 0, 1

	without := mailFooterHeight(palette, input, false)
	with := mailFooterHeight(palette, input, true)

	if with != without+1 {
		t.Fatalf("telemetry row must add exactly one footer line: without=%d with=%d", without, with)
	}
}

func TestMailFooterHeightBaseline(t *testing.T) {
	// Baseline (no telemetry) must match the pre-#441 layout budget so the fix is
	// purely additive: sep(1) + palette(N) + input(N) + border(1) + status(1).
	cases := []struct {
		palette, input, want int
	}{
		{0, 1, 4}, // 1 + 0 + 1 + 1 + 1
		{0, 3, 6}, // multi-line input
		{2, 1, 6}, // palette open
	}
	for _, c := range cases {
		if got := mailFooterHeight(c.palette, c.input, false); got != c.want {
			t.Errorf("mailFooterHeight(%d,%d,false)=%d, want %d", c.palette, c.input, got, c.want)
		}
	}
}

// Reserving the telemetry row shrinks the viewport by exactly one line, which is
// what keeps the status bar on-screen. This pins the viewport arithmetic that
// syncViewportHeight performs so a future refactor can't silently re-clip the
// bottom bar.
func TestViewportHeightLeavesRoomForTelemetryRow(t *testing.T) {
	const termHeight, header, banners = 30, 2, 0
	palette, input := 0, 1

	vpWithout := termHeight - header - banners - mailFooterHeight(palette, input, false)
	vpWith := termHeight - header - banners - mailFooterHeight(palette, input, true)

	if vpWith != vpWithout-1 {
		t.Fatalf("telemetry row must cost exactly one viewport line: without=%d with=%d", vpWithout, vpWith)
	}
}
