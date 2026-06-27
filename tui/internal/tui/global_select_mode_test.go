package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthropics/lingtai-tui/i18n"
)

// ctrl+y is the global select-mode toggle. In any non-mail view it flips the
// app-level selectMode flag (which drops mouse capture and shows the indicator);
// the mail view keeps owning its own copyMode via mail.go's handler.

func newMailboxApp() App {
	return App{currentView: appViewMailbox, mailbox: NewMailboxModel("")}
}

func TestGlobalSelectModeTogglesFromNonMailView(t *testing.T) {
	a := newMailboxApp()
	if a.selectMode {
		t.Fatalf("selectMode should default to false")
	}

	updated, _ := a.Update(ctrlYKey(t))
	got := updated.(App)
	if !got.selectMode {
		t.Fatalf("expected selectMode=true after ctrl+y in mailbox view")
	}

	updated, _ = got.Update(ctrlYKey(t))
	got = updated.(App)
	if got.selectMode {
		t.Fatalf("expected selectMode=false after second ctrl+y")
	}
}

func TestGlobalSelectModeEscExits(t *testing.T) {
	a := newMailboxApp()
	a.selectMode = true

	updated, _ := a.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	got := updated.(App)
	if got.selectMode {
		t.Fatalf("expected esc to exit global select mode")
	}
}

// When select mode is on in a non-mail view, the root drops mouse capture so the
// terminal can drag-select text — mirroring the mail view's MouseModeNone.
func TestGlobalSelectModeDropsMouseCapture(t *testing.T) {
	a := newMailboxApp()
	a.selectMode = true
	if mode := a.View().MouseMode; mode != tea.MouseModeNone {
		t.Fatalf("MouseMode = %v, want MouseModeNone while selectMode is on", mode)
	}

	a.selectMode = false
	if mode := a.View().MouseMode; mode != tea.MouseModeCellMotion {
		t.Fatalf("MouseMode = %v, want MouseModeCellMotion when selectMode is off", mode)
	}
}

// The conspicuous indicator: in a non-mail view with select mode on, the root
// renders the same localized copy/select badge the mail view uses, so the user
// always gets a visible reminder of the mode.
func TestGlobalSelectModeRendersIndicator(t *testing.T) {
	i18n.SetLang("en")
	t.Cleanup(func() { i18n.SetLang("en") })

	a := newMailboxApp()
	a.width, a.height = 80, 24
	a.selectMode = true
	out := ansi.Strip(a.View().Content)

	// Assert on a stable prefix of the localized badge ("COPY") so width
	// truncation on the chrome row can't false-fail.
	want := i18n.T("mail.copy_mode")
	prefix := strings.SplitN(want, " ", 2)[0]
	if !strings.Contains(out, prefix) {
		t.Fatalf("expected select-mode indicator (prefix %q) in non-mail view; got:\n%s", prefix, out)
	}

	a.selectMode = false
	out = ansi.Strip(a.View().Content)
	if strings.Contains(out, prefix) {
		t.Fatalf("indicator must not render when selectMode is off; got:\n%s", out)
	}
}

// Entering the mail view resets the app-level selectMode: the mail view owns its
// own copyMode + badge, so the global flag must not double up.
func TestGlobalSelectModeResetOnEnteringMail(t *testing.T) {
	a := newMailboxApp()
	a.selectMode = true
	updated, _ := a.switchToView("mail")
	got := updated.(App)
	if got.selectMode {
		t.Fatalf("expected selectMode reset when switching to mail view")
	}
}

// The global indicator reserves a top-chrome row (just like the startup banner),
// so the child view yields one row rather than the indicator overpainting it.
func TestGlobalSelectModeReservesChromeRow(t *testing.T) {
	a := budgetApp(t, "") // parked in /help, a non-mail view
	a.width, a.height = 80, 24

	if rows := a.layoutBudget().TopChromeRows; rows != 0 {
		t.Fatalf("selectMode off: TopChromeRows = %d, want 0", rows)
	}

	a.selectMode = true
	b := a.layoutBudget()
	if b.TopChromeRows != 1 {
		t.Fatalf("selectMode on: TopChromeRows = %d, want 1", b.TopChromeRows)
	}
	if b.ChildHeight != 23 {
		t.Fatalf("selectMode on: ChildHeight = %d, want 23 (24 - 1 top chrome)", b.ChildHeight)
	}
}

// In the mail view, ctrl+y keeps toggling mail.copyMode (existing behavior) and
// must NOT also set the app-level selectMode — otherwise both badges would show.
func TestMailViewCtrlYDoesNotSetGlobalSelectMode(t *testing.T) {
	m := newSizedMailModel(t)
	a := App{currentView: appViewMail, mail: m}

	updated, _ := a.Update(ctrlYKey(t))
	got := updated.(App)
	if got.selectMode {
		t.Fatalf("ctrl+y in mail view must not set app-level selectMode")
	}
	if !got.mail.copyMode {
		t.Fatalf("ctrl+y in mail view must still toggle mail.copyMode")
	}
}
