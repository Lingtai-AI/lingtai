package tui

import (
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/internal/config"
)

func TestRecoveryEscReturnsToInitializedMail(t *testing.T) {
	projectDir := t.TempDir()
	globalDir := t.TempDir()
	a := NewApp(globalDir, projectDir, false, true, false, []string{"manager"}, config.DefaultTUIConfig(), "", "")
	if !a.recoveryMode || a.currentView != appViewFirstRun {
		t.Fatal("expected recovery setup view")
	}

	model, _ := a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	a = model.(App)
	model, escCmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	a = model.(App)
	change, ok := runCmd(escCmd).(ViewChangeMsg)
	if !ok || change.View != "mail" {
		t.Fatalf("recovery Esc returned %#v, want mail view change", change)
	}
	model, _ = a.Update(change)
	a = model.(App)
	if a.currentView != appViewMail {
		t.Fatalf("current view = %v, want mail", a.currentView)
	}
	// The queued child resize was the observed panic: it must reach a fully
	// constructed Mail input even when recovery was cancelled.
	model, _ = a.Update(childWindowSizeMsg{WindowSizeMsg: tea.WindowSizeMsg{Width: 80, Height: 24}})
	a = model.(App)
	if a.mail.humanDir != filepath.Join(projectDir, "human") {
		t.Fatalf("mail human dir = %q", a.mail.humanDir)
	}
	_ = a.View()
}
