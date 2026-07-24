package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthropics/lingtai-tui/i18n"
)

const minimumChatWidth = 60

// LayoutBudget is the root-owned vertical and horizontal layout contract. The
// root App reserves rows for persistent chrome and columns for the mail-only
// Agent rail BEFORE the child screen sizes itself, then forwards the resulting
// content rectangle via a WindowSizeMsg. View() composes root chrome around the
// child content, so chrome never gets appended after a child has already
// rendered at full terminal height.
type LayoutBudget struct {
	TerminalWidth int // full terminal width, clamped >= 0
	Height        int // full terminal height
	ContentWidth  int // width handed to the child screen, clamped >= 0
	RailWidth     int // root-owned mail rail width
	MinChatWidth  int // minimum usable content width when a rail is requested
	RailVisible   bool

	TopChromeRows    int // rows reserved at the top for root chrome
	BottomChromeRows int // rows reserved at the bottom for root chrome (0 today)
	ChildHeight      int // height handed to the child screen (clamped >= 0)
}

// ChildWindowSize is the WindowSizeMsg the child screen should receive: the
// budgeted content width and reduced height. Both Update's incoming raw resize
// handler and sendSize's root-synthesized path call this method, so viewport,
// composer, header, and footer all receive the same geometry.
func (b LayoutBudget) ChildWindowSize() tea.WindowSizeMsg {
	return tea.WindowSizeMsg{Width: b.ContentWidth, Height: b.ChildHeight}
}

// topChromeRows reports how many rows the root reserves at the top: one for the
// startup banner when non-empty, plus one for the global select-mode indicator
// when select mode is on (any non-mail view). They stack when present.
func (a App) topChromeRows() int {
	rows := 0
	if a.startupBanner != "" {
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

// resolveHorizontalLayout applies the root's one horizontal subtraction. A rail
// is visible only in an allowed view and only when the requested width leaves at
// least minChatWidth columns for content. Hidden rails consume zero columns.
func resolveHorizontalLayout(terminalWidth, requestedRailWidth, minChatWidth int, railAllowed bool) (int, int, int, bool) {
	if terminalWidth < 0 {
		terminalWidth = 0
	}
	if minChatWidth < 0 {
		minChatWidth = 0
	}

	railVisible := railAllowed && requestedRailWidth > 0 &&
		terminalWidth-requestedRailWidth >= minChatWidth
	if !railVisible {
		return terminalWidth, terminalWidth, 0, false
	}
	return terminalWidth, terminalWidth - requestedRailWidth, requestedRailWidth, true
}

// layoutBudget computes the current root layout budget from terminal size and
// root-owned chrome. Horizontal dimensions are clamped before subtraction, and
// content width is reduced exactly once only when the 24-column Mail rail fits
// beside the minimum chat width. Non-Mail views request no rail.
func (a App) layoutBudget() LayoutBudget {
	top := a.topChromeRows()
	bottom := a.bottomChromeRows()
	child := a.height - top - bottom
	if child < 0 {
		child = 0
	}

	requestedRailWidth := 0
	if a.currentView == appViewMail && a.width >= agentRailWidth+minimumChatWidth+1 {
		requestedRailWidth = agentRailWidth
	}
	terminalWidth, contentWidth, railWidth, railVisible := resolveHorizontalLayout(
		a.width,
		requestedRailWidth,
		minimumChatWidth,
		a.currentView == appViewMail,
	)

	return LayoutBudget{
		TerminalWidth:    terminalWidth,
		Height:           a.height,
		ContentWidth:     contentWidth,
		RailWidth:        railWidth,
		MinChatWidth:     minimumChatWidth,
		RailVisible:      railVisible,
		TopChromeRows:    top,
		BottomChromeRows: bottom,
		ChildHeight:      child,
	}
}

// topChrome renders the root-owned top chrome (the rows counted by
// topChromeRows). Returns "" when there is no top chrome. The returned string,
// when non-empty, is exactly topChromeRows() rows tall and is composed ABOVE
// the child content in View(). The startup banner and select-mode indicator
// stack in that order when present.
func (a App) topChrome() string {
	var rows []string
	if a.startupBanner != "" {
		rows = append(rows, "  "+lipgloss.NewStyle().Foreground(ColorStuck).Render(a.startupBanner))
	}
	if a.selectModeIndicatorActive() {
		rows = append(rows, a.selectModeIndicator())
	}
	if len(rows) == 0 {
		return ""
	}
	return strings.Join(rows, "\n")
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
