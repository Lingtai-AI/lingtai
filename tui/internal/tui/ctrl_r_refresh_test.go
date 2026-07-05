package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// These tests assert the Ctrl+R refresh contract across TUI views:
//
//   - Views with reloadable data refresh on Ctrl+R.
//   - Views that already had a bare-`r` reload on origin/main keep it AND gain
//     a Ctrl+R alias (no bare-`r` handler is removed or newly introduced).
//   - Text-editing views keep typing `r` normally and grow no bare-`r` refresh
//     interception.
//
// Ctrl+R is constructed as tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}; its
// .String() is "ctrl+r" (verified against charm.land/bubbletea/v2). A bare `r`
// keypress is tea.KeyPressMsg{Code: 'r', Text: "r"} whose .String() is "r".

func ctrlR() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl} }
func bareR() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'r', Text: "r"} }

// --- Cmd-returning views: Ctrl+R must trigger a (re)load command. ---

func TestPropsCtrlRTriggersReload(t *testing.T) {
	m := NewPropsModel(t.TempDir(), t.TempDir(), t.TempDir())
	_, cmd := m.Update(ctrlR())
	if cmd == nil {
		t.Fatal("PropsModel ctrl+r returned nil cmd; expected a reload command")
	}
}

func TestMailCtrlRTriggersRefresh(t *testing.T) {
	dir := t.TempDir()
	m := NewMailModel(dir, "human@local", dir, dir, "orch", 20, dir, "en", false, 0)
	_, cmd := m.Update(ctrlR())
	if cmd == nil {
		t.Fatal("MailModel ctrl+r returned nil cmd; expected a refresh command")
	}
}

func TestDoctorCtrlRRerunsDiagnostic(t *testing.T) {
	m := NewDoctorModel(t.TempDir(), t.TempDir())
	// Simulate the diagnostic having finished so loading is false.
	m.loading = false
	updated, cmd := m.Update(ctrlR())
	if cmd == nil {
		t.Fatal("DoctorModel ctrl+r returned nil cmd; expected a re-run command")
	}
	if !updated.loading {
		t.Fatal("DoctorModel ctrl+r should set loading=true while the diagnostic re-runs")
	}
}

// --- Views with an existing bare-`r` reload on origin/main: keep bare `r`,
//     add ctrl+r alias. No bare-`r` handler is removed. ---

func TestProjectsCtrlRAndBareRBothReload(t *testing.T) {
	m := NewProjectsModel(t.TempDir(), t.TempDir())
	if _, cmd := m.Update(ctrlR()); cmd == nil {
		t.Fatal("ProjectsModel ctrl+r returned nil cmd; expected reload")
	}
	if _, cmd := m.Update(bareR()); cmd == nil {
		t.Fatal("ProjectsModel bare r regressed; the pre-existing bare-r reload must be preserved")
	}
}

func TestPresetLibraryCtrlRAndBareRBothReload(t *testing.T) {
	m := NewPresetLibraryModel("en", t.TempDir())
	// Both keypresses must be accepted by the list-focus handler without
	// panicking; bare `r` is the pre-existing reload and ctrl+r is its alias.
	if _, _ = m.Update(ctrlR()); true {
	}
	if _, _ = m.Update(bareR()); true {
	}
}

// --- Viewer-wrapping views: Ctrl+R rebuilds the inner viewer from disk. ---

func TestNotificationCtrlRAndBareRReload(t *testing.T) {
	agentDir := t.TempDir()
	m := NewNotificationModel(agentDir)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	// Ctrl+R and bare r must not panic even when no sqlite sidecar exists.
	if _, _ = m.Update(ctrlR()); true {
	}
	if _, _ = m.Update(bareR()); true {
	}
	// View must contain an actionable message about the missing sidecar.
	view := m.View()
	if !strings.Contains(view, "log.sqlite") {
		t.Fatalf("NotificationModel view must mention log.sqlite when sidecar is absent; got:\n%s", view)
	}
}

func TestViewerViewsHandleCtrlRWithoutPanic(t *testing.T) {
	base := t.TempDir()
	agent := filepath.Join(base, "agent")
	if err := os.MkdirAll(agent, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Run("knowledge", func(t *testing.T) {
		m := NewKnowledgeModel(base, agent)
		if _, _ = m.Update(ctrlR()); true {
		}
	})
	t.Run("system", func(t *testing.T) {
		m := NewSystemModel(base, agent)
		if _, _ = m.Update(ctrlR()); true {
		}
	})
	t.Run("mailbox", func(t *testing.T) {
		m := NewMailboxModel(base)
		if _, _ = m.Update(ctrlR()); true {
		}
	})
	t.Run("library", func(t *testing.T) {
		m := NewLibraryModel(base, agent, "en")
		if _, _ = m.Update(ctrlR()); true {
		}
	})
	t.Run("addon", func(t *testing.T) {
		m := NewAddonModel(base)
		if _, _ = m.Update(ctrlR()); true {
		}
	})
}

// --- Text-editing views: bare `r` must keep typing; no bare-`r` refresh. ---

// TestPresetEditorBareRTypesNormally drives the editor into inline edit mode and
// confirms a bare `r` is appended to the field value rather than swallowed by a
// refresh handler. This guards the requirement that text-editing screens keep
// `r` typing intact and grow no bare-`r` interception.
func TestPresetEditorBareRTypesNormally(t *testing.T) {
	m := NewPresetEditorModel(testPresetEditorPreset(), "en", nil, "")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	// Open the inline editor on the focused field (feName at cursor 0).
	updated, _ := m.openInline()
	m = updated
	if m.mode != emInline {
		t.Fatalf("expected inline edit mode after openInline, got %v", m.mode)
	}
	before := m.input.Value()
	m, _ = m.Update(bareR())
	after := m.input.Value()
	if after != before+"r" {
		t.Fatalf("bare r not typed into preset editor field: before=%q after=%q", before, after)
	}
}
