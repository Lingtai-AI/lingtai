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

// TestLoginModel_EnterWithNoCredentialsOpensMethodChooser verifies that
// when the credentials page has no saved credentials, pressing Enter opens
// the Codex browser/device-code chooser so the user can add their first
// credential without leaving the page.
func TestLoginModel_EnterWithNoCredentialsOpensMethodChooser(t *testing.T) {
	dir := t.TempDir()
	m := NewLoginModel("", dir)
	if len(m.entries) != 0 {
		t.Fatalf("precondition: expected empty entries; got %#v", m.entries)
	}

	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("opening method chooser must not start a network command")
	}
	if !m.codexChoosingMethod {
		t.Fatal("Enter with no credentials should open the Codex method chooser")
	}
	if m.codexLogging {
		t.Fatal("method chooser should not start login yet")
	}
	if m.codexMethodCursor != 0 {
		t.Fatalf("default method cursor = %d, want browser OAuth (0)", m.codexMethodCursor)
	}

	// View should contain the method chooser options.
	m.width = 80
	view := m.View()
	if !strings.Contains(view, "Browser") {
		t.Fatalf("chooser view should mention browser method; view=%q", view)
	}
}

// TestLoginModel_EmptyViewShowsAddRow verifies the view shows an affordance
// for adding a Codex credential when no credentials are saved yet.
func TestLoginModel_EmptyViewShowsAddRow(t *testing.T) {
	dir := t.TempDir()
	m := NewLoginModel("", dir)
	m.width = 80
	view := m.View()
	if !strings.Contains(view, "Add Codex OAuth") {
		t.Fatalf("empty credentials view should show 'Add Codex OAuth' row; view=%q", view)
	}
	if !strings.Contains(view, "login with Codex") {
		t.Fatalf("empty credentials footer should mention 'login with Codex'; view=%q", view)
	}
}

// seedNonCodexAPIKey writes a non-Codex API key (zhipu) into the config so
// NewLoginModel picks it up without a codex-auth.json present.
func seedNonCodexAPIKey(t *testing.T, dir string) {
	t.Helper()
	cfg := `{"keys":{"zhipu":"sk-stub-zhipu-key"}}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(cfg), 0o600); err != nil {
		t.Fatalf("write config.json: %v", err)
	}
}

// TestLoginModel_NonCodexOnlyShowsAddCodexRow verifies that when only a
// non-Codex credential exists (e.g. zhipu) and no codex-auth.json is present,
// the view still renders the "Add Codex OAuth" virtual row.
func TestLoginModel_NonCodexOnlyShowsAddCodexRow(t *testing.T) {
	dir := t.TempDir()
	seedNonCodexAPIKey(t, dir)

	m := NewLoginModel("", dir)
	if len(m.entries) != 1 || m.entries[0].Provider == "codex" {
		t.Fatalf("precondition: expected one non-Codex entry; got %#v", m.entries)
	}

	m.width = 80
	view := m.View()
	if !strings.Contains(view, "Add Codex OAuth") {
		t.Fatalf("non-Codex-only credentials view should show 'Add Codex OAuth' row; view=%q", view)
	}
}

// TestLoginModel_NonCodexOnlyEnterOnVirtualRowOpensChooser verifies that
// navigating down onto the virtual "Add Codex OAuth" row and pressing Enter
// opens the method chooser without starting any network operation.
func TestLoginModel_NonCodexOnlyEnterOnVirtualRowOpensChooser(t *testing.T) {
	dir := t.TempDir()
	seedNonCodexAPIKey(t, dir)

	m := NewLoginModel("", dir)
	if len(m.entries) != 1 {
		t.Fatalf("precondition: expected one entry; got %d", len(m.entries))
	}
	// cursor starts at 0 (the zhipu entry). Navigate down to virtual row.
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.cursor != 1 {
		t.Fatalf("Down should move cursor to virtual row (1); got %d", m.cursor)
	}
	// Press Enter on virtual row.
	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("Enter on virtual Add Codex row must not start a network command")
	}
	if !m.codexChoosingMethod {
		t.Fatal("Enter on virtual Add Codex row should open the method chooser")
	}
	if m.codexLogging {
		t.Fatal("method chooser must not start login yet")
	}
}

// TestLoginModel_NonCodexOnlyCursorBoundedByVirtualRow verifies that cursor
// navigation does not go past the virtual "Add Codex OAuth" row.
func TestLoginModel_NonCodexOnlyCursorBoundedByVirtualRow(t *testing.T) {
	dir := t.TempDir()
	seedNonCodexAPIKey(t, dir)

	m := NewLoginModel("", dir)
	// Press down twice — should stop at index 1 (virtual row).
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.cursor != 1 {
		t.Fatalf("cursor should be clamped at virtual row index 1; got %d", m.cursor)
	}
}

func TestSetupCredentialsModelUsesSetupChrome(t *testing.T) {
	dir := t.TempDir()
	m := NewSetupCredentialsModel("", dir)
	m.width = 100

	if !m.setupSubpage {
		t.Fatal("setup credentials constructor should mark LoginModel as a setup subpage")
	}
	view := m.View()
	if !strings.Contains(view, "Setup") || !strings.Contains(view, "Credentials") {
		t.Fatalf("setup credentials view should be titled as setup credentials; view=%s", view)
	}
	if !strings.Contains(view, "/setup") || !strings.Contains(view, "/login") {
		t.Fatalf("setup credentials note should explain /setup ownership and /login shortcut; view=%s", view)
	}
}
