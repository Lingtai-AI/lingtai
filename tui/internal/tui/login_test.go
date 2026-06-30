package tui

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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

func withLoginHealthRefresh(t *testing.T, fn func(string, CodexTokens) (*CodexTokens, error)) {
	t.Helper()
	old := refreshCodexTokensForLoginHealth
	refreshCodexTokensForLoginHealth = fn
	t.Cleanup(func() { refreshCodexTokensForLoginHealth = old })
}

func TestCheckHealth_CodexOAuthUsesTokenFileNotAccessProbe(t *testing.T) {
	dir := t.TempDir()
	authPath := seedLoginCodexAuth(t, dir)

	var called atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Store(true)
		http.Error(w, "stale access token", http.StatusUnauthorized)
	}))
	defer srv.Close()

	msg := checkHealth(loginEntry{
		Provider:  "codex",
		IsOAuth:   true,
		BaseURL:   srv.URL,
		Key:       "stale-access-token",
		CodexPath: authPath,
	})

	if called.Load() {
		t.Fatal("Codex OAuth health check must not probe /codex/models with a short-lived access token")
	}
	if msg.Status != loginValid {
		t.Fatalf("Codex OAuth with a valid refresh-token file should be valid; status=%v detail=%q", msg.Status, msg.Detail)
	}
}

func TestCheckHealth_CodexOAuthRefreshesExpiredTokenFile(t *testing.T) {
	dir := t.TempDir()
	authPath := filepath.Join(dir, "codex-auth.json")
	oldTokens := CodexTokens{
		AccessToken:  "old-access",
		RefreshToken: "old-refresh",
		ExpiresAt:    time.Now().Add(-time.Hour).Unix(),
		Email:        "stub@example.com",
	}
	data, _ := json.Marshal(oldTokens)
	if err := os.WriteFile(authPath, data, 0o600); err != nil {
		t.Fatalf("write expired token fixture: %v", err)
	}

	withLoginHealthRefresh(t, func(refresh string, existing CodexTokens) (*CodexTokens, error) {
		if refresh != "old-refresh" {
			t.Fatalf("refresh token = %q, want old-refresh", refresh)
		}
		fresh := existing
		fresh.AccessToken = "new-access"
		fresh.ExpiresAt = time.Now().Add(time.Hour).Unix()
		return &fresh, nil
	})

	msg := checkHealth(loginEntry{Provider: "codex", IsOAuth: true, CodexPath: authPath})
	if msg.Status != loginValid {
		t.Fatalf("expired access token with refresh success should be valid; status=%v detail=%q", msg.Status, msg.Detail)
	}
	got, ok := readCodexTokenFile(authPath)
	if !ok {
		t.Fatal("refreshed token file should parse")
	}
	if got.AccessToken != "new-access" {
		t.Fatalf("refreshed token file access token = %q, want new-access", got.AccessToken)
	}
}

