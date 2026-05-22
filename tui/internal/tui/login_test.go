package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
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
