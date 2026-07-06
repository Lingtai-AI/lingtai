package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// ctrlOKey constructs the ctrl+o keypress. We assert its String() is "ctrl+o"
// so the key code used here can't silently desync from the handler's switch.
func ctrlOKey(t *testing.T) tea.KeyPressMsg {
	t.Helper()
	k := tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl}
	if k.String() != "ctrl+o" {
		t.Fatalf("ctrl+o keypress String() = %q, want %q", k.String(), "ctrl+o")
	}
	return k
}

// TestCtrlOAnchorToBottom pins the PR #469 fix: pressing ctrl+o on a ready
// MailModel cycles the verbose level, re-renders, and anchors the viewport to
// the bottom (so the latest output is visible) — even when the viewport started
// scrolled away from the bottom. It also covers the not-ready fallback: ctrl+o
// on a model without a sized viewport must still return the refresh command.
func TestCtrlOAnchorToBottom(t *testing.T) {
	t.Run("ready anchors to bottom and returns nil", func(t *testing.T) {
		m := newSizedMailModel(t)
		if !m.ready {
			t.Fatalf("precondition: model should be ready after WindowSizeMsg")
		}

		// Load enough messages that the rendered stream is taller than the
		// viewport, then scroll to the top so the viewport starts AWAY from the
		// bottom. Without the anchor fix, ctrl+o's re-render leaves the viewport
		// at the top, so AtBottom() would be false.
		var msgs []ChatMessage
		for i := 0; i < 200; i++ {
			msgs = append(msgs, ChatMessage{
				Type:     "mail",
				From:     "human",
				To:       "codex",
				Body:     strings.Repeat("line ", 20),
				IsFromMe: true,
			})
		}
		m.messages = msgs
		m.viewport.SetContent(m.renderMessages(m.visibleMessages()))
		m.viewport.GotoTop()
		if m.viewport.AtBottom() {
			t.Fatalf("precondition: viewport should start away from bottom (content must exceed viewport height)")
		}

		if m.verbose != verboseOff {
			t.Fatalf("precondition: verbose should start at verboseOff, got %v", m.verbose)
		}

		updated, cmd := m.Update(ctrlOKey(t))
		if cmd != nil {
			t.Fatalf("ready ctrl+o should return nil cmd, got %v", cmd)
		}
		if updated.verbose != verboseThinking {
			t.Fatalf("ctrl+o should cycle verboseOff -> verboseThinking, got %v", updated.verbose)
		}
		if !updated.viewport.AtBottom() {
			t.Fatalf("ready ctrl+o must anchor the viewport to the bottom")
		}
	})

	t.Run("verbose cycles off -> thinking -> extended -> off", func(t *testing.T) {
		m := newSizedMailModel(t)

		updated, _ := m.Update(ctrlOKey(t))
		if updated.verbose != verboseThinking {
			t.Fatalf("first ctrl+o: got %v, want verboseThinking", updated.verbose)
		}
		updated, _ = updated.Update(ctrlOKey(t))
		if updated.verbose != verboseExtended {
			t.Fatalf("second ctrl+o: got %v, want verboseExtended", updated.verbose)
		}
		updated, _ = updated.Update(ctrlOKey(t))
		if updated.verbose != verboseOff {
			t.Fatalf("third ctrl+o: got %v, want verboseOff (cycle wraps)", updated.verbose)
		}
	})

	t.Run("not ready returns refresh command", func(t *testing.T) {
		// No WindowSizeMsg, so the viewport is never sized and ready stays false.
		m := NewMailModel("", "", "", "", "codex", 10, "", "en", false, 0)
		if m.ready {
			t.Fatalf("precondition: model should not be ready before WindowSizeMsg")
		}

		updated, cmd := m.Update(ctrlOKey(t))
		if cmd == nil {
			t.Fatalf("not-ready ctrl+o must return the refreshMail fallback command")
		}
		if updated.verbose != verboseThinking {
			t.Fatalf("not-ready ctrl+o should still cycle verbose to verboseThinking, got %v", updated.verbose)
		}
	})
}
