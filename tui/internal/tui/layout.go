package tui

import (
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthropics/lingtai-tui/i18n"
)

// LayoutBudget is the root-owned layout contract. The root App reserves rows
// for persistent chrome (top status banners, and — in the future — a bottom
// status area) BEFORE the child screen sizes itself, then forwards the reduced
// height to the child via a WindowSizeMsg. View() composes root chrome around
// the child content, so chrome never gets appended after a child has already
// rendered at full terminal height.
//
// This is the foundation a future persistent status line / chrome consumer
// plugs into: declare its rows in layoutBudget(), render them in the chrome
// helpers, and the child automatically yields the space. Today the only
// consumer is the startup banner (one top row, shown only when non-empty).
type LayoutBudget struct {
	Width  int
	Height int // full terminal height

	TopChromeRows    int // rows reserved at the top for root chrome
	BottomChromeRows int // rows reserved at the bottom for root chrome (0 today)
	ChildHeight      int // height handed to the child screen (clamped >= 0)
}

// ChildWindowSize is the WindowSizeMsg the child screen should receive: full
// width, reduced height. Both Update's incoming-WindowSizeMsg handler and
// sendSize() forward this so the child never sizes to the full terminal height
// when root chrome is present.
func (b LayoutBudget) ChildWindowSize() tea.WindowSizeMsg {
	return tea.WindowSizeMsg{Width: b.Width, Height: b.ChildHeight}
}

// topChromeRows reports how many rows the root reserves at the top: one for the
// startup banner when non-empty, one when visiting another agent, plus one for
// the global select-mode indicator when select mode is on (any non-mail view).
// They stack when present.
func (a App) topChromeRows() int {
	rows := 0
	if a.startupBanner != "" {
		rows++
	}
	if a.visiting {
		rows++
	}
	if a.selectModeIndicatorActive() {
		rows++
	}
	return rows
}

// selectModeIndicatorActive reports whether the root should render its global
// select-mode indicator. The mail view owns its own copyMode badge, so the
// root indicator is scoped to every other view.
func (a App) selectModeIndicatorActive() bool {
	return a.selectMode && a.currentView != appViewMail
}

// bottomChromeRows reports how many rows the root reserves at the bottom. There
// is no bottom chrome consumer yet, so this is always zero; it exists so a
// future status area has an explicit, testable hook rather than a hard-coded
// assumption that the child owns the last row.
func (a App) bottomChromeRows() int {
	return 0
}

// layoutBudget computes the current root layout budget from terminal size and
// the rows reserved by root chrome. ChildHeight is clamped to >= 0 so a
// terminal too short to fit the chrome never forwards a negative height
// (screens re-clamp to their own minimums internally).
func (a App) layoutBudget() LayoutBudget {
	top := a.topChromeRows()
	bottom := a.bottomChromeRows()
	child := a.height - top - bottom
	if child < 0 {
		child = 0
	}
	return LayoutBudget{
		Width:            a.width,
		Height:           a.height,
		TopChromeRows:    top,
		BottomChromeRows: bottom,
		ChildHeight:      child,
	}
}

// topChrome renders the root-owned top chrome (the rows counted by
// topChromeRows). Returns "" when there is no top chrome. The returned string,
// when non-empty, is exactly topChromeRows() rows tall and is composed ABOVE
// the child content in View(). The startup banner, visit banner, and select-mode
// indicator stack in that order when present.
func (a App) topChrome() string {
	var rows []string
	if a.startupBanner != "" {
		rows = append(rows, "  "+lipgloss.NewStyle().Foreground(ColorStuck).Render(a.startupBanner))
	}
	if a.visiting {
		rows = append(rows, a.visitBanner())
	}
	if a.selectModeIndicatorActive() {
		rows = append(rows, a.selectModeIndicator())
	}
	if len(rows) == 0 {
		return ""
	}
	return strings.Join(rows, "\n")
}

func (a App) visitBanner() string {
	target := firstNonEmpty(a.visitTargetAgentName, filepath.Base(a.orchDir), "?")
	targetProject := filepath.Base(filepath.Dir(a.projectDir))
	original := firstNonEmpty(a.visitOriginalOrchName, filepath.Base(a.visitOriginalOrchDir), "?")
	originalProject := filepath.Base(filepath.Dir(a.visitOriginalProjectDir))
	text := i18n.TF("projects.visit_banner", target, targetProject, original, originalProject)
	if a.width > 0 {
		text = ansi.Truncate("  "+text, a.width-1, "…")
	} else {
		text = "  " + text
	}
	return lipgloss.NewStyle().Foreground(ColorAccent).Render(text)
}

// selectModeIndicator renders the one-row global select-mode badge. It reuses
// the mail view's localized "mail.copy_mode" string so the wording stays
// centralized (drag to select · ⌘C copy · ctrl+y/esc exit), styled with the
// same accent the mail badge uses. Truncated to the terminal width so it never
// wraps the reserved single row.
func (a App) selectModeIndicator() string {
	badge := "  ◉ " + i18n.T("mail.copy_mode")
	if a.width > 0 {
		badge = ansi.Truncate(badge, a.width-1, "…")
	}
	return lipgloss.NewStyle().Foreground(ColorAccent).Render(badge)
}

// composeWithChrome stacks root top chrome above the child content. With no
// chrome it returns the child content unchanged, so screens with no banner
// render identically to before this contract existed.
func (a App) composeWithChrome(child string) string {
	top := a.topChrome()
	if top == "" {
		return child
	}
	return top + "\n" + child
}
