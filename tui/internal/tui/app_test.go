package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// runCmd executes a tea.Cmd and returns the message it produces (nil for a nil
// cmd). Used to inspect what command an Update returned without running a full
// program loop.
func runCmd(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	return cmd()
}

// helpApp builds an App parked in the /help view, sized so the inner markdown
// viewport has real dimensions to scroll.
func helpApp(t *testing.T) App {
	t.Helper()
	a := App{currentView: appViewHelp, help: NewHelpModel()}
	m, _ := a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return m.(App)
}

// TestHelpViewQClosesNotQuit guards the regression where /help could not be
// exited: with appViewHelp missing from the global "q" exclusion, pressing q
// in the help view quit the whole app instead of closing the viewer. The fix
// routes q into HelpModel, which emits MarkdownViewerCloseMsg.
func TestHelpViewQClosesNotQuit(t *testing.T) {
	a := helpApp(t)

	updated, cmd := a.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})

	if _, ok := runCmd(cmd).(tea.QuitMsg); ok {
		t.Fatal("q in /help quit the app; want viewer close")
	}
	if _, ok := runCmd(cmd).(MarkdownViewerCloseMsg); !ok {
		t.Fatalf("q in /help did not emit MarkdownViewerCloseMsg; cmd produced %T", runCmd(cmd))
	}
	_ = updated
}

// TestHelpViewCloseReturnsToMail verifies App routes MarkdownViewerCloseMsg
// from the help viewer back to the mail view.
func TestHelpViewCloseReturnsToMail(t *testing.T) {
	a := helpApp(t)

	updated, _ := a.Update(MarkdownViewerCloseMsg{})

	if got := updated.(App).currentView; got != appViewMail {
		t.Fatalf("after close, currentView = %v, want appViewMail", got)
	}
}

// TestHelpViewScrollReachesViewport guards the regression where pgdown/scroll
// keys never reached the markdown viewport because appViewHelp was omitted from
// the "forward to current view" switch. A pgdown should move the right viewport
// off its top position.
func TestHelpViewScrollReachesViewport(t *testing.T) {
	a := helpApp(t)

	if !a.help.inner.rightVP.AtTop() {
		t.Fatal("precondition: help viewport should start at top")
	}
	updated, _ := a.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if updated.(App).help.inner.rightVP.AtTop() {
		t.Fatal("pgdown in /help did not scroll viewport off the top")
	}
}

// TestHelpViewMouseWheelReachesViewport is the mouse analogue: a wheel-down
// event must reach the inner viewport and move it off its top position.
func TestHelpViewMouseWheelReachesViewport(t *testing.T) {
	a := helpApp(t)

	if !a.help.inner.rightVP.AtTop() {
		t.Fatal("precondition: help viewport should start at top")
	}
	updated, _ := a.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if updated.(App).help.inner.rightVP.AtTop() {
		t.Fatal("mouse wheel in /help did not scroll viewport off the top")
	}
}

func TestLoginCommandOpensSetupCredentialsSubpage(t *testing.T) {
	a := App{currentView: appViewMail, globalDir: t.TempDir(), orchDir: t.TempDir()}
	model, _ := a.handlePaletteCommand("login", "")
	got := model.(App)
	if got.currentView != appViewLogin {
		t.Fatalf("/login currentView = %v, want appViewLogin compatibility surface", got.currentView)
	}
	if !got.login.setupSubpage {
		t.Fatal("/login should route to the Setup credentials subpage, not a standalone login surface")
	}
}

func TestSetupCredentialsArgsOpenCredentialsSubpage(t *testing.T) {
	a := App{currentView: appViewMail, globalDir: t.TempDir(), projectDir: t.TempDir(), orchDir: t.TempDir()}
	model, _ := a.handlePaletteCommand("setup", "credentials")
	got := model.(App)
	if got.currentView != appViewLogin {
		t.Fatalf("/setup credentials currentView = %v, want appViewLogin credentials subpage", got.currentView)
	}
	if !got.login.setupSubpage {
		t.Fatal("/setup credentials should use setup-subpage login model")
	}
}
