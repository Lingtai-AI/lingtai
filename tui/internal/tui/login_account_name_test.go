package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthropics/lingtai-tui/i18n"
)

// writeCodexAccountFile writes a stub token bundle to path (creating parent
// dirs). email may be "" to simulate a bundle whose id_token carried no email
// (older logins / a JWT without the profile claim). No real secret is used.
func writeCodexAccountFile(t *testing.T, path, email string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir for %q: %v", path, err)
	}
	tok := CodexTokens{
		AccessToken:  "stub-access",
		RefreshToken: "stub-refresh",
		ExpiresAt:    9999999999,
		Email:        email,
	}
	data, _ := json.Marshal(tok)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

// TestCodexAccountDisplay_LabelPerCase pins the display label helper directly:
// email wins, then the per-account file slug, then the localized default-account
// label for the legacy file with no email. Every case is prefixed "OAuth — " so
// the row always shows a recognizable name, never a bare "OAuth".
func TestCodexAccountDisplay_LabelPerCase(t *testing.T) {
	defaultLabel := i18n.T("codex.account_default")

	cases := []struct {
		name string
		acct codexAccount
		want string
	}{
		{
			name: "email present",
			acct: codexAccount{Email: "alice@example.com"},
			want: "OAuth — alice@example.com",
		},
		{
			name: "per-account file, no email -> slug",
			acct: codexAccount{Path: "/x/codex-auth/work-bob.json", Email: ""},
			want: "OAuth — work-bob",
		},
		{
			name: "legacy, no email -> localized default label",
			acct: codexAccount{Legacy: true, Email: ""},
			want: "OAuth — " + defaultLabel,
		},
		{
			name: "legacy with email prefers email",
			acct: codexAccount{Legacy: true, Email: "primary@example.com"},
			want: "OAuth — primary@example.com",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := codexAccountDisplay(tc.acct); got != tc.want {
				t.Fatalf("codexAccountDisplay = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestLoginModel_MultiAccountRowsShowRecognizableNames is the acceptance test
// for Jason's report: with several Codex accounts, EACH row must carry a
// recognizable account name. This seeds three accounts — a legacy file with no
// email (the previously-bare "OAuth" row), a per-account file with an email, and
// a per-account file with no email (slug only) — and asserts all three names
// appear in the rendered credentials view, and that no row renders a bare
// "OAuth" with nothing after it.
func TestLoginModel_MultiAccountRowsShowRecognizableNames(t *testing.T) {
	_, globalDir := withTempCodexHome(t)

	// Legacy file, no email — used to render as a bare "OAuth".
	writeCodexAccountFile(t, legacyCodexAuthPath(globalDir), "")
	// Per-account file with an email.
	writeCodexAccountFile(t, filepath.Join(codexAuthDir(globalDir), "alice.json"), "alice@example.com")
	// Per-account file without an email — recognizable via its slug.
	writeCodexAccountFile(t, filepath.Join(codexAuthDir(globalDir), "work-bob.json"), "")

	m := NewLoginModel("", globalDir)
	if len(m.entries) != 3 {
		t.Fatalf("expected 3 codex entries; got %d: %#v", len(m.entries), m.entries)
	}

	defaultLabel := i18n.T("codex.account_default")
	for _, e := range m.entries {
		if e.Provider != "codex" {
			t.Fatalf("unexpected non-codex entry: %#v", e)
		}
		// No row may be a bare "OAuth" with no distinguishing suffix.
		if strings.TrimSpace(e.Display) == "OAuth" {
			t.Fatalf("codex row has no recognizable name (bare OAuth): %#v", e)
		}
	}

	m.width = 100
	view := m.View()
	for _, want := range []string{"alice@example.com", "work-bob", defaultLabel} {
		if !strings.Contains(view, want) {
			t.Fatalf("credentials view missing recognizable name %q; view=%q", want, view)
		}
	}
}

// TestLoginModel_LegacyNoEmailRowNotBareOAuth isolates the legacy-only case: a
// single legacy account with no email must still show the localized default
// label rather than a bare "OAuth", so a user who later adds a second account
// can tell which is which.
func TestLoginModel_LegacyNoEmailRowNotBareOAuth(t *testing.T) {
	_, globalDir := withTempCodexHome(t)
	writeCodexAccountFile(t, legacyCodexAuthPath(globalDir), "")

	m := NewLoginModel("", globalDir)
	if len(m.entries) != 1 || !m.entries[0].CodexLegacy {
		t.Fatalf("expected one legacy codex entry; got %#v", m.entries)
	}
	want := "OAuth — " + i18n.T("codex.account_default")
	if m.entries[0].Display != want {
		t.Fatalf("legacy no-email display = %q, want %q", m.entries[0].Display, want)
	}
}

// TestLoginModel_OAuthDonePreservesRecognizableName verifies the re-auth/add
// completion path (CodexOAuthDoneMsg) also lands a recognizable name on the
// row. When the fresh tokens carry no email and the target is a per-account
// file, the row must show the file slug — not fall back to a bare "OAuth".
func TestLoginModel_OAuthDonePreservesRecognizableName(t *testing.T) {
	_, globalDir := withTempCodexHome(t)
	// Pre-existing legacy account so the "add another" completion writes a new
	// per-account file (its slug derives from the email, but tokens here have
	// none, so the derived path uses the "codex" fallback slug).
	writeCodexAccountFile(t, legacyCodexAuthPath(globalDir), "primary@example.com")

	m := NewLoginModel("", globalDir)
	// Re-auth an EXISTING per-account file with a known slug so we can assert on
	// it deterministically.
	target := filepath.Join(codexAuthDir(globalDir), "teammate.json")
	writeCodexAccountFile(t, target, "teammate@example.com")
	m = NewLoginModel("", globalDir)
	m.codexLoginTargetPath = target
	m.codexLogging = true
	m.codexLoginEpoch = 5

	// Fresh tokens with NO email (simulating a JWT that lacked the profile claim).
	m, _ = m.Update(CodexOAuthDoneMsg{
		Epoch:  5,
		Tokens: &CodexTokens{AccessToken: "a", RefreshToken: "r", Email: ""},
	})

	var got *loginEntry
	for i := range m.entries {
		if m.entries[i].CodexPath == target {
			got = &m.entries[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("re-auth target row not found; entries=%#v", m.entries)
	}
	if got.Display != "OAuth — teammate" {
		t.Fatalf("re-auth row display = %q, want %q", got.Display, "OAuth — teammate")
	}
}

// TestLoginModel_OAuthDoneLegacyNoEmailShowsDefault verifies the seed-first-
// account completion path: the very first login writes the legacy file, and if
// the tokens carry no email the row shows the localized default label, not a
// bare "OAuth".
func TestLoginModel_OAuthDoneLegacyNoEmailShowsDefault(t *testing.T) {
	_, globalDir := withTempCodexHome(t)
	m := NewLoginModel("", globalDir)
	if len(m.entries) != 0 {
		t.Fatalf("precondition: expected no entries; got %#v", m.entries)
	}
	m.codexLoginTargetPath = ""
	m.codexLogging = true
	m.codexLoginEpoch = 1
	m, _ = m.Update(CodexOAuthDoneMsg{
		Epoch:  1,
		Tokens: &CodexTokens{AccessToken: "a", RefreshToken: "r", Email: ""},
	})
	if len(m.entries) != 1 {
		t.Fatalf("expected one seeded legacy entry; got %#v", m.entries)
	}
	want := "OAuth — " + i18n.T("codex.account_default")
	if m.entries[0].Display != want {
		t.Fatalf("seeded legacy no-email display = %q, want %q", m.entries[0].Display, want)
	}
}
