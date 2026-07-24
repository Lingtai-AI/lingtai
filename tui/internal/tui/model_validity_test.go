package tui

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// writeCodexProbeToken writes a token bundle that is not due for refresh
// (expires_at far in the future), matching what a real, currently-valid
// Codex credential looks like on disk — every real writer (OAuth exchange,
// refreshCodexTokens) always sets expires_at, so a token file with none is
// not a state ensureFreshCodexTokens needs to special-case.
func writeCodexProbeToken(t *testing.T, path, access string) {
	t.Helper()
	writeCodexProbeTokenWithExpiry(t, path, access, "refresh", 4102444800) // 2100-01-01
}

// setCodexTokenURLForTest points codexTokenURL (oauth.go) at a test server
// for the duration of the test and returns a restore func. Keeps
// refreshCodexTokens testable without ever contacting auth.openai.com.
func setCodexTokenURLForTest(url string) (restore func()) {
	prev := codexTokenURL
	codexTokenURL = url
	return func() { codexTokenURL = prev }
}

// writeCodexProbeTokenWithExpiry writes a token bundle with an explicit
// expires_at (unix seconds), simulating an on-disk token that is expired or
// about to expire — the state a valid Codex account is in when the TUI has
// been open past the kernel's normal startup refresh.
func writeCodexProbeTokenWithExpiry(t *testing.T, path, access, refresh string, expiresAt int64) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"access_token":%q,"refresh_token":%q,"expires_at":%d}`, access, refresh, expiresAt)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCodexModelValidityUsesResponsesAndSelectedAccount(t *testing.T) {
	var gotAuth, gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/responses" {
			t.Fatalf("request = %s %s, want POST /responses", r.Method, r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		data, _ := io.ReadAll(r.Body)
		if strings.Contains(string(data), `"model":"gpt-test"`) {
			gotModel = "gpt-test"
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"object":"response","output":[]}`))
	}))
	defer srv.Close()

	globalDir := t.TempDir()
	writeCodexProbeToken(t, filepath.Join(globalDir, "codex-auth.json"), "selected-access")
	status, detail := probeCodexModel("codex", "gpt-test", srv.URL, globalDir, "")
	if status != probeOK || detail != "" {
		t.Fatalf("probe = %v, %q; want OK", status, detail)
	}
	if gotAuth != "Bearer selected-access" || gotModel != "gpt-test" {
		t.Fatalf("request auth/model = %q/%q", gotAuth, gotModel)
	}
}

// TestCodexModelValidityRefreshesExpiredAccessTokenBeforeProbing exercises
// the proven false-negative code path (not a claim about what the specific
// reported screenshot's token state actually was): a working ChatGPT-backed
// Codex account whose on-disk access_token has expired. Before this fix,
// readCodexTokenFile had no expiry check — unlike the kernel's
// CodexTokenManager.get_access_token, which refreshes before every use — so
// probeCodexModel sent the stale token straight to /responses, got a 401,
// and reported the account/model as ineligible even though the
// refresh_token is valid and a refreshed access_token is accepted.
func TestCodexModelValidityRefreshesExpiredAccessTokenBeforeProbing(t *testing.T) {
	const freshAccess = "fresh-access-token"
	var tokenCalls, responsesCalls int32

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
	restoreTokenURL := setCodexTokenURLForTest(tokenSrv.URL)
	defer restoreTokenURL()

	responsesSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&responsesCalls, 1)
		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+freshAccess {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid token"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"object":"response","output":[]}`))
	}))
	defer responsesSrv.Close()

	globalDir := t.TempDir()
	path := filepath.Join(globalDir, "codex-auth.json")
	writeCodexProbeTokenWithExpiry(t, path, "stale-access", "stale-refresh", 1) // already expired

	status, detail := probeCodexModel("codex", "gpt-5.6-sol", responsesSrv.URL, globalDir, "")
	if status != probeOK || detail != "" {
		t.Fatalf("probe = %v, %q; want OK after refreshing the expired token", status, detail)
	}
	if atomic.LoadInt32(&tokenCalls) != 1 {
		t.Fatalf("token refresh calls = %d, want 1", tokenCalls)
	}
	if atomic.LoadInt32(&responsesCalls) != 1 {
		t.Fatalf("responses calls = %d, want 1", responsesCalls)
	}

	refreshed, ok := readCodexTokenFile(path)
	if !ok || refreshed.AccessToken != freshAccess {
		t.Fatalf("on-disk token not updated with refreshed access_token: %+v (ok=%v)", refreshed, ok)
	}
}

