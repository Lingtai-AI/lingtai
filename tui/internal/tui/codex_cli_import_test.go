package tui

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeCodexTestJWT builds an unsigned synthetic JWT (base64url, no padding,
// trailing ".sig") whose payload carries the given claims. No code path under
// test verifies the signature; the shape only needs to match what
// extractEmailFromJWT / extractAccountIDFromJWT decode.
func makeCodexTestJWT(t *testing.T, claims map[string]interface{}) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal jwt claims: %v", err)
	}
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

// codexCLIProfileClaims returns OpenAI-style JWT claims carrying both an email
// (https://api.openai.com/profile) and an account_id
// (https://api.openai.com/auth), matching what real Codex CLI tokens embed.
func codexCLIProfileClaims(email, accountID string) map[string]interface{} {
	return map[string]interface{}{
		"sub": "u-1",
		"https://api.openai.com/profile": map[string]string{"email": email},
		"https://api.openai.com/auth":    map[string]string{"account_id": accountID},
	}
}

// writeCodexCLIAuthFile writes a Codex CLI-shaped auth.json under dir and
// returns its path. An empty authMode defaults to "chatgpt".
func writeCodexCLIAuthFile(t *testing.T, dir, authMode, idToken, accessToken, refreshToken string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir cli dir: %v", err)
	}
	if authMode == "" {
		authMode = "chatgpt"
	}
	raw := map[string]interface{}{
		"auth_mode":      authMode,
		"OPENAI_API_KEY": nil,
		"tokens": map[string]interface{}{
			"id_token":      idToken,
			"access_token":  accessToken,
			"refresh_token": refreshToken,
			"account_id":    "ignored-field",
		},
		"last_refresh": "2026-01-01T00:00:00Z",
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		t.Fatalf("marshal cli auth: %v", err)
	}
	path := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write cli auth: %v", err)
	}
	return path
}

// TestReadCodexCLIAuthFile_Valid verifies the nested CLI format converts to
// the flat CodexTokens: tokens copied, email derived from the id_token,
// ExpiresAt zero (the CLI file carries no expiry).
func TestReadCodexCLIAuthFile_Valid(t *testing.T) {
	email := "sam@example.com"
	accountID := "org-12345678"
	idToken := makeCodexTestJWT(t, codexCLIProfileClaims(email, accountID))
	accessToken := makeCodexTestJWT(t, codexCLIProfileClaims(email, accountID))
	path := writeCodexCLIAuthFile(t, t.TempDir(), "chatgpt", idToken, accessToken, "rt-abc")

	tok, ok := readCodexCLIAuthFile(path)
	if !ok {
		t.Fatal("a valid CLI auth file should parse")
	}
	if tok.AccessToken != accessToken || tok.RefreshToken != "rt-abc" {
		t.Fatalf("token material mismatch: access=%q refresh=%q", tok.AccessToken, tok.RefreshToken)
	}
	if tok.Email != email {
		t.Fatalf("email should be derived from the id_token; got %q", tok.Email)
	}
	if tok.ExpiresAt != 0 {
		t.Fatalf("CLI file carries no expiry; ExpiresAt should be 0, got %d", tok.ExpiresAt)
	}
}

// TestReadCodexCLIAuthFile_MissingRefreshToken verifies ok=false when the CLI
// file has no refresh token.
func TestReadCodexCLIAuthFile_MissingRefreshToken(t *testing.T) {
	claims := codexCLIProfileClaims("sam@example.com", "org-12345678")
	path := writeCodexCLIAuthFile(t, t.TempDir(), "chatgpt", makeCodexTestJWT(t, claims), makeCodexTestJWT(t, claims), "")
	if _, ok := readCodexCLIAuthFile(path); ok {
		t.Fatal("CLI auth without a refresh_token must not parse as valid")
	}
}

