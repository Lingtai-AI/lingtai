package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestAdaptiveInputMaxHeight(t *testing.T) {
	tests := []struct {
		height int
		want   int
	}{
		{height: 12, want: 4},
		{height: 24, want: 8},
		{height: 60, want: 14},
		{height: 4, want: 1},
	}
	for _, tt := range tests {
		if got := adaptiveInputMaxHeight(tt.height); got != tt.want {
			t.Errorf("adaptiveInputMaxHeight(%d) = %d, want %d", tt.height, got, tt.want)
		}
	}
}

func TestMaybeShowEditorHintAtMaxHeight(t *testing.T) {
	m := NewMailModel("", "", "", "", "codex", 10, "", "en", false, 0)
	m.height = 24
	m.updateInputMaxHeight()
	m.input.SetWidth(80)
	m.input.SetValue("a\nb\nc\nd\ne\nf\ng\nh\ni")

	m.maybeShowEditorHint()

	if m.statusFlash == "" {
		t.Fatalf("expected editor hint status flash")
	}
	if m.statusExpiry.IsZero() {
		t.Fatalf("expected editor hint to have expiry")
	}
}

func TestMaybeShowEditorHintNotAtMaxHeight(t *testing.T) {
	m := NewMailModel("", "", "", "", "codex", 10, "", "en", false, 0)
	m.height = 24
	m.updateInputMaxHeight()
	m.input.SetWidth(80)
	m.input.SetValue("short")

	m.maybeShowEditorHint()

	if m.statusFlash != "" {
		t.Fatalf("unexpected editor hint status flash: %q", m.statusFlash)
	}
}

func TestPasteAtMaxHeightShowsEditorHint(t *testing.T) {
	m := NewMailModel("", "", "", "", "codex", 10, "", "en", false, 0)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	m, _ = m.Update(tea.PasteMsg{Content: strings.Repeat("line\n", 12)})

	if m.statusFlash == "" {
		t.Fatalf("expected paste at max height to show editor hint")
	}
}
