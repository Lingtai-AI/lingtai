package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/i18n"
)

// writeCodexTokenAt writes a valid stub token bundle to an absolute path so a
// Codex account row reports as valid. Fake tokens only — never real secrets.
func writeCodexTokenAt(t *testing.T, path, email string) {
	t.Helper()
	tok := CodexTokens{
		AccessToken:  "stub-access",
		RefreshToken: "stub-refresh",
		ExpiresAt:    9999999999,
		Email:        email,
	}
	data, _ := json.Marshal(tok)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir for token: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write token %s: %v", path, err)
	}
}

// TestLoginModel_EnterOnCodexSetsActiveAppliesToSavedPresets is the core new
// behavior: Enter on a valid Codex OAuth row applies that account to all saved
// Codex presets (rewriting codex_auth_path), reports the count, and does NOT
// open the re-auth chooser or start any network command.
func TestLoginModel_EnterOnCodexSetsActiveAppliesToSavedPresets(t *testing.T) {
	_, globalDir := withTempCodexHome(t)

	// Two saved codex presets, initially legacy-bound; one non-codex preset.
	saveCodexPresetForTest(t, "codex-a", "")
	saveCodexPresetForTest(t, "codex-b", "")
	saveNonCodexPresetForTest(t, "zhipu-1")

	// A per-account Codex credential to select as active.
	acctPath := filepath.Join(globalDir, codexAuthSubdir, "work.json")
	writeCodexTokenAt(t, acctPath, "work@example.com")

	m := NewLoginModel("", globalDir)
	// Locate the per-account (non-legacy) codex entry and put the cursor on it.
	idx := -1
	for i := range m.entries {
		if m.entries[i].Provider == "codex" && m.entries[i].CodexPath == acctPath {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("could not find the per-account codex entry; entries=%#v", m.entries)
	}
	m.cursor = idx
	// Mark it valid (NewLoginModel sets loginChecking until health resolves; the
	// apply gate must accept an account whose token file parses).
	m.entries[idx].Status = loginValid

	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("Enter to set active must not start a network command")
	}
	if m.codexChoosingMethod {
		t.Fatal("Enter on an existing Codex row must NOT open the re-auth chooser anymore")
	}
	if m.codexLogging {
		t.Fatal("Enter to set active must not start a login")
	}

	wantRef := codexAuthRefForPath(globalDir, acctPath)
	for _, name := range []string{"codex-a", "codex-b"} {
		ref, ok := reloadSavedRef(t, name)
		if !ok || ref != wantRef {
			t.Errorf("preset %q ref = %q (present=%v), want %q", name, ref, ok, wantRef)
		}
	}
	// Status message should report the applied count (2) — and must not be the
	// generic re-auth/cancel string.
	if m.message == "" {
		t.Fatal("setting active should surface a status message")
	}
	if !strings.Contains(m.message, "2") {
		t.Errorf("status message should mention the 2 updated presets; got %q", m.message)
	}
}

func TestLoginModel_EnterOnCodexAllowsTransientHealthErrorWithValidTokenFile(t *testing.T) {
	_, globalDir := withTempCodexHome(t)
	saveCodexPresetForTest(t, "codex-a", "")

	acctPath := filepath.Join(globalDir, codexAuthSubdir, "work.json")
	writeCodexTokenAt(t, acctPath, "work@example.com")

	m := NewLoginModel("", globalDir)
	idx := -1
	for i := range m.entries {
		if m.entries[i].Provider == "codex" && m.entries[i].CodexPath == acctPath {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("could not find the per-account codex entry; entries=%#v", m.entries)
	}
	m.cursor = idx
	m.entries[idx].Status = loginError
	m.entries[idx].Detail = "refresh unavailable"

	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("Enter to set active after a transient health error must not start a network command")
	}
	wantRef := codexAuthRefForPath(globalDir, acctPath)
	if ref, ok := reloadSavedRef(t, "codex-a"); !ok || ref != wantRef {
		t.Fatalf("transient health error with valid refresh-token file should still apply active account; ref=%q present=%v want=%q", ref, ok, wantRef)
	}
	if !m.messageOK {
		t.Fatalf("setting active after transient health error should be confirmation feedback; message=%q", m.message)
	}
}

