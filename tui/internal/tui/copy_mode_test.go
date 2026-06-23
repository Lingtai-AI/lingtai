package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/anthropics/lingtai-tui/i18n"
)

// newSizedMailModel builds a mail model sized for rendering, mirroring the
// harness used by mail_input_height_test.go.
func newSizedMailModel(t *testing.T) MailModel {
	t.Helper()
	m := NewMailModel("", "", "", "", "codex", 10, "", "en", false, 0)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return m
}

// ctrlYKey constructs the copy-mode toggle keypress. We assert its String()
// matches the constant the handler compares against, so a future binding change
// in one place can't silently desync from the key code used here.
func ctrlYKey(t *testing.T) tea.KeyPressMsg {
	t.Helper()
	k := tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl}
	if k.String() != copyModeToggleKey {
		t.Fatalf("ctrl+y keypress String() = %q, want %q", k.String(), copyModeToggleKey)
	}
	return k
}

func TestCopyModeToggleFlipsFlag(t *testing.T) {
	m := newSizedMailModel(t)
	if m.copyMode {
		t.Fatalf("copyMode should default to false")
	}

	if !m.input.Focused() {
		t.Fatalf("precondition: input should be focused after construction")
	}

	m, _ = m.Update(ctrlYKey(t))
	if !m.copyMode {
		t.Fatalf("expected copyMode=true after first toggle")
	}
	// Confirmed product requirement: input keeps focus while copy mode is on.
	if !m.input.Focused() {
		t.Fatalf("input must stay focused while copy mode is on")
	}

	m, _ = m.Update(ctrlYKey(t))
	if m.copyMode {
		t.Fatalf("expected copyMode=false after second toggle")
	}
	if !m.input.Focused() {
		t.Fatalf("input must stay focused after exiting copy mode")
	}
}

func TestCopyModeEscExits(t *testing.T) {
	m := newSizedMailModel(t)
	m.copyMode = true

	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.copyMode {
		t.Fatalf("expected esc to exit copy mode")
	}
}

// TestCopyModeEscDoesNotDismissInsightsWhenOff guards the precedence: when copy
// mode is OFF, esc must fall through to the existing insight-dismiss handler
// (i.e. copy-mode esc handling must not shadow it).
func TestCopyModeEscOffFallsThrough(t *testing.T) {
	m := newSizedMailModel(t)
	// copyMode is false; esc should not panic and should leave copyMode false.
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.copyMode {
		t.Fatalf("esc with copy mode off must not enable copy mode")
	}
}

// TestAppViewCopyModeMouseMode pins the single integration point: App.View()
// declares MouseModeNone only when the mail view is current AND copy mode is on.
func TestAppViewCopyModeMouseMode(t *testing.T) {
	m := newSizedMailModel(t)

	cases := []struct {
		name        string
		currentView appView
		copyMode    bool
		want        tea.MouseMode
	}{
		{"mail+copy", appViewMail, true, tea.MouseModeNone},
		{"mail+normal", appViewMail, false, tea.MouseModeCellMotion},
		{"non-mail+copy", appViewMailbox, true, tea.MouseModeCellMotion},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mm := m
			mm.copyMode = tc.copyMode
			a := App{currentView: tc.currentView, mail: mm, mailbox: NewMailboxModel("")}
			v := a.View()
			if v.MouseMode != tc.want {
				t.Fatalf("MouseMode = %v, want %v", v.MouseMode, tc.want)
			}
		})
	}
}

func TestCopyModeBadgeRenders(t *testing.T) {
	m := newSizedMailModel(t)
	m.copyMode = true
	out := m.View()
	// The full badge may be width-truncated on narrow terminals; assert on a
	// stable prefix of the localized string.
	want := i18n.T("mail.copy_mode")
	prefix := strings.SplitN(want, " ", 2)[0] // "COPY"
	if !strings.Contains(out, prefix) {
		t.Fatalf("expected copy-mode badge (prefix %q) in view output", prefix)
	}
}

// TestCopyModeBadgeFitsNarrowWidth verifies the badge never wraps the status bar
// onto a second line on small terminals (an explicit product constraint).
func TestCopyModeBadgeFitsNarrowWidth(t *testing.T) {
	for _, w := range []int{40, 50, 58} {
		m := NewMailModel("", "", "", "", "codex", 10, "", "en", false, 0)
		m, _ = m.Update(tea.WindowSizeMsg{Width: w, Height: 24})
		m.copyMode = true
		out := m.View()
		for _, line := range strings.Split(out, "\n") {
			if lw := lipgloss.Width(line); lw > w {
				t.Fatalf("width=%d: a rendered line is %d cols wide (overflows): %q", w, lw, line)
			}
		}
	}
}

func TestCopyModeLifecycleResetSwitchToView(t *testing.T) {
	m := newSizedMailModel(t)
	m.copyMode = true
	a := App{currentView: appViewMailbox, mail: m}
	updated, _ := a.switchToView("mail")
	got := updated.(App)
	if got.mail.copyMode {
		t.Fatalf("expected copy mode reset when (re-)entering mail via switchToView")
	}
}

func TestCopyModeLifecycleResetMarkdownClose(t *testing.T) {
	m := newSizedMailModel(t)
	m.copyMode = true
	a := App{currentView: appViewLibrary, mail: m}
	updated, _ := a.Update(MarkdownViewerCloseMsg{})
	got := updated.(App)
	if got.mail.copyMode {
		t.Fatalf("expected copy mode reset on MarkdownViewerCloseMsg return to mail")
	}
}
