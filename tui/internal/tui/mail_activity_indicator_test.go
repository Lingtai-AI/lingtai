package tui

import (
	"testing"
	"time"
)

// TestStateGlyph verifies each agent state maps to its expected badge glyph,
// case-insensitively. ACTIVE uses the animated spinner frame; the rest are
// distinct static glyphs (with color carrying IDLE vs STUCK).
func TestStateGlyph(t *testing.T) {
	cases := []struct {
		state string
		want  string
	}{
		{"ACTIVE", spinnerFrames[0]},
		{"active", spinnerFrames[0]}, // case-insensitive
		{"IDLE", "◉"},
		{"idle", "◉"},
		{"STUCK", "◉"}, // color (ColorStuck) carries the distinction
		{"ASLEEP", "◌"},
		{"SUSPENDED", "○"},
		{"REFRESHING", "⟳"},
		{"refreshing", "⟳"},
		{"", "◉"},
		{"bogus", "◉"},
	}
	for _, c := range cases {
		m := MailModel{orchState: c.state, pulseTick: 0}
		if got := m.stateGlyph(); got != c.want {
			t.Errorf("stateGlyph(%q) = %q, want %q", c.state, got, c.want)
		}
	}
}

// TestStateGlyphActiveAnimates verifies the ACTIVE spinner advances with
// pulseTick (modulo the frame count) so the badge visibly animates.
func TestStateGlyphActiveAnimates(t *testing.T) {
	for i := 0; i < len(spinnerFrames)*2+1; i++ {
		m := MailModel{orchState: "ACTIVE", pulseTick: i}
		want := spinnerFrames[i%len(spinnerFrames)]
		if got := m.stateGlyph(); got != want {
			t.Errorf("pulseTick=%d: stateGlyph() = %q, want %q", i, got, want)
		}
	}
}

// TestActiveElapsed verifies the elapsed suffix renders only while ACTIVE with
// a non-zero start time, formatted as seconds under a minute and minutes above.
// Offsets are mid-interval to avoid second/minute boundary flake.
func TestActiveElapsed(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name  string
		state string
		since time.Time
		want  string
	}{
		{"not active drops timer", "idle", now.Add(-30 * time.Second), ""},
		{"active but zero start", "active", time.Time{}, ""},
		{"active seconds", "active", now.Add(-12500 * time.Millisecond), " 12s"},
		{"active minutes", "active", now.Add(-210 * time.Second), " 3m"},
	}
	for _, c := range cases {
		m := MailModel{orchState: c.state, activeSince: c.since}
		if got := m.activeElapsed(); got != c.want {
			t.Errorf("%s: activeElapsed() = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestActiveSinceLifecycle verifies the activeSince timestamp is set on entry to
// ACTIVE, preserved while staying ACTIVE, and cleared on any non-ACTIVE refresh
// (including the synthesized suspended/refreshing states, which arrive as
// non-ACTIVE through the same mailRefreshPayload path).
func TestActiveSinceLifecycle(t *testing.T) {
	dir := t.TempDir()
	m := NewMailModel(dir, "human", dir, dir, "orch", 20, dir, "en", false, 0)
	m = sizeMail(t, m)

	// Enter ACTIVE → timer starts.
	m, _ = m.Update(mailRefreshPayload{state: "active", alive: true})
	if m.activeSince.IsZero() {
		t.Fatal("activeSince should be set on entering ACTIVE")
	}
	first := m.activeSince

	// Stay ACTIVE → timestamp preserved (not reset each refresh).
	m, _ = m.Update(mailRefreshPayload{state: "active", alive: true})
	if !m.activeSince.Equal(first) {
		t.Errorf("activeSince should be preserved while staying ACTIVE; was %v, now %v", first, m.activeSince)
	}

	// Leave ACTIVE → timer cleared so the badge drops the elapsed suffix.
	m, _ = m.Update(mailRefreshPayload{state: "idle", alive: true})
	if !m.activeSince.IsZero() {
		t.Error("activeSince should be cleared when leaving ACTIVE")
	}
}
