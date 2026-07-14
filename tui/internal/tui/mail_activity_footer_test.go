package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthropics/lingtai-tui/i18n"
)

// emailToLine returns the ANSI-stripped "Email To:" footer line from a full
// View(), or "" if absent. The activity indicator lives on this line, so tests
// isolate it from the header (which has its own separate state badge / center
// spinner). Stripping ANSI keeps assertions free of color-code digits.
func emailToLine(view string) string {
	for _, line := range strings.Split(ansi.Strip(view), "\n") {
		if strings.Contains(line, "Email To:") {
			return line
		}
	}
	return ""
}

// TestFooterShowsActivityIndicatorWhenActive verifies the live indicator is
// rendered on the "Email To:" interaction line — the ACTIVE spinner glyph and
// the localized state label — so the human sees agent activity where their
// attention already is, without entering verbose mode.
func TestFooterShowsActivityIndicatorWhenActive(t *testing.T) {
	dir := t.TempDir()
	m := NewMailModel(dir, "human", dir, dir, "orch", 20, dir, "en", false, 0)
	m = sizeMail(t, m)

	m, _ = m.Update(mailRefreshPayload{state: "active", alive: true})

	footer := emailToLine(m.View())
	if footer == "" {
		t.Fatal("no Email To: footer line in view")
	}
	if !strings.Contains(footer, spinnerFrames[0]) {
		t.Errorf("Email To: line should carry the ACTIVE spinner glyph %q; got %q", spinnerFrames[0], footer)
	}
	if label := i18n.T("state.active"); !strings.Contains(footer, label) {
		t.Errorf("Email To: line should carry the state label %q; got %q", label, footer)
	}
}

// TestFooterIndicatorIdleHasNoTimer verifies that when the agent is IDLE the
// footer shows the static glyph + label but no elapsed timer (no digits).
func TestFooterIndicatorIdleHasNoTimer(t *testing.T) {
	dir := t.TempDir()
	m := NewMailModel(dir, "human", dir, dir, "orch", 20, dir, "en", false, 0)
	m = sizeMail(t, m)

	m, _ = m.Update(mailRefreshPayload{state: "idle", alive: true})

	footer := emailToLine(m.View())
	if footer == "" {
		t.Fatal("no Email To: footer line in view")
	}
	if !strings.Contains(footer, "◉") {
		t.Errorf("idle Email To: line should carry the static ◉ glyph; got %q", footer)
	}
	// The idle label ("idle") and agent name ("orch") have no digits, so any
	// digit on the ANSI-stripped line would signal a stray elapsed timer.
	if strings.ContainsAny(footer, "0123456789") {
		t.Errorf("idle Email To: line should not contain an elapsed timer; got %q", footer)
	}
}
