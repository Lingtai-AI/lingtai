package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/i18n"
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

func setMailViewportLineCount(t *testing.T, m *MailModel, lineCount int) {
	t.Helper()
	if lineCount < 1 {
		lineCount = 1
	}
	lines := make([]string, lineCount)
	for i := range lines {
		lines[i] = "message line"
	}
	m.viewport.SetContent(strings.Join(lines, "\n"))
	if got := m.viewport.TotalLineCount(); got != lineCount {
		t.Fatalf("viewport line count = %d, want %d", got, lineCount)
	}
}

func mailViewportBottomOffset(m MailModel) int {
	bottom := m.viewport.TotalLineCount() - m.viewport.Height()
	if bottom < 0 {
		return 0
	}
	return bottom
}

func hasChatTailHint(m MailModel) bool {
	return strings.Contains(m.View(), i18n.T("mail.jump_bottom_hint"))
}

func TestMailCtrlEndKeyRepresentation(t *testing.T) {
	ctrlEndKey(t)
}

func TestMailCtrlEndJumpsViewportToBottom(t *testing.T) {
	m := scrollableMailModel(t)
	m.loadedExtra = m.pageSize
	m.input.SetValue("draft")
	if !m.input.Focused() {
		t.Fatalf("precondition: compose textarea should be focused")
	}
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
	if !updated.input.Focused() || updated.input.Value() != "draft" {
		t.Fatalf("ctrl+end should leave compose focus/value unchanged, focused=%v value=%q", updated.input.Focused(), updated.input.Value())
	}
}

func TestMailCtrlEndNotReadyReturnsNoop(t *testing.T) {
	m := NewMailModel("", "", "", "", "codex", 10, "", "en", false, 0)
	m.input.SetValue("draft")

	updated, cmd := m.Update(ctrlEndKey(t))
	if cmd != nil {
		t.Fatalf("ctrl+end before ready should not return a command")
	}
	if updated.ready {
		t.Fatalf("ctrl+end before ready should not mark mail ready")
	}
	if updated.input.Value() != "draft" {
		t.Fatalf("ctrl+end before ready should not alter input value: %q", updated.input.Value())
	}
}

func TestMailChatTailHintVisibility(t *testing.T) {
	t.Run("hidden at bottom", func(t *testing.T) {
		m := newSizedMailModel(t)
		m.initialLoading = false
		setMailViewportLineCount(t, &m, m.viewport.Height()*3)
		m.viewport.GotoBottom()

		if hasChatTailHint(m) {
			t.Fatalf("chat-tail hint should be hidden at bottom")
		}
	})

	t.Run("hidden one page from bottom", func(t *testing.T) {
		m := newSizedMailModel(t)
		m.initialLoading = false
		setMailViewportLineCount(t, &m, m.viewport.Height()*3)
		m.viewport.SetYOffset(mailViewportBottomOffset(m) - m.viewport.Height())
		if got, want := m.chatTailRemainingLines(), m.viewport.Height(); got != want {
			t.Fatalf("remaining lines = %d, want exactly one page (%d)", got, want)
		}

		if hasChatTailHint(m) {
			t.Fatalf("chat-tail hint should be hidden at one page from bottom")
		}
	})

	t.Run("visible more than one page from bottom", func(t *testing.T) {
		m := newSizedMailModel(t)
		m.initialLoading = false
		setMailViewportLineCount(t, &m, m.viewport.Height()*3)
		m.viewport.SetYOffset(mailViewportBottomOffset(m) - m.viewport.Height() - 1)
		if got, want := m.chatTailRemainingLines(), m.viewport.Height()+1; got != want {
			t.Fatalf("remaining lines = %d, want just over one page (%d)", got, want)
		}

		if !hasChatTailHint(m) {
			t.Fatalf("chat-tail hint should be visible when more than one page from bottom")
		}
	})
}

func TestMailCtrlEndHidesChatTailHint(t *testing.T) {
	m := newSizedMailModel(t)
	m.initialLoading = false
	setMailViewportLineCount(t, &m, m.viewport.Height()*3)
	m.viewport.SetYOffset(mailViewportBottomOffset(m) - m.viewport.Height() - 1)
	if !hasChatTailHint(m) {
		t.Fatalf("precondition: chat-tail hint should be visible before ctrl+end")
	}

	updated, cmd := m.Update(ctrlEndKey(t))
	if cmd != nil {
		t.Fatalf("ctrl+end should not return a command")
	}
	if !updated.viewport.AtBottom() {
		t.Fatalf("ctrl+end should jump to bottom")
	}
	if hasChatTailHint(updated) {
		t.Fatalf("chat-tail hint should hide after ctrl+end jumps to bottom")
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
