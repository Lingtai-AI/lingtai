package tui

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/anthropics/lingtai-tui/i18n"
)

// wizardFooterButton represents one of the two button slots a wizard
// page renders below its content. Pages model the buttons as the final
// two positions in their existing cursor (one past the last row →
// Back; two past → Next), so navigating with ↑↓ moves between rows
// and buttons uniformly. Enter on a focused button activates it.
type wizardFooterButton int

const (
	wizardFooterNone wizardFooterButton = iota
	wizardFooterBack
	wizardFooterNext
)

// renderWizardFooter renders the Back/Next button row. Pass
// `wizardFooterNone` for the inactive state (cursor is on a row),
// or one of the button constants when the cursor is on that button.
//
// `showBack` controls whether the Back button is rendered at all
// (e.g. the first wizard page has nothing to go back to).
func renderWizardFooter(focused wizardFooterButton, showBack bool) string {
	backLabel := i18n.T("firstrun.button.back")
	nextLabel := i18n.T("firstrun.button.next")

	render := func(label string, isFocused bool) string {
		bracket := "[ "
		bracketClose := " ]"
		if isFocused {
			return lipgloss.NewStyle().
				Bold(true).
				Foreground(ColorAccent).
				Render(bracket + label + bracketClose)
		}
		return StyleFaint.Render(bracket + label + bracketClose)
	}

	var b strings.Builder
	b.WriteString("\n  ")
	if showBack {
		b.WriteString(render(backLabel, focused == wizardFooterBack))
		b.WriteString("   ")
	}
	b.WriteString(render(nextLabel, focused == wizardFooterNext))
	b.WriteString("\n")
	return b.String()
}