func TestCheckHealth_CodexOAuthDistinguishesRevokedAndTransientRefresh(t *testing.T) {
	dir := t.TempDir()
	authPath := filepath.Join(dir, "codex-auth.json")
	tokens := CodexTokens{
		AccessToken:  "old-access",
		RefreshToken: "old-refresh",
		ExpiresAt:    time.Now().Add(-time.Hour).Unix(),
	}
	data, _ := json.Marshal(tokens)
	if err := os.WriteFile(authPath, data, 0o600); err != nil {
		t.Fatalf("write expired token fixture: %v", err)
	}

	t.Run("revoked", func(t *testing.T) {
		withLoginHealthRefresh(t, func(string, CodexTokens) (*CodexTokens, error) {
			return nil, ErrCodexAuthRevoked
		})
		msg := checkHealth(loginEntry{Provider: "codex", IsOAuth: true, CodexPath: authPath})
		if msg.Status != loginInvalid {
			t.Fatalf("revoked refresh token should be invalid; status=%v detail=%q", msg.Status, msg.Detail)
		}
	})

	t.Run("transient", func(t *testing.T) {
		withLoginHealthRefresh(t, func(string, CodexTokens) (*CodexTokens, error) {
			return nil, errors.New("temporary network failure")
		})
		msg := checkHealth(loginEntry{Provider: "codex", IsOAuth: true, CodexPath: authPath})
		if msg.Status != loginError {
			t.Fatalf("transient refresh failure should be a health error, not invalid; status=%v detail=%q", msg.Status, msg.Detail)
		}
	})
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

// (The legacy [r] two-press logout shortcut was removed: r now re-authenticates
// the selected Codex account. Deletion is d / Del / Backspace. New coverage:
// TestLoginModel_DTwoPressDeletesCodex, TestLoginModel_RReauthsExistingAccount,
// and TestLoginModel_RDoesNotDelete in login_active_test.go.)

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

// TestLoginModel_CodexRReauthShowsMethodChooser verifies the method chooser is
// now reached via r (re-auth) on an existing Codex row — Enter sets the account
// active instead (see TestLoginModel_EnterOnCodexSetsActiveAppliesToSavedPresets).
// HOME is isolated so the no-op apply that r does NOT trigger can't touch real
// presets, and seedLoginCodexAuth writes the legacy file under the temp home.
func TestLoginModel_CodexRReauthShowsMethodChooser(t *testing.T) {
	_, globalDir := withTempCodexHome(t)
	seedLoginCodexAuth(t, globalDir)

	m := NewLoginModel("", globalDir)
	if len(m.entries) != 1 || !m.entries[0].IsOAuth {
		t.Fatalf("expected single codex OAuth entry; got %#v", m.entries)
	}

	r := tea.KeyPressMsg{Text: "r", Code: 'r'}
	m, cmd := m.Update(r)
	if cmd != nil {
		t.Fatal("opening the method chooser must not start a network command")
	}
	if !m.codexChoosingMethod {
		t.Fatal("r on a Codex OAuth entry should show the method chooser")
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

// TestLoginModel_EscDuringLoginStaysOnCredentials verifies the reported
// UX-bug fix: pressing Esc while a Codex login is mid-flight cancels the
// login and stays on the credentials screen — it must NOT emit a
// ViewChangeMsg that would dump the user back to the home/mail view.
func TestLoginModel_EscDuringLoginStaysOnCredentials(t *testing.T) {
	dir := t.TempDir()
	m := NewLoginModel("", dir)
	cancelled := false
	m.codexLogging = true
	m.codexCancel = func() { cancelled = true }
	m.codexAuthURL = "https://auth.openai.com/x"
	m.codexDeviceURL = "https://auth.openai.com/codex/device"
	m.codexDeviceCode = "ABCD-1234"
	startEpoch := m.codexLoginEpoch

	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	if !cancelled {
		t.Error("Esc during login must invoke codexCancel")
	}
	if cmd != nil {
		// Run the command and confirm it is NOT a ViewChangeMsg to mail.
		if msg := cmd(); msg != nil {
			if vc, ok := msg.(ViewChangeMsg); ok {
				t.Fatalf("Esc mid-login must not change view; got ViewChangeMsg{View:%q}", vc.View)
			}
		}
	}
	if m.codexLogging {
		t.Error("codexLogging should be cleared after Esc cancel")
	}
	if m.codexLoginEpoch == startEpoch {
		t.Error("codexLoginEpoch should bump on Esc cancel so late callbacks are dropped")
	}
	if m.codexAuthURL != "" || m.codexDeviceURL != "" || m.codexDeviceCode != "" {
		t.Errorf("Esc cancel must clear transient login fields; url=%q devURL=%q code=%q",
			m.codexAuthURL, m.codexDeviceURL, m.codexDeviceCode)
	}
}

// TestLoginModel_EscWithNoLoginExitsToMail verifies that when no Codex
// login is in flight, Esc still returns to the home/mail view (unchanged
// behavior for the idle credentials screen).
func TestLoginModel_EscWithNoLoginExitsToMail(t *testing.T) {
	dir := t.TempDir()
	m := NewLoginModel("", dir)
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("idle Esc should emit a ViewChangeMsg command")
	}
	msg := cmd()
	vc, ok := msg.(ViewChangeMsg)
	if !ok || vc.View != "mail" {
		t.Fatalf("idle Esc should return to mail; got %T %+v", msg, msg)
	}
}

// TestLoginModel_CodexRowShownWithExistingEntry verifies the multi-Codex
// unblock: an "Add another Codex account" row is shown even when a Codex
// credential already exists, so adding a SECOND account is always reachable
// (it targets a new token file, not the existing one). Re-auth of the
// existing account is a separate action (Enter on the entry itself).
func TestLoginModel_CodexRowShownWithExistingEntry(t *testing.T) {
	dir := t.TempDir()
	seedLoginCodexAuth(t, dir)

	m := NewLoginModel("", dir)
	if len(m.entries) != 1 || m.entries[0].Provider != "codex" {
		t.Fatalf("precondition: expected single codex entry; got %#v", m.entries)
	}
	if !m.virtualAddCodexRow() {
		t.Fatal("Codex add-account row must remain available when a codex entry exists")
	}
	m.width = 80
	view := m.View()
	if !strings.Contains(view, "Add another Codex account") {
		t.Fatalf("view should show the add-another-account row; view=%q", view)
	}

	// Cursor on the virtual row (index == len(entries)). Enter opens the
	// chooser AND targets a new account (empty target path).
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.cursor != 1 {
		t.Fatalf("Down should reach the virtual row at index 1; got %d", m.cursor)
	}
	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("Enter on the add-account row must not start a network command")
	}
	if !m.codexChoosingMethod {
		t.Fatal("Enter on the add-account row should open the Codex method chooser")
	}
	if m.codexLoginTargetPath != "" {
		t.Fatalf("add-account must target a NEW account (empty path); got %q", m.codexLoginTargetPath)
	}
}

// TestLoginModel_ReauthExistingAccountTargetsItsFile verifies that r on an
// existing Codex account entry sets the login target to THAT account's token
// file, so re-auth overwrites the right account rather than creating a new one.
// (Enter now sets the account active; re-auth moved to r.)
func TestLoginModel_ReauthExistingAccountTargetsItsFile(t *testing.T) {
	_, globalDir := withTempCodexHome(t)
	seedLoginCodexAuth(t, globalDir)

	m := NewLoginModel("", globalDir)
	if len(m.entries) != 1 || m.entries[0].Provider != "codex" {
		t.Fatalf("precondition: expected single codex entry; got %#v", m.entries)
	}
	wantPath := m.entries[0].CodexPath
	if wantPath == "" {
		t.Fatal("codex entry should carry its token file path")
	}
	// cursor starts at 0 (the codex entry). r re-auths this account.
	r := tea.KeyPressMsg{Text: "r", Code: 'r'}
	m, cmd := m.Update(r)
	if cmd != nil {
		t.Fatal("r on a codex entry must not start a network command")
	}
	if !m.codexChoosingMethod {
		t.Fatal("r on a codex entry should open the method chooser")
	}
	if m.codexLoginTargetPath != wantPath {
		t.Fatalf("re-auth should target the account's own file %q; got %q", wantPath, m.codexLoginTargetPath)
	}
}

// TestLoginModel_AddAccountWritesNewFileNotLegacy verifies that completing an
// "add another account" login (empty target) when a legacy account already
// exists writes a NEW per-account file under codex-auth/ rather than
// overwriting the legacy account.
func TestLoginModel_AddAccountWritesNewFileNotLegacy(t *testing.T) {
	dir := t.TempDir()
	seedLoginCodexAuth(t, dir) // legacy account present (stub@example.com)
	legacy := filepath.Join(dir, "codex-auth.json")
	legacyBefore, _ := os.ReadFile(legacy)

	m := NewLoginModel("", dir)
	// Simulate the user choosing "add another account": target stays empty.
	m.codexLoginTargetPath = ""
	m.codexLogging = true
	m.codexLoginEpoch = 7

	done := CodexOAuthDoneMsg{
		Epoch: 7,
		Tokens: &CodexTokens{
			AccessToken:  "new-access",
			RefreshToken: "new-refresh",
			Email:        "second@example.com",
		},
	}
	m, _ = m.Update(done)

	// Legacy file must be untouched.
	legacyAfter, _ := os.ReadFile(legacy)
	if string(legacyBefore) != string(legacyAfter) {
		t.Error("adding a new account must not overwrite the legacy account file")
	}
	// A new per-account file must now exist under codex-auth/.
	accts := listCodexAccounts(dir)
	if len(accts) != 2 {
		t.Fatalf("expected 2 accounts after add; got %d: %#v", len(accts), accts)
	}
	foundSecond := false
	for _, a := range accts {
		if a.Email == "second@example.com" && !a.Legacy {
			foundSecond = true
		}
	}
	if !foundSecond {
		t.Errorf("new per-account file for second@example.com not found; accts=%#v", accts)
	}
}

// TestLoginModel_FirstAccountSeedsLegacyFile verifies that the FIRST Codex
// login (no account yet) seeds the legacy file, so existing presets with no
// codex_auth_path keep working without any field churn.
func TestLoginModel_FirstAccountSeedsLegacyFile(t *testing.T) {
	dir := t.TempDir()
	m := NewLoginModel("", dir)
	if len(m.entries) != 0 {
		t.Fatalf("precondition: expected no entries; got %#v", m.entries)
	}
	m.codexLoginTargetPath = ""
	m.codexLogging = true
	m.codexLoginEpoch = 1
	m, _ = m.Update(CodexOAuthDoneMsg{
		Epoch:  1,
		Tokens: &CodexTokens{AccessToken: "a", RefreshToken: "r", Email: "first@example.com"},
	})
	if _, err := os.Stat(filepath.Join(dir, "codex-auth.json")); err != nil {
		t.Fatalf("first account should seed the legacy file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "codex-auth")); err == nil {
		t.Error("first account should not create a per-account dir")
	}
}

// TestLoginModel_NoClaudeCodeAuthRow verifies the Claude Code auth row is
// no longer surfaced on the credentials screen (the auth path is
// unsupported for now and intentionally hidden).
func TestLoginModel_NoClaudeCodeAuthRow(t *testing.T) {
	dir := t.TempDir()
	m := NewLoginModel("", dir)
	m.width = 80
	view := m.View()
	if strings.Contains(view, "Claude Code auth") {
		t.Fatalf("credentials view must not render the Claude Code auth row; view=%q", view)
	}
}

// TestLoginModel_CodexForceLoginDecision pins the add-another-account fix at
// the decision layer: codexForceLogin (which feeds prompt=login into
// buildAuthorizeURL) must be true ONLY when adding a new account while one
// already exists. Re-auth of an existing account and seeding the very first
// account must NOT force the OpenAI login page — those legitimately reuse the
// active ChatGPT session.
func TestLoginModel_CodexForceLoginDecision(t *testing.T) {
	t.Run("add-another-with-existing forces login", func(t *testing.T) {
		dir := t.TempDir()
		seedLoginCodexAuth(t, dir) // one account already present
		m := NewLoginModel("", dir)
		// "Add another account": empty target, an existing codex entry present.
		m.codexLoginTargetPath = ""
		if !m.codexForceLogin() {
			t.Fatal("adding another account with an existing one must force prompt=login")
		}
	})

	t.Run("re-auth existing account does not force login", func(t *testing.T) {
		dir := t.TempDir()
		seedLoginCodexAuth(t, dir)
		m := NewLoginModel("", dir)
		// Re-auth targets the existing account's own file.
		m.codexLoginTargetPath = m.entries[0].CodexPath
		if m.codexForceLogin() {
			t.Fatal("re-authenticating an existing account must NOT force prompt=login")
		}
	})

	t.Run("first account does not force login", func(t *testing.T) {
		dir := t.TempDir()
		m := NewLoginModel("", dir)
		if len(m.entries) != 0 {
			t.Fatalf("precondition: expected no codex entries; got %#v", m.entries)
		}
		// First-ever account: empty target, no existing entry.
		m.codexLoginTargetPath = ""
		if m.codexForceLogin() {
			t.Fatal("seeding the first account must NOT force prompt=login")
		}
	})
}

// TestSetupAndFirstRunShareCodexAccountStore is the unification guard: the
// first-run wizard writes the primary Codex account to the legacy file
// (legacyCodexAuthPath), and the `/setup credentials` LoginModel must read its
// accounts from that SAME store (listCodexAccounts over globalDir) rather than
// a forked credential source. If a future change makes either path use its own
// store/file, this test breaks — first-run and /setup credentials must not
// fork credential/OAuth logic (see ANATOMY.md). Both also share the single
// OAuth entrypoint startOAuthFlow→buildAuthorizeURL.
func TestSetupAndFirstRunShareCodexAccountStore(t *testing.T) {
	dir := t.TempDir()

	// Simulate what first-run does on a successful primary login: write the
	// legacy account file (firstrun.go's CodexOAuthDoneMsg handler uses
	// legacyCodexAuthPath). We mirror that exact path here.
	tok := CodexTokens{
		AccessToken:  "primary-access",
		RefreshToken: "primary-refresh",
		ExpiresAt:    9999999999,
		Email:        "primary@example.com",
	}
	data, _ := json.Marshal(tok)
	if err := os.WriteFile(legacyCodexAuthPath(dir), data, 0o600); err != nil {
		t.Fatalf("seed legacy codex account: %v", err)
	}

	// The /setup credentials model (same constructor /login uses) must see the
	// account first-run wrote — proving a shared store, not a forked one.
	m := NewSetupCredentialsModel("", dir)
	var codexEntry *loginEntry
	for i := range m.entries {
		if m.entries[i].Provider == "codex" {
			codexEntry = &m.entries[i]
			break
		}
	}
	if codexEntry == nil {
		t.Fatal("/setup credentials must enumerate the Codex account first-run wrote to the legacy store")
	}
	if codexEntry.CodexPath != legacyCodexAuthPath(dir) {
		t.Fatalf("setup credentials codex entry path = %q, want shared legacy path %q",
			codexEntry.CodexPath, legacyCodexAuthPath(dir))
	}
	if !codexEntry.CodexLegacy {
		t.Error("the first-run primary account must be surfaced as the legacy account")
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
