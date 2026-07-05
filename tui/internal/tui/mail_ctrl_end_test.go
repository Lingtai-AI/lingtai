package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func ctrlEndKey(t *testing.T) tea.KeyPressMsg {
	t.Helper()
	k := tea.KeyPressMsg{Code: tea.KeyEnd, Mod: tea.ModCtrl}
	if got := k.String(); got != "ctrl+end" {
		t.Fatalf("ctrl+end keypress String() = %q, want %q", got, "ctrl+end")
	}
	if key := k.Key(); key.Code != tea.KeyEnd || key.Mod != tea.ModCtrl {
		t.Fatalf("ctrl+end keypress Key() = {Code:%v Mod:%v}, want {Code:%v Mod:%v}", key.Code, key.Mod, tea.KeyEnd, tea.ModCtrl)
	}
	return k
}

func scrollableMailModel(t *testing.T) MailModel {
	t.Helper()
	m := newSizedMailModel(t)
	content := strings.TrimSuffix(strings.Repeat("message line\n", m.viewport.Height()+30), "\n")
	m.viewport.SetContent(content)
	m.viewport.GotoBottom()
	if !m.viewport.AtBottom() || m.viewport.YOffset() == 0 {
		t.Fatalf("precondition: viewport should have scrollable content at bottom, offset=%d height=%d", m.viewport.YOffset(), m.viewport.Height())
	}
	m.viewport.GotoTop()
	if m.viewport.AtBottom() {
		t.Fatalf("precondition: viewport should be away from bottom")
	}
	return m
}

func TestMailCtrlEndKeyRepresentation(t *testing.T) {
	ctrlEndKey(t)
}

func TestMailCtrlEndJumpsViewportToBottom(t *testing.T) {
	m := scrollableMailModel(t)
	m.loadedExtra = m.pageSize
	before := m.viewport.YOffset()

	updated, cmd := m.Update(ctrlEndKey(t))
	if cmd != nil {
		t.Fatalf("ctrl+end should not return a command")
	}
	if !updated.viewport.AtBottom() {
		t.Fatalf("ctrl+end should jump viewport to bottom, before offset=%d after offset=%d", before, updated.viewport.YOffset())
	}
	if updated.viewport.YOffset() <= before {
		t.Fatalf("ctrl+end should increase viewport offset, before=%d after=%d", before, updated.viewport.YOffset())
	}
	if updated.loadedExtra != m.loadedExtra {
		t.Fatalf("ctrl+end should not collapse loaded history, loadedExtra=%d want %d", updated.loadedExtra, m.loadedExtra)
	}
}

func TestMailCtrlEndOverlayPriority(t *testing.T) {
	t.Run("editor warning", func(t *testing.T) {
		m := scrollableMailModel(t)
		m.showEditorWarn = true

		updated, cmd := m.Update(ctrlEndKey(t))
		if cmd != nil {
			t.Fatalf("ctrl+end under editor warning should not return a command")
		}
		if updated.viewport.AtBottom() {
			t.Fatalf("ctrl+end should not move viewport while editor warning is active")
		}
		if !updated.showEditorWarn {
			t.Fatalf("ctrl+end should leave editor warning active")
		}
	})

	t.Run("slash palette", func(t *testing.T) {
		m := scrollableMailModel(t)
		m.input.SetValue("/help")
		if !m.input.IsPaletteActive() {
			t.Fatalf("precondition: slash palette should be active")
		}

		updated, _ := m.Update(ctrlEndKey(t))
		if updated.viewport.AtBottom() {
			t.Fatalf("ctrl+end should not move viewport while slash palette is active")
		}
		if !updated.input.IsPaletteActive() {
			t.Fatalf("ctrl+end should leave slash palette active")
		}
	})
}
