package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// seedLoginCodexAuth writes a stub codex-auth.json under dir and returns
// its path. Mirrors firstrun_test.go's writeCodexAuth — duplicated as a
// local helper because login_test.go is otherwise independent.
func seedLoginCodexAuth(t *testing.T, dir string) string {
	t.Helper()
	tok := CodexTokens{
		AccessToken:  "stub-access",
		RefreshToken: "stub-refresh",
		ExpiresAt:    9999999999,
		Email:        "stub@example.com",
	}
	data, _ := json.Marshal(tok)
	p := filepath.Join(dir, "codex-auth.json")
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatalf("write codex-auth.json: %v", err)
	}
	return p
}

// TestLoginModel_DelTwoPressDeletesCodex verifies the two-press Del
// confirmation in /login: first press arms, second press removes
// codex-auth.json AND drops the entry from the in-memory slice.
func TestLoginModel_DelTwoPressDeletesCodex(t *testing.T) {
	dir := t.TempDir()
	authPath := seedLoginCodexAuth(t, dir)

	m := NewLoginModel("", dir)
	if len(m.entries) != 1 || m.entries[0].Provider != "codex" {
		t.Fatalf("expected single codex entry; got %#v", m.entries)
	}

	// First Del — should arm only.
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDelete})
	if m.deleteArmedIdx != 0 {
		t.Errorf("first Del should arm deleteArmedIdx=0; got %d", m.deleteArmedIdx)
	}
	if _, err := os.Stat(authPath); err != nil {
		t.Errorf("first Del must not delete the file: %v", err)
	}
	if len(m.entries) != 1 {
		t.Errorf("first Del must not drop the entry; len=%d", len(m.entries))
	}

	// Second Del — actually deletes.
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDelete})
	if m.deleteArmedIdx != -1 {
		t.Errorf("deleteArmedIdx should reset to -1 after delete; got %d", m.deleteArmedIdx)
	}
	if _, err := os.Stat(authPath); !os.IsNotExist(err) {
		t.Errorf("codex-auth.json should be removed; stat err: %v", err)
	}
	if len(m.entries) != 0 {
		t.Errorf("entry should be dropped; entries=%#v", m.entries)
	}
}

// TestLoginModel_RTwoPressDeletesCodex verifies the legacy [r] logout
// shortcut follows the same two-press confirmation path as Del/Backspace.
func TestLoginModel_RTwoPressDeletesCodex(t *testing.T) {
	dir := t.TempDir()
	authPath := seedLoginCodexAuth(t, dir)

	m := NewLoginModel("", dir)
	if len(m.entries) != 1 || m.entries[0].Provider != "codex" {
		t.Fatalf("expected single codex entry; got %#v", m.entries)
	}

	r := tea.KeyPressMsg{Text: "r", Code: 'r'}
	m, _ = m.Update(r)
	if m.deleteArmedIdx != 0 {
		t.Errorf("first r should arm deleteArmedIdx=0; got %d", m.deleteArmedIdx)
	}
	if _, err := os.Stat(authPath); err != nil {
		t.Errorf("first r must not delete the file: %v", err)
	}

	m, _ = m.Update(r)
	if m.deleteArmedIdx != -1 {
		t.Errorf("deleteArmedIdx should reset to -1 after delete; got %d", m.deleteArmedIdx)
	}
	if _, err := os.Stat(authPath); !os.IsNotExist(err) {
		t.Errorf("codex-auth.json should be removed by r shortcut; stat err: %v", err)
	}
	if len(m.entries) != 0 {
		t.Errorf("entry should be dropped; entries=%#v", m.entries)
	}
}

// TestLoginModel_DelDisarmedByMovement: pressing arrow keys after a
// first Del must disarm the confirmation so a later Del on a different
// row doesn't accidentally fire the previous arm.
func TestLoginModel_DelDisarmedByMovement(t *testing.T) {
	dir := t.TempDir()
	authPath := seedLoginCodexAuth(t, dir)

	m := NewLoginModel("", dir)
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDelete})
	if m.deleteArmedIdx != 0 {
		t.Fatalf("expected arm; got %d", m.deleteArmedIdx)
	}
	// "down" — should disarm.
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.deleteArmedIdx != -1 {
		t.Error("Down should clear the delete arm")
	}
	if _, err := os.Stat(authPath); err != nil {
		t.Errorf("credential must survive a disarm: %v", err)
	}
}

// TestLoginModel_LateOAuthDoneIgnoredAfterCancel mirrors the firstrun
// test: a CodexOAuthDoneMsg with a stale epoch must NOT write
// codex-auth.json from /login either.
func TestLoginModel_LateOAuthDoneIgnoredAfterCancel(t *testing.T) {
	dir := t.TempDir()
	m := NewLoginModel("", dir)
	m.codexLoginEpoch = 3

	stale := CodexOAuthDoneMsg{
		Epoch: 1,
		Tokens: &CodexTokens{
			AccessToken:  "leaked",
			RefreshToken: "leaked-refresh",
		},
	}
	m, _ = m.Update(stale)
	if _, err := os.Stat(filepath.Join(dir, "codex-auth.json")); !os.IsNotExist(err) {
		t.Errorf("stale OAuth callback must not write codex-auth.json; stat err: %v", err)
	}
}

func TestLoginModel_CodexEnterShowsMethodChooser(t *testing.T) {
	dir := t.TempDir()
	seedLoginCodexAuth(t, dir)

	m := NewLoginModel("", dir)
	if len(m.entries) != 1 || !m.entries[0].IsOAuth {
		t.Fatalf("expected single codex OAuth entry; got %#v", m.entries)
	}

	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("opening the method chooser must not start a network command")
	}
	if !m.codexChoosingMethod {
		t.Fatal("Enter on Codex OAuth entry should show method chooser")
	}
	if m.codexLogging {
		t.Fatal("method chooser should not start login yet")
	}
	if m.codexMethodCursor != 0 {
		t.Fatalf("default method cursor = %d, want browser OAuth (0)", m.codexMethodCursor)
	}

	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.codexMethodCursor != 1 {
		t.Fatalf("Down should select device code; cursor=%d", m.codexMethodCursor)
	}
	view := m.View()
	if !strings.Contains(view, "Device code") || !strings.Contains(view, "remote") {
		t.Fatalf("chooser view should mention remote-friendly device code; view=%s", view)
	}
}