// TestLoginModel_EnterOnLegacyResetsSavedPresets verifies Enter on the legacy
// account row resets saved Codex presets back to the legacy fallback (no
// codex_auth_path key).
func TestLoginModel_EnterOnLegacyResetsSavedPresets(t *testing.T) {
	_, globalDir := withTempCodexHome(t)

	saveCodexPresetForTest(t, "codex-a", "~/.lingtai-tui/codex-auth/work.json")

	// Seed both the legacy account and the per-account file so both rows exist.
	writeCodexTokenAt(t, legacyCodexAuthPath(globalDir), "primary@example.com")
	writeCodexTokenAt(t, filepath.Join(globalDir, codexAuthSubdir, "work.json"), "work@example.com")

	m := NewLoginModel("", globalDir)
	idx := -1
	for i := range m.entries {
		if m.entries[i].Provider == "codex" && m.entries[i].CodexLegacy {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("could not find the legacy codex entry; entries=%#v", m.entries)
	}
	m.cursor = idx
	m.entries[idx].Status = loginValid

	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if ref, ok := reloadSavedRef(t, "codex-a"); ok {
		t.Errorf("legacy selection should remove codex_auth_path; got %q (present=%v)", ref, ok)
	}
}

// TestLoginModel_EnterNoSavedPresetsReportsGracefully verifies Enter on a valid
// Codex row with zero saved Codex presets reports the no-presets message and
// does not error or open the chooser.
func TestLoginModel_EnterNoSavedPresetsReportsGracefully(t *testing.T) {
	_, globalDir := withTempCodexHome(t)
	// No saved codex presets at all.

	acctPath := filepath.Join(globalDir, codexAuthSubdir, "work.json")
	writeCodexTokenAt(t, acctPath, "work@example.com")

	m := NewLoginModel("", globalDir)
	idx := -1
	for i := range m.entries {
		if m.entries[i].Provider == "codex" && m.entries[i].CodexPath == acctPath {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("could not find the codex entry; entries=%#v", m.entries)
	}
	m.cursor = idx
	m.entries[idx].Status = loginValid

	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("Enter with no saved presets must not start a command")
	}
	if m.codexChoosingMethod {
		t.Fatal("Enter must not open the chooser")
	}
	want := i18n.T("login.codex_no_saved_presets")
	if m.message != want {
		t.Fatalf("status = %q, want no-saved-presets message %q", m.message, want)
	}
}

// TestLoginModel_EnterOnInvalidCodexDoesNotApply verifies that pressing Enter on
// an INVALID Codex row (broken/unparseable token file) does not rewrite any
// saved preset — guarding against pointing every preset at a dead account.
func TestLoginModel_EnterOnInvalidCodexDoesNotApply(t *testing.T) {
	_, globalDir := withTempCodexHome(t)

	saveCodexPresetForTest(t, "codex-a", "")

	// A malformed token file: present (so it lists) but invalid (no refresh).
	acctPath := filepath.Join(globalDir, codexAuthSubdir, "broken.json")
	if err := os.MkdirAll(filepath.Dir(acctPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(acctPath, []byte(`{"access_token":"x"}`), 0o600); err != nil {
		t.Fatalf("write broken token: %v", err)
	}

	m := NewLoginModel("", globalDir)
	idx := -1
	for i := range m.entries {
		if m.entries[i].Provider == "codex" && m.entries[i].CodexPath == acctPath {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("could not find the broken codex entry; entries=%#v", m.entries)
	}
	m.cursor = idx
	m.entries[idx].Status = loginInvalid

	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	// The saved preset must remain legacy-bound (no codex_auth_path).
	if ref, ok := reloadSavedRef(t, "codex-a"); ok {
		t.Errorf("invalid account must not be applied; preset gained ref %q", ref)
	}
	if m.message == "" {
		t.Error("selecting an invalid account should surface a hint")
	}
}

// TestLoginModel_RReauthsExistingAccount verifies the relocated re-auth: pressing
// r on a Codex row opens the method chooser targeting THAT account's own token
// file (the behavior Enter used to have).
func TestLoginModel_RReauthsExistingAccount(t *testing.T) {
	dir := t.TempDir()
	authPath := seedLoginCodexAuth(t, dir)

	m := NewLoginModel("", dir)
	if len(m.entries) != 1 || m.entries[0].Provider != "codex" {
		t.Fatalf("precondition: expected single codex entry; got %#v", m.entries)
	}

	r := tea.KeyPressMsg{Text: "r", Code: 'r'}
	m, cmd := m.Update(r)
	if cmd != nil {
		t.Fatal("r re-auth must not start a network command (opens chooser first)")
	}
	if !m.codexChoosingMethod {
		t.Fatal("r on a codex entry should open the method chooser")
	}
	if m.codexLoginTargetPath != authPath {
		t.Fatalf("r re-auth should target the account's own file %q; got %q", authPath, m.codexLoginTargetPath)
	}
	// r must NOT have deleted the file.
	if _, err := os.Stat(authPath); err != nil {
		t.Errorf("r must not delete the credential anymore: %v", err)
	}
}

// TestLoginModel_DTwoPressDeletesCodex verifies the relocated logout: d follows
// the same two-press confirmation that Del/Backspace use, and r no longer
// deletes.
func TestLoginModel_DTwoPressDeletesCodex(t *testing.T) {
	dir := t.TempDir()
	authPath := seedLoginCodexAuth(t, dir)

	m := NewLoginModel("", dir)
	d := tea.KeyPressMsg{Text: "d", Code: 'd'}

	// First d — arms only.
	m, _ = m.Update(d)
	if m.deleteArmedIdx != 0 {
		t.Errorf("first d should arm deleteArmedIdx=0; got %d", m.deleteArmedIdx)
	}
	if _, err := os.Stat(authPath); err != nil {
		t.Errorf("first d must not delete the file: %v", err)
	}

	// Second d — deletes.
	m, _ = m.Update(d)
	if m.deleteArmedIdx != -1 {
		t.Errorf("deleteArmedIdx should reset to -1 after delete; got %d", m.deleteArmedIdx)
	}
	if _, err := os.Stat(authPath); !os.IsNotExist(err) {
		t.Errorf("codex-auth.json should be removed by d; stat err: %v", err)
	}
	if len(m.entries) != 0 {
		t.Errorf("entry should be dropped; entries=%#v", m.entries)
	}
}

// TestLoginModel_RDoesNotDelete pins that r is no longer wired to the delete
// path: a single r on a codex entry must not arm deletion.
func TestLoginModel_RDoesNotDelete(t *testing.T) {
	dir := t.TempDir()
	authPath := seedLoginCodexAuth(t, dir)

	m := NewLoginModel("", dir)
	r := tea.KeyPressMsg{Text: "r", Code: 'r'}
	m, _ = m.Update(r)
	if m.deleteArmedIdx != -1 {
		t.Errorf("r must not arm deletion; deleteArmedIdx=%d", m.deleteArmedIdx)
	}
	if _, err := os.Stat(authPath); err != nil {
		t.Errorf("r must leave the credential file intact: %v", err)
	}
}

// TestLoginModel_ActiveIndicatorRendered verifies the (active) marker appears on
// the row whose account all saved Codex presets reference.
func TestLoginModel_ActiveIndicatorRendered(t *testing.T) {
	_, globalDir := withTempCodexHome(t)

	acctPath := filepath.Join(globalDir, codexAuthSubdir, "work.json")
	writeCodexTokenAt(t, acctPath, "work@example.com")
	ref := codexAuthRefForPath(globalDir, acctPath)
	saveCodexPresetForTest(t, "codex-a", ref)
	saveCodexPresetForTest(t, "codex-b", ref)

	m := NewLoginModel("", globalDir)
	m.width = 100
	view := m.View()
	if !strings.Contains(view, i18n.T("login.codex_active_marker")) {
		t.Fatalf("view should mark the active account; view=%q", view)
	}
}
