package tui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// These tests cover ensureFreshCodexTokens (oauth.go), the refresh helper the
// startup validator (validateOneCodexAuthFile) relies on. They were
// previously exercised indirectly through the retired save-time Codex model
// probe; they now target the helper directly.

// setCodexTokenURLForTest points codexTokenURL at a test server for the
// duration of the test and returns a restore func, so refreshCodexTokens is
// testable without ever contacting auth.openai.com.
func setCodexTokenURLForTest(url string) (restore func()) {
	prev := codexTokenURL
	codexTokenURL = url
	return func() { codexTokenURL = prev }
}

// writeCodexTokenWithExpiry writes a token bundle with an explicit expires_at
// (unix seconds), simulating an on-disk token that is expired or current.
func writeCodexTokenWithExpiry(t *testing.T, path, access, refresh string, expiresAt int64) CodexTokens {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"access_token":%q,"refresh_token":%q,"expires_at":%d}`, access, refresh, expiresAt)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	tokens, ok := readCodexTokenFile(path)
	if !ok {
		t.Fatalf("read back token file %s", path)
	}
	return tokens
}

func TestEnsureFreshCodexTokensKeepsCurrentToken(t *testing.T) {
	restore := setCodexTokenURLForTest("http://127.0.0.1:1") // must never be dialed
	defer restore()
	path := filepath.Join(t.TempDir(), "codex-auth.json")
	tokens := writeCodexTokenWithExpiry(t, path, "current-access", "refresh", 4102444800) // 2100-01-01

	got, err := ensureFreshCodexTokens(path, tokens)
	if err != nil || got.AccessToken != "current-access" {
		t.Fatalf("ensureFreshCodexTokens = %+v, %v; want the current token unchanged", got, err)
	}
}

func TestEnsureFreshCodexTokensRefreshesExpiredTokenAndWritesBack(t *testing.T) {
	const freshAccess = "fresh-access-token"
	var tokenCalls int32
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&tokenCalls, 1)
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse refresh form: %v", err)
		}
		if got := r.FormValue("refresh_token"); got != "stale-refresh" {
			t.Fatalf("refresh_token = %q, want stale-refresh", got)
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"access_token":%q,"refresh_token":"stale-refresh","expires_in":3600}`, freshAccess)
	}))
	defer tokenSrv.Close()
	restore := setCodexTokenURLForTest(tokenSrv.URL)
	defer restore()

	path := filepath.Join(t.TempDir(), "codex-auth.json")
	tokens := writeCodexTokenWithExpiry(t, path, "stale-access", "stale-refresh", 1) // already expired

	got, err := ensureFreshCodexTokens(path, tokens)
	if err != nil || got.AccessToken != freshAccess {
		t.Fatalf("ensureFreshCodexTokens = %+v, %v; want the refreshed token", got, err)
	}
	if atomic.LoadInt32(&tokenCalls) != 1 {
		t.Fatalf("token refresh calls = %d, want 1", tokenCalls)
	}
	refreshed, ok := readCodexTokenFile(path)
	if !ok || refreshed.AccessToken != freshAccess {
		t.Fatalf("on-disk token not updated with refreshed access_token: %+v (ok=%v)", refreshed, ok)
	}
}

func TestEnsureFreshCodexTokensRevokedGrantIsRevoked(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer tokenSrv.Close()
	restore := setCodexTokenURLForTest(tokenSrv.URL)
	defer restore()

	path := filepath.Join(t.TempDir(), "codex-auth.json")
	tokens := writeCodexTokenWithExpiry(t, path, "stale-access", "revoked-refresh", 1)

	if _, err := ensureFreshCodexTokens(path, tokens); err != ErrCodexAuthRevoked {
		t.Fatalf("ensureFreshCodexTokens err = %v, want ErrCodexAuthRevoked", err)
	}
}

// A transient refresh failure (connection dropped) must be reported as
// transient — distinct from a revoked grant — and must leave the on-disk
// token untouched.
func TestEnsureFreshCodexTokensTransientFailureLeavesTokenUntouched(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("test server does not support hijacking")
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Fatalf("hijack: %v", err)
		}
		conn.Close()
	}))
	defer tokenSrv.Close()
	restore := setCodexTokenURLForTest(tokenSrv.URL)
	defer restore()

	path := filepath.Join(t.TempDir(), "codex-auth.json")
	tokens := writeCodexTokenWithExpiry(t, path, "stale-access", "still-good-refresh", 1)

	if _, err := ensureFreshCodexTokens(path, tokens); err != ErrCodexAuthTransient {
		t.Fatalf("ensureFreshCodexTokens err = %v, want ErrCodexAuthTransient", err)
	}
	unchanged, ok := readCodexTokenFile(path)
	if !ok || unchanged.AccessToken != "stale-access" {
		t.Fatalf("on-disk token was modified by a failed refresh: %+v (ok=%v)", unchanged, ok)
	}
}
