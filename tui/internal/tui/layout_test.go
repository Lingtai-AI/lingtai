package tui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// budgetApp builds an App parked in the /help view with the given startup
// banner. The help view has a real constructor (unlike a zero-value MailModel,
// whose textarea is nil) and records the height it is sized to in
// help.inner.height — which is what these tests inspect to prove the child
// received the reduced budget. The startupBanner is root-owned chrome,
// independent of which view is current, so the help view exercises the same
// layout contract the mail banner uses in production.
func budgetApp(t *testing.T, banner string) App {
	t.Helper()
	return App{
		currentView:   appViewHelp,
		help:          NewHelpModel(),
		startupBanner: banner,
	}
}

// TestLayoutBudgetNoChrome: with no root chrome, the child budget equals the
// terminal size and there are zero reserved rows.
func TestLayoutBudgetNoChrome(t *testing.T) {
	a := budgetApp(t, "")
	a.width = 80
	a.height = 24

	b := a.layoutBudget()

	if b.TopChromeRows != 0 || b.BottomChromeRows != 0 {
		t.Fatalf("no banner: chrome rows = (%d, %d), want (0, 0)", b.TopChromeRows, b.BottomChromeRows)
	}
	if b.ChildHeight != 24 {
		t.Fatalf("no banner: ChildHeight = %d, want 24", b.ChildHeight)
	}
	if cs := b.ChildWindowSize(); cs.Width != 80 || cs.Height != 24 {
		t.Fatalf("no banner: ChildWindowSize = %dx%d, want 80x24", cs.Width, cs.Height)
	}
}

// TestLayoutBudgetTopChrome: a non-empty startupBanner reserves exactly one top
// row, reducing the child height by one.
func TestLayoutBudgetTopChrome(t *testing.T) {
	a := budgetApp(t, "⚠ something")
	a.width = 80
	a.height = 24

	b := a.layoutBudget()

	if b.TopChromeRows != 1 {
		t.Fatalf("banner: TopChromeRows = %d, want 1", b.TopChromeRows)
	}
	if b.BottomChromeRows != 0 {
		t.Fatalf("banner: BottomChromeRows = %d, want 0", b.BottomChromeRows)
	}
	if b.ChildHeight != 23 {
		t.Fatalf("banner: ChildHeight = %d, want 23 (24 - 1 top chrome)", b.ChildHeight)
	}
	if cs := b.ChildWindowSize(); cs.Width != 80 || cs.Height != 23 {
		t.Fatalf("banner: ChildWindowSize = %dx%d, want 80x23", cs.Width, cs.Height)
	}
}

// TestUpdateForwardsReducedHeight: the direct Update(WindowSizeMsg) path must
// forward the *child* window size (reduced by top chrome), not the raw
// terminal height.
func TestUpdateForwardsReducedHeight(t *testing.T) {
	a := budgetApp(t, "⚠ banner")

	updated, _ := a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	got := updated.(App)

	if got.help.inner.height != 23 {
		t.Fatalf("Update forwarded child height = %d, want 23 (reduced by 1 banner row)", got.help.inner.height)
	}
	// Root still records the full terminal height.
	if got.height != 24 {
		t.Fatalf("Update recorded app height = %d, want 24 (full terminal)", got.height)
	}
}

// TestUpdateNoChromeForwardsFullHeight: without root chrome the child receives
// the full terminal height.
func TestUpdateNoChromeForwardsFullHeight(t *testing.T) {
	a := budgetApp(t, "")

	updated, _ := a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	got := updated.(App)

	if got.help.inner.height != 24 {
		t.Fatalf("Update forwarded child height = %d, want 24 (no chrome)", got.help.inner.height)
	}
}

// TestSendSizeForwardsReducedHeight: the sendSize() cmd path must also forward
// the reduced child size, matching the direct Update path.
func TestSendSizeForwardsReducedHeight(t *testing.T) {
	a := budgetApp(t, "⚠ banner")
	a.width = 80
	a.height = 24

	msg := runCmd(a.sendSize())
	childSize, ok := msg.(childWindowSizeMsg)
	if !ok {
		t.Fatalf("sendSize produced %T, want childWindowSizeMsg", msg)
	}
	ws := childSize.WindowSizeMsg
	if ws.Width != 80 || ws.Height != 23 {
		t.Fatalf("sendSize forwarded %dx%d, want 80x23 (reduced by 1 banner row)", ws.Width, ws.Height)
	}
}

