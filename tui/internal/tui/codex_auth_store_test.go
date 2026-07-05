package tui

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func writeStubCodexToken(t *testing.T, path, email string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	tok := CodexTokens{
		AccessToken:  "stub-access",
		RefreshToken: "stub-refresh",
		ExpiresAt:    9999999999,
		Email:        email,
	}
	data, _ := json.Marshal(tok)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
}

func fakeCodexAccessTokenWithEmail(t *testing.T, email string) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload := map[string]interface{}{
		"sub": "u-1",
		"https://api.openai.com/profile": map[string]string{
			"email": email,
		},
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return fmt.Sprintf("%s.%s.sig", header, base64.RawURLEncoding.EncodeToString(payloadJSON))
}

// TestResolveCodexAuthPath_LegacyFallback verifies an empty ref resolves to the
// legacy single-account file.
func TestResolveCodexAuthPath_LegacyFallback(t *testing.T) {
	dir := t.TempDir()
	got := resolveCodexAuthPath(dir, "")
	want := filepath.Join(dir, "codex-auth.json")
	if got != want {
		t.Fatalf("empty ref should resolve to legacy file %q; got %q", want, got)
	}
}

// TestResolveCodexAuthPath_RelativeUnderGlobalDir verifies a bare relative ref
// lands inside the TUI-owned tree (not $PWD).
func TestResolveCodexAuthPath_RelativeUnderGlobalDir(t *testing.T) {
	dir := t.TempDir()
	got := resolveCodexAuthPath(dir, "codex-auth/work.json")
	want := filepath.Join(dir, "codex-auth", "work.json")
	if got != want {
		t.Fatalf("relative ref should resolve under globalDir %q; got %q", want, got)
	}
}

// TestCodexAuthPathValid distinguishes a valid token file from a missing or
// malformed one.
func TestCodexAuthPathValid(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.json")
	writeStubCodexToken(t, good, "a@example.com")
	if !codexAuthPathValid(good) {
		t.Error("a token file with a refresh_token should be valid")
	}
	if codexAuthPathValid(filepath.Join(dir, "missing.json")) {
		t.Error("a missing file should be invalid")
	}
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte(`{"access_token":"x"}`), 0o600) // no refresh_token
	if codexAuthPathValid(bad) {
		t.Error("a file without a refresh_token should be invalid")
	}
}

func TestReadCodexTokenFileDerivesEmailFromAccessToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "codex-auth.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	tok := CodexTokens{
		AccessToken:  fakeCodexAccessTokenWithEmail(t, "alice@example.com"),
		RefreshToken: "stub-refresh",
		ExpiresAt:    9999999999,
	}
	data, _ := json.Marshal(tok)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	got, ok := readCodexTokenFile(path)
	if !ok {
		t.Fatal("token file should parse as valid")
	}
	if got.Email != "alice@example.com" {
		t.Fatalf("expected email derived from access_token, got %q", got.Email)
	}
}

func TestReadCodexTokenFileKeepsEmailForInvalidRefresh(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "codex-auth.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	tok := CodexTokens{
		AccessToken: fakeCodexAccessTokenWithEmail(t, "invalid@example.com"),
		ExpiresAt:   9999999999,
	}
	data, _ := json.Marshal(tok)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	got, ok := readCodexTokenFile(path)
	if ok {
		t.Fatal("token file without refresh_token should remain invalid")
	}
	if got.Email != "invalid@example.com" {
		t.Fatalf("invalid token file should still keep derived email for display; got %q", got.Email)
	}
}

// TestListCodexAccounts_LegacyAndPerAccount verifies enumeration surfaces the
// legacy file (as a legacy account with empty ref) plus per-account files.
func TestListCodexAccounts_LegacyAndPerAccount(t *testing.T) {
	dir := t.TempDir()
	writeStubCodexToken(t, legacyCodexAuthPath(dir), "legacy@example.com")
	writeStubCodexToken(t, filepath.Join(codexAuthDir(dir), "work.json"), "work@example.com")

	accts := listCodexAccounts(dir)
	if len(accts) != 2 {
		t.Fatalf("expected 2 accounts (legacy + work); got %d: %#v", len(accts), accts)
	}
	if !accts[0].Legacy || accts[0].Ref != "" {
		t.Errorf("first account should be the legacy file with empty ref; got %#v", accts[0])
	}
	if accts[1].Legacy || accts[1].Ref == "" {
		t.Errorf("second account should be a per-account file with a non-empty ref; got %#v", accts[1])
	}
	if accts[1].Email != "work@example.com" {
		t.Errorf("per-account email mismatch; got %q", accts[1].Email)
	}
}

// TestNewCodexAuthPath_NoCollision verifies new account paths avoid clobbering
// existing files by appending a numeric suffix.
func TestNewCodexAuthPath_NoCollision(t *testing.T) {
	dir := t.TempDir()
	first := newCodexAuthPath(dir, "sam@example.com")
	if filepath.Base(first) != "sam.json" {
		t.Fatalf("first account for sam@ should be sam.json; got %q", filepath.Base(first))
	}
	writeStubCodexToken(t, first, "sam@example.com")
	second := newCodexAuthPath(dir, "sam@example.com")
	if second == first {
		t.Fatalf("second account must not collide with %q", first)
	}
	if filepath.Base(second) != "sam-2.json" {
		t.Fatalf("collision should yield sam-2.json; got %q", filepath.Base(second))
	}
}

// TestCodexAuthRefForPath_HomeShortened verifies a per-account path maps back to
// a "~/"-prefixed ref and the legacy file maps to the implicit empty ref.
func TestCodexAuthRefForPath_LegacyMapsToEmpty(t *testing.T) {
	dir := t.TempDir()
	if ref := codexAuthRefForPath(dir, legacyCodexAuthPath(dir)); ref != "" {
		t.Fatalf("legacy path should map to empty ref; got %q", ref)
	}
}