// TestReadCodexCLIAuthFile_WrongAuthMode verifies ok=false unless auth_mode is
// "chatgpt" (e.g. an enterprise "sso" auth file is not importable).
func TestReadCodexCLIAuthFile_WrongAuthMode(t *testing.T) {
	claims := codexCLIProfileClaims("sam@example.com", "org-12345678")
	path := writeCodexCLIAuthFile(t, t.TempDir(), "sso", makeCodexTestJWT(t, claims), makeCodexTestJWT(t, claims), "rt-abc")
	if _, ok := readCodexCLIAuthFile(path); ok {
		t.Fatal("non-chatgpt auth_mode must not parse as valid")
	}
}

// TestCodexCLIAuthPath_HonorsCODEXHOME verifies the CODEX_HOME override and
// the ~/.codex fallback.
func TestCodexCLIAuthPath_HonorsCODEXHOME(t *testing.T) {
	t.Setenv("CODEX_HOME", "/tmp/codex-home")
	if got := codexCLIAuthPath(); got != filepath.Join("/tmp/codex-home", "auth.json") {
		t.Fatalf("CODEX_HOME should point at $CODEX_HOME/auth.json; got %q", got)
	}
	t.Setenv("CODEX_HOME", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	if got := codexCLIAuthPath(); got != filepath.Join(home, ".codex", "auth.json") {
		t.Fatalf("unset CODEX_HOME should fall back to ~/.codex/auth.json; got %q", got)
	}
}

// TestImportCodexCLIAuth_WritesPerAccountFile verifies the end-to-end import:
// slug from the first 8 account_id chars, ref resolvable onto the written
// file, 0600 perms, and the CLI file left byte-identical.
func TestImportCodexCLIAuth_WritesPerAccountFile(t *testing.T) {
	email := "sam@example.com"
	accountID := "org-12345678"
	claims := codexCLIProfileClaims(email, accountID)
	cliDir := t.TempDir()
	t.Setenv("CODEX_HOME", cliDir)
	writeCodexCLIAuthFile(t, cliDir, "chatgpt", makeCodexTestJWT(t, claims), makeCodexTestJWT(t, claims), "rt-abc")
	cliPath := filepath.Join(cliDir, "auth.json")
	before, err := os.ReadFile(cliPath)
	if err != nil {
		t.Fatalf("read cli auth: %v", err)
	}

	globalDir := t.TempDir()
	ref, label, err := importCodexCLIAuth(globalDir)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if ref != "codex-auth/org-1234.json" {
		t.Fatalf("slug should come from the first 8 account_id chars; got ref %q", ref)
	}
	if label != email {
		t.Fatalf("label should be the account email; got %q", label)
	}

	target := resolveCodexAuthPath(globalDir, ref)
	if target != filepath.Join(globalDir, "codex-auth", "org-1234.json") {
		t.Fatalf("ref should resolve onto the written file; got %q", target)
	}
	tok, ok := readCodexTokenFile(target)
	if !ok || tok.RefreshToken != "rt-abc" || tok.Email != email {
		t.Fatalf("imported file should parse with the CLI credential: ok=%v tok=%+v", ok, tok)
	}
	if fi, err := os.Stat(target); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("imported token file should be 0600; mode=%v err=%v", fi.Mode(), err)
	}

	after, err := os.ReadFile(cliPath)
	if err != nil {
		t.Fatalf("re-read cli auth: %v", err)
	}
	if string(after) != string(before) {
		t.Fatal("import must not modify the Codex CLI auth file")
	}
}

// TestImportCodexCLIAuth_SlugFallsBackToEmail verifies the email-local-part
// slug when the access token carries no account_id.
func TestImportCodexCLIAuth_SlugFallsBackToEmail(t *testing.T) {
	email := "sam.smith@example.com"
	claims := map[string]interface{}{
		"sub": "u-1",
		"https://api.openai.com/profile": map[string]string{"email": email},
	}
	cliDir := t.TempDir()
	t.Setenv("CODEX_HOME", cliDir)
	writeCodexCLIAuthFile(t, cliDir, "chatgpt", makeCodexTestJWT(t, claims), makeCodexTestJWT(t, claims), "rt-abc")

	ref, label, err := importCodexCLIAuth(t.TempDir())
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if ref != "codex-auth/sam-smith.json" {
		t.Fatalf("slug should derive from the email local part; got %q", ref)
	}
	if label != email {
		t.Fatalf("label should be the email; got %q", label)
	}
}