// TestCodexModelValidityRevokedRefreshStaysAuthError proves a genuinely
// revoked account (refresh rejected with 401/403) still fails closed as a
// deterministic auth error, not silently passed.
func TestCodexModelValidityRevokedRefreshStaysAuthError(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer tokenSrv.Close()
	restoreTokenURL := setCodexTokenURLForTest(tokenSrv.URL)
	defer restoreTokenURL()

	responsesSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("must not reach the Responses endpoint with a token known to be unrefreshable")
	}))
	defer responsesSrv.Close()

	globalDir := t.TempDir()
	path := filepath.Join(globalDir, "codex-auth.json")
	writeCodexProbeTokenWithExpiry(t, path, "stale-access", "revoked-refresh", 1)

	status, detail := probeCodexModel("codex", "gpt-5.6-sol", responsesSrv.URL, globalDir, "")
	if status != probeAuthError {
		t.Fatalf("probe = %v, %q; want probeAuthError for a revoked refresh grant", status, detail)
	}
}

// TestCodexModelValidityTransientRefreshFailureIsRetryableNotAuthError
// proves the other half of the false-negative fix: when a token is due for
// refresh but the refresh itself fails for a reason unrelated to the grant
// (network error here), the probe must NOT fall back to sending the
// known-stale access token to /responses and let its 401 be misread as
// deterministic ineligibility. It must report an operational, retryable
// outcome (probeOverloaded), distinct from a hard auth error, so any
// caller of probeCodexModel can tell "temporarily unreachable" apart
// from "this account/model is genuinely ineligible."
func TestCodexModelValidityTransientRefreshFailureIsRetryableNotAuthError(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate a network-adjacent failure: close the connection without
		// a response, which surfaces to refreshCodexTokens as a non-nil,
		// non-HTTP-status error (client.PostForm returns an error), the same
		// class of failure as a DNS/timeout/connection-reset.
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
	restoreTokenURL := setCodexTokenURLForTest(tokenSrv.URL)
	defer restoreTokenURL()

	responsesSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("must not reach the Responses endpoint with a token known to be stale after a failed refresh")
	}))
	defer responsesSrv.Close()

	globalDir := t.TempDir()
	path := filepath.Join(globalDir, "codex-auth.json")
	writeCodexProbeTokenWithExpiry(t, path, "stale-access", "still-good-refresh", 1)

	status, detail := probeCodexModel("codex", "gpt-5.6-sol", responsesSrv.URL, globalDir, "")
	if status != probeOverloaded {
		t.Fatalf("probe = %v, %q; want probeOverloaded (retryable) for a transient refresh failure, not a hard auth error", status, detail)
	}

	// The on-disk token must be untouched: a transient failure must not
	// overwrite a possibly-still-valid refresh_token with anything.
	unchanged, ok := readCodexTokenFile(path)
	if !ok || unchanged.AccessToken != "stale-access" {
		t.Fatalf("on-disk token was modified by a failed refresh: %+v (ok=%v)", unchanged, ok)
	}
}

func TestCodexPoolModelValidityFailsClosedForIneligibleNonemptyPool(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"model not eligible"}`))
	}))
	defer srv.Close()

	globalDir := t.TempDir()
	writeCodexProbeToken(t, filepath.Join(globalDir, "codex-auth", "member.json"), "pool-access")
	pool := fmt.Sprintf(`{"version":1,"accounts":[{"path":"codex-auth/member.json","weight":1}]}`)
	if err := os.WriteFile(codexPoolPath(globalDir), []byte(pool), 0o600); err != nil {
		t.Fatal(err)
	}
	// A valid legacy token must not become a hidden fallback when the pool is
	// non-empty and its selected member is ineligible.
	writeCodexProbeToken(t, filepath.Join(globalDir, "codex-auth.json"), "legacy-access")

	status, detail := probeCodexModel("codex-pool", "gpt-test", srv.URL, globalDir, "")
	if status != probeAuthError || !strings.Contains(detail, "no eligible Codex pool account") {
		t.Fatalf("probe = %v, %q; want loud ineligible-pool failure", status, detail)
	}
	if calls != 1 {
		t.Fatalf("Responses calls = %d, want exactly the selected pool member", calls)
	}
}

func TestCodexPoolModelValidity_ZeroDisabledAndBlankUseLegacyProbe(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"object":"response","output":[]}`))
	}))
	defer srv.Close()
	dir := t.TempDir()
	writeCodexProbeToken(t, filepath.Join(dir, "codex-auth.json"), "legacy")
	raw := []byte(`{"version":1,"accounts":[{"path":"","weight":1},{"path":"codex-auth/disabled.json","weight":1,"enabled":false},{"path":"codex-auth/zero.json","weight":0}]}`)
	if err := os.WriteFile(codexPoolPath(dir), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	status, detail := probeCodexModel("codex-pool", "gpt-test", srv.URL, dir, "")
	if status != probeOK || detail != "" || calls != 1 {
		t.Fatalf("probe = %v, %q, calls=%d; want legacy fallback", status, detail, calls)
	}
}

func TestCodexModelValidityRequiresSelectedModel(t *testing.T) {
	status, detail := probeCodexModel("codex", "", "", t.TempDir(), "")
	if status != probeUnknown || !strings.Contains(detail, "selected Codex model is missing") {
		t.Fatalf("probe = %v, %q; want explicit missing-model state", status, detail)
	}
}