// TestSendSizeNoChromeForwardsFullHeight: without chrome, sendSize forwards the
// full terminal size.
func TestSendSizeNoChromeForwardsFullHeight(t *testing.T) {
	a := budgetApp(t, "")
	a.width = 80
	a.height = 24

	msg := runCmd(a.sendSize())
	ws := msg.(childWindowSizeMsg).WindowSizeMsg
	if ws.Width != 80 || ws.Height != 24 {
		t.Fatalf("sendSize forwarded %dx%d, want 80x24 (no chrome)", ws.Width, ws.Height)
	}
}

func TestSendSizeDoesNotOverwriteRawTerminalHeight(t *testing.T) {
	a := budgetApp(t, "root chrome")

	model, _ := a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	a = model.(App)
	if a.height != 24 || a.help.inner.height != 23 {
		t.Fatalf("initial size: app=%d child=%d, want app 24 child 23", a.height, a.help.inner.height)
	}

	first := runCmd(a.sendSize())
	model, _ = a.Update(first)
	a = model.(App)
	if a.height != 24 || a.help.inner.height != 23 {
		t.Fatalf("after first synthetic size: app=%d child=%d, want stable app 24 child 23", a.height, a.help.inner.height)
	}

	second := runCmd(a.sendSize())
	model, _ = a.Update(second)
	a = model.(App)
	if a.height != 24 || a.help.inner.height != 23 {
		t.Fatalf("after second synthetic size: app=%d child=%d, want stable app 24 child 23", a.height, a.help.inner.height)
	}
}

func TestVisitingDoesNotReserveRootChrome(t *testing.T) {
	a := budgetApp(t, "")
	a.visiting = true
	a.width = 80
	a.height = 24

	b := a.layoutBudget()

	if b.TopChromeRows != 0 {
		t.Fatalf("visiting TopChromeRows = %d, want 0", b.TopChromeRows)
	}
	if b.ChildHeight != 24 {
		t.Fatalf("visiting ChildHeight = %d, want full height 24", b.ChildHeight)
	}
}

// TestViewComposesBannerOnce: View() must include the banner text exactly once
// and compose it outside the child content (as root-owned top chrome).
func TestViewComposesBannerOnce(t *testing.T) {
	a := budgetApp(t, "UNIQUEBANNERTOKEN")
	a.width = 80
	a.height = 24
	// Size the child so its View() renders.
	updated, _ := a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	a = updated.(App)

	out := a.View().Content
	if n := strings.Count(out, "UNIQUEBANNERTOKEN"); n != 1 {
		t.Fatalf("banner token appears %d times in View(), want exactly 1", n)
	}
}

// TestLayoutBudgetClampsSmallHeight: a terminal too short to fit the reserved
// chrome must clamp ChildHeight to >= 0 (never negative), without panicking.
func TestLayoutBudgetClampsSmallHeight(t *testing.T) {
	a := budgetApp(t, "⚠ banner")
	a.width = 80
	a.height = 0

	b := a.layoutBudget()
	if b.ChildHeight < 0 {
		t.Fatalf("ChildHeight = %d, want >= 0 (clamped)", b.ChildHeight)
	}
	if cs := b.ChildWindowSize(); cs.Height < 0 {
		t.Fatalf("ChildWindowSize.Height = %d, want >= 0 (clamped)", cs.Height)
	}
}

func TestLayoutBudgetOwnsHorizontalGeometryWithoutChangingCurrentWidth(t *testing.T) {
	a := budgetApp(t, "")
	a.width = 80
	a.height = 24

	b := a.layoutBudget()
	if got := b.TerminalWidth; got != 80 {
		t.Fatalf("TerminalWidth = %d, want 80", got)
	}
	if got := b.ContentWidth; got != 80 {
		t.Fatalf("ContentWidth = %d, want unchanged current width 80", got)
	}
	if got := b.RailWidth; got != 0 {
		t.Fatalf("RailWidth = %d, want 0 in the layout-foundation PR", got)
	}
	if got := b.MinChatWidth; got != minimumChatWidth {
		t.Fatalf("MinChatWidth = %d, want contract value %d", got, minimumChatWidth)
	}
	if b.RailVisible {
		t.Fatal("RailVisible = true, want false while RailWidth is zero")
	}
	if got := b.ChildWindowSize().Width; got != b.ContentWidth {
		t.Fatalf("child width = %d, want ContentWidth", got)
	}
}