// TestImportCodexCLIAuth_AlreadyImportedSameAccount verifies a second import
// of the same account (same access-token account_id) fails with "already
// imported" and leaves the stored credential untouched.
func TestImportCodexCLIAuth_AlreadyImportedSameAccount(t *testing.T) {
	email := "sam@example.com"
	accountID := "org-12345678"
	claims := codexCLIProfileClaims(email, accountID)
	cliDir := t.TempDir()
	t.Setenv("CODEX_HOME", cliDir)
	writeCodexCLIAuthFile(t, cliDir, "chatgpt", makeCodexTestJWT(t, claims), makeCodexTestJWT(t, claims), "rt-abc")
	globalDir := t.TempDir()

	if _, _, err := importCodexCLIAuth(globalDir); err != nil {
		t.Fatalf("first import should succeed: %v", err)
	}
	// Same account, new refresh token: re-import must refuse, not clobber.
	writeCodexCLIAuthFile(t, cliDir, "chatgpt", makeCodexTestJWT(t, claims), makeCodexTestJWT(t, claims), "rt-different")
	if _, _, err := importCodexCLIAuth(globalDir); err == nil || !strings.Contains(err.Error(), "already imported") {
		t.Fatalf("re-import of the same account should fail with 'already imported'; got %v", err)
	}
	tok, _ := readCodexTokenFile(filepath.Join(globalDir, "codex-auth", "org-1234.json"))
	if tok.RefreshToken != "rt-abc" {
		t.Fatalf("existing credential must not be overwritten; got %q", tok.RefreshToken)
	}
}

// TestImportCodexCLIAuth_AlreadyImportedLegacy verifies a valid legacy
// single-account file blocks the import entirely.
func TestImportCodexCLIAuth_AlreadyImportedLegacy(t *testing.T) {
	claims := codexCLIProfileClaims("sam@example.com", "org-12345678")
	cliDir := t.TempDir()
	t.Setenv("CODEX_HOME", cliDir)
	writeCodexCLIAuthFile(t, cliDir, "chatgpt", makeCodexTestJWT(t, claims), makeCodexTestJWT(t, claims), "rt-abc")
	globalDir := t.TempDir()
	writeStubCodexToken(t, filepath.Join(globalDir, "codex-auth.json"), "other@example.com")

	if _, _, err := importCodexCLIAuth(globalDir); err == nil || !strings.Contains(err.Error(), "already imported") {
		t.Fatalf("a valid legacy credential should block import; got %v", err)
	}
	if _, err := os.Stat(filepath.Join(globalDir, "codex-auth", "org-1234.json")); !os.IsNotExist(err) {
		t.Fatal("import must not write a per-account file when already imported")
	}
}

// TestImportCodexCLIAuth_NeverOverwritesExistingFile verifies the no-clobber
// guarantee: a pre-existing per-account file with a different account_id but
// the same slug blocks the import and stays byte-identical.
func TestImportCodexCLIAuth_NeverOverwritesExistingFile(t *testing.T) {
	claims := codexCLIProfileClaims("sam@example.com", "org-12345678")
	cliDir := t.TempDir()
	t.Setenv("CODEX_HOME", cliDir)
	writeCodexCLIAuthFile(t, cliDir, "chatgpt", makeCodexTestJWT(t, claims), makeCodexTestJWT(t, claims), "rt-abc")
	globalDir := t.TempDir()

	target := filepath.Join(globalDir, "codex-auth", "org-1234.json")
	writeStubCodexToken(t, target, "other@example.com")

	if _, _, err := importCodexCLIAuth(globalDir); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("import must refuse to overwrite an existing file; got %v", err)
	}
	tok, ok := readCodexTokenFile(target)
	if !ok || tok.Email != "other@example.com" {
		t.Fatalf("existing file should be untouched: ok=%v tok=%+v", ok, tok)
	}
}
