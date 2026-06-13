package tui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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

// topChromeRows reports how many rows the root reserves at the top. Today that
// is one row for the startup banner when it is non-empty, else zero.
func (a App) topChromeRows() int {
	if a.startupBanner != "" {
		return 1
	}
	return 0
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
// the child content in View().
func (a App) topChrome() string {
	if a.startupBanner == "" {
		return ""
	}
	return "  " + lipgloss.NewStyle().Foreground(ColorStuck).Render(a.startupBanner)
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