func TestLayoutBudgetHorizontalTinyAndThresholdWidthsAreSafe(t *testing.T) {
	const requestedRailWidth = 20
	minimum := minimumChatWidth
	threshold := minimum + requestedRailWidth

	for _, width := range []int{-1, 0, 1, minimum - 1, minimum, threshold - 1, threshold, threshold + 1} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			terminal, content, rail, visible := resolveHorizontalLayout(
				width,
				requestedRailWidth,
				minimum,
				true,
			)

			if terminal < 0 || content < 0 || rail < 0 {
				t.Fatalf("width=%d produced negative geometry: terminal=%d content=%d rail=%d", width, terminal, content, rail)
			}
			if content+rail != terminal {
				t.Fatalf("width=%d geometry does not partition terminal: content(%d)+rail(%d) != terminal(%d)", width, content, rail, terminal)
			}

			wantVisible := width >= threshold
			if visible != wantVisible {
				t.Fatalf("width=%d RailVisible=%v, want %v at threshold %d", width, visible, wantVisible, threshold)
			}
			if visible {
				if rail != requestedRailWidth || content < minimum {
					t.Fatalf("width=%d visible geometry: content=%d rail=%d, want rail=%d and content >= %d", width, content, rail, requestedRailWidth, minimum)
				}
			} else if rail != 0 || content != terminal {
				t.Fatalf("width=%d hidden rail changed content: content=%d rail=%d terminal=%d", width, content, rail, terminal)
			}
		})
	}

	terminal, content, rail, visible := resolveHorizontalLayout(threshold, requestedRailWidth, minimum, false)
	if visible || rail != 0 || content != terminal {
		t.Fatalf("non-mail geometry consumed rail: terminal=%d content=%d rail=%d visible=%v", terminal, content, rail, visible)
	}
}

func mailLayoutApp(t *testing.T) App {
	t.Helper()
	dir := t.TempDir()
	humanDir := filepath.Join(dir, "human")
	orchDir := filepath.Join(dir, "main")
	for _, path := range []string{humanDir, orchDir} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mail := NewMailModel(humanDir, "human@local", "~", orchDir, "main", 50, dir, "en", false, 0)
	return App{currentView: appViewMail, mail: mail}
}

func assertMailHorizontalGeometry(t *testing.T, a App) {
	t.Helper()
	b := a.layoutBudget()
	content := b.ContentWidth
	if a.mail.width != content {
		t.Fatalf("MailModel width = %d, want budget ContentWidth %d", a.mail.width, content)
	}
	if got := a.mail.viewport.Width(); got != content {
		t.Fatalf("mail viewport width = %d, want budget ContentWidth %d", got, content)
	}
	if got := a.mail.input.width; got != content {
		t.Fatalf("mail composer width = %d, want budget ContentWidth %d", got, content)
	}

	lines := strings.Split(a.mail.View(), "\n")
	if len(lines) < 2 || lipgloss.Width(lines[1]) != content {
		t.Fatalf("mail header width = %d, want budget ContentWidth %d", lipgloss.Width(lines[1]), content)
	}
	foundFooter := false
	for _, line := range lines {
		if strings.Contains(line, "Email To:") {
			foundFooter = true
			if got := lipgloss.Width(line); got != content {
				t.Fatalf("mail footer width = %d, want budget ContentWidth %d", got, content)
			}
		}
		if got := lipgloss.Width(line); got > content {
			t.Fatalf("mail line overflows ContentWidth: got %d > %d: %q", got, content, line)
		}
	}
	if !foundFooter {
		t.Fatal("mail footer geometry assertion could not find Email To line")
	}
}

func TestRawAndSyntheticResizeShareMailHorizontalGeometry(t *testing.T) {
	a := mailLayoutApp(t)
	model, _ := a.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	a = model.(App)
	assertMailHorizontalGeometry(t, a)

	msg := runCmd(a.sendSize())
	model, _ = a.Update(msg)
	a = model.(App)
	assertMailHorizontalGeometry(t, a)
}

func TestNonMailViewKeepsFullTerminalWidth(t *testing.T) {
	a := budgetApp(t, "")
	model, _ := a.Update(tea.WindowSizeMsg{Width: 73, Height: 24})
	a = model.(App)

	b := a.layoutBudget()
	if got := b.ContentWidth; got != 73 {
		t.Fatalf("non-mail ContentWidth = %d, want full terminal width 73", got)
	}
	if got := a.help.inner.width; got != 73 {
		t.Fatalf("non-mail child width = %d, want full terminal width 73", got)
	}
}
