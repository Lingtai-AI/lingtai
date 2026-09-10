package tui

import (
	"errors"
	"os/exec"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/i18n"
)

func TestLaunchCodexPoolStandaloneTUI_MissingBinary(t *testing.T) {
	oldLookPath := codexPoolStandaloneLookPath
	oldCommand := codexPoolStandaloneCommand
	defer func() {
		codexPoolStandaloneLookPath = oldLookPath
		codexPoolStandaloneCommand = oldCommand
	}()

	commandCalled := false
	codexPoolStandaloneLookPath = func(string) (string, error) {
		return "", errors.New("not found")
	}
	codexPoolStandaloneCommand = func(string, ...string) *exec.Cmd {
		commandCalled = true
		return exec.Command("true")
	}

	if cmd := launchCodexPoolStandaloneTUI(); cmd != nil {
		t.Fatal("missing codex-pool must not create an interactive command")
	}
	if commandCalled {
		t.Fatal("missing codex-pool must not construct a process")
	}
}

func TestLaunchCodexPoolStandaloneTUI_DispatchesTUICommand(t *testing.T) {
	oldLookPath := codexPoolStandaloneLookPath
	oldCommand := codexPoolStandaloneCommand
	defer func() {
		codexPoolStandaloneLookPath = oldLookPath
		codexPoolStandaloneCommand = oldCommand
	}()

	var gotName string
	var gotArgs []string
	codexPoolStandaloneLookPath = func(name string) (string, error) {
		return "/installed/codex-pool", nil
	}
	codexPoolStandaloneCommand = func(name string, args ...string) *exec.Cmd {
		gotName = name
		gotArgs = append([]string(nil), args...)
		// Construct only a harmless test command. The tea command is not run
		// here; parent validation owns any real interactive-process exercise.
		return exec.Command("true")
	}

	if cmd := launchCodexPoolStandaloneTUI(); cmd == nil {
		t.Fatal("installed codex-pool should produce an interactive tea command")
	}
	if gotName != codexPoolStandaloneBinary {
		t.Fatalf("command name = %q, want %q", gotName, codexPoolStandaloneBinary)
	}
	if len(gotArgs) != 1 || gotArgs[0] != "tui" {
		t.Fatalf("command args = %#v, want [tui]", gotArgs)
	}
}

func TestLoginModel_StandaloneEntryMissingBinaryIsActionable(t *testing.T) {
	oldLookPath := codexPoolStandaloneLookPath
	defer func() { codexPoolStandaloneLookPath = oldLookPath }()
	codexPoolStandaloneLookPath = func(string) (string, error) {
		return "", errors.New("not found")
	}

	m, cmd := (LoginModel{}).Update(tea.KeyPressMsg{Text: "p", Code: 'p'})
	if cmd != nil {
		t.Fatal("missing standalone binary must not start a process")
	}
	if m.message != i18n.T("login.codex_pool_standalone_missing") {
		t.Fatalf("missing-binary message = %q, want localized setup guidance", m.message)
	}
}

func TestLoginModel_StandaloneEntryReportsReturn(t *testing.T) {
	m, cmd := (LoginModel{}).Update(codexPoolStandaloneDoneMsg{})
	if cmd != nil {
		t.Fatal("return message must not schedule another command")
	}
	if m.message != i18n.T("login.codex_pool_standalone_returned") || !m.messageOK {
		t.Fatalf("return feedback = %q (success=%v), want localized success", m.message, m.messageOK)
	}
}

func TestLoginModel_PoolWarningScopedToBuiltinPool(t *testing.T) {
	m := LoginModel{
		activePreset: "codex",
		entries:      []loginEntry{{Provider: "codex", IsOAuth: true}},
		width:        120,
	}
	if view := m.View(); strings.Contains(view, i18n.T("login.codex_pool_deprecated_warning")) {
		t.Fatal("native single-account Codex must not show the old Pool deprecation warning")
	}

	m.activePreset = "codex-pool"
	if view := m.View(); !strings.Contains(view, i18n.T("login.codex_pool_deprecated_warning")) {
		t.Fatal("old builtin codex-pool consumer should show the scoped deprecation warning")
	}
}

// Exercise the actual outer App route, not only a direct LoginModel call.
func TestApp_StandaloneCompletionReachesLogin(t *testing.T) {
	a := App{currentView: appViewLogin, login: LoginModel{}}
	updated, cmd := a.Update(codexPoolStandaloneDoneMsg{})
	if cmd != nil {
		t.Fatal("completion must not schedule another process")
	}
	got := updated.(App)
	if got.login.message != i18n.T("login.codex_pool_standalone_returned") || !got.login.messageOK {
		t.Fatal("outer App did not forward standalone completion to active login view")
	}
}
