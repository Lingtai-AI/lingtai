package tui

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestGeneratePKCE(t *testing.T) {
	verifier, challenge := generatePKCE()

	if verifier == "" {
		t.Fatal("verifier must not be empty")
	}
	if challenge == "" {
		t.Fatal("challenge must not be empty")
	}

	// Verify challenge == base64url(sha256(verifier)).
	h := sha256.Sum256([]byte(verifier))
	expected := base64.RawURLEncoding.EncodeToString(h[:])
	if challenge != expected {
		t.Fatalf("challenge mismatch:\n  got:  %s\n  want: %s", challenge, expected)
	}

	// Two calls should produce different values (randomness check).
	v2, _ := generatePKCE()
	if verifier == v2 {
		t.Fatal("two calls returned the same verifier — randomness failure")
	}
}

func TestGenerateState(t *testing.T) {
	state := generateState()

	// Base64url-encoded 32 bytes = 43 chars (no padding).
	// Matches the official Codex CLI's state format.
	if len(state) != 43 {
		t.Fatalf("state length = %d, want 43", len(state))
	}

	// Must be valid base64url (no padding).
	decoded, err := base64.RawURLEncoding.DecodeString(state)
	if err != nil {
		t.Fatalf("state is not valid base64url: %v", err)
	}
	if len(decoded) != 32 {
		t.Fatalf("decoded state length = %d, want 32", len(decoded))
	}

	// Two calls should differ.
	s2 := generateState()
	if state == s2 {
		t.Fatal("two calls returned the same state — randomness failure")
	}
}

// TestBuildAuthorizeURL pins every parameter OpenAI's auth-server allowlist
// validates against for the shared Codex client_id. Drift on any of these
// breaks login with a cryptic "Authentication Error" page — verified in the
// wild, see commit history. Bump these values in lockstep with the official
// openai/codex CLI; do not relax the test by switching to substring checks.
func TestBuildAuthorizeURL(t *testing.T) {
	const (
		redirect  = "http://localhost:1455/auth/callback"
		challenge = "test-challenge"
		state     = "test-state"
	)

	// forceLogin=false is the re-auth / first-account path: the URL must
	// carry NO prompt parameter so the browser reuses the active ChatGPT
	// session (the user is re-authenticating the same account).
	got := buildAuthorizeURL(redirect, challenge, state, false)

	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	if u.Scheme+"://"+u.Host+u.Path != "https://auth.openai.com/oauth/authorize" {
		t.Fatalf("base URL = %q, want https://auth.openai.com/oauth/authorize", u.Scheme+"://"+u.Host+u.Path)
	}

	q := u.Query()
	want := map[string]string{
		"response_type":              "code",
		"client_id":                  "app_EMoamEEZ73f0CkXaXp7hrann",
		"redirect_uri":               redirect,
		"scope":                      "openid profile email offline_access api.connectors.read api.connectors.invoke",
		"code_challenge":             challenge,
		"code_challenge_method":      "S256",
		"id_token_add_organizations": "true",
		"codex_cli_simplified_flow":  "true",
		"state":                      state,
		"originator":                 "codex_cli_rs",
	}
	for k, v := range want {
		if got := q.Get(k); got != v {
			t.Errorf("query param %q = %q, want %q", k, got, v)
		}
	}

	// No extra params we don't recognize — extras might be silently
	// rejected or cause future drift. With forceLogin=false there is in
	// particular NO prompt param.
	for k := range q {
		if _, ok := want[k]; !ok {
			t.Errorf("unexpected query param %q (= %q)", k, q.Get(k))
		}
	}
}

// TestBuildAuthorizeURL_ForceLogin pins the add-another-account fix: when
// forceLogin is true the authorize URL must carry prompt=login so OpenAI's
// auth server shows the account chooser / login page instead of silently
// reusing the browser's existing ChatGPT session. Without this, "Add another
// Codex account" re-adds the account already signed in (Jason's bug after
// PR #415). Every other allowlisted param must stay identical — prompt=login
// is purely additive.
func TestBuildAuthorizeURL_ForceLogin(t *testing.T) {
	const (
		redirect  = "http://localhost:1455/auth/callback"
		challenge = "test-challenge"
		state     = "test-state"
	)

	got := buildAuthorizeURL(redirect, challenge, state, true)
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}

	q := u.Query()
	if q.Get("prompt") != "login" {
		t.Errorf("prompt = %q, want %q (force-login must request the login page)", q.Get("prompt"), "login")
	}

	// The force-login URL must still carry every allowlisted param the
	// non-force path does — prompt is additive, not a replacement.
	base := map[string]string{
		"response_type":              "code",
		"client_id":                  "app_EMoamEEZ73f0CkXaXp7hrann",
		"redirect_uri":               redirect,
		"scope":                      "openid profile email offline_access api.connectors.read api.connectors.invoke",
		"code_challenge":             challenge,
		"code_challenge_method":      "S256",
		"id_token_add_organizations": "true",
		"codex_cli_simplified_flow":  "true",
		"state":                      state,
		"originator":                 "codex_cli_rs",
	}
	for k, v := range base {
		if got := q.Get(k); got != v {
			t.Errorf("query param %q = %q, want %q", k, got, v)
		}
	}

	// The only param beyond the base set may be prompt.
	for k := range q {
		if _, ok := base[k]; !ok && k != "prompt" {
			t.Errorf("unexpected query param %q (= %q)", k, q.Get(k))
		}
	}
}

func TestExchangeCodeForTokens(t *testing.T) {
	// Build a fake JWT with the OpenAI profile claim for the id_token.
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payloadObj := map[string]interface{}{
		"sub": "user-123",
		"https://api.openai.com/profile": map[string]string{
			"email": "test@example.com",
		},
	}
	payloadJSON, _ := json.Marshal(payloadObj)
	payload := base64.RawURLEncoding.EncodeToString(payloadJSON)
	fakeJWT := fmt.Sprintf("%s.%s.sig", header, payload)

	// Mock token server.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}

		// Verify all expected form params.
		checks := map[string]string{
			"grant_type":    "authorization_code",
			"client_id":     codexClientID,
			"code":          "test-auth-code",
			"code_verifier": "test-verifier",
			"redirect_uri":  "http://localhost:1455/auth/callback",
		}
		for k, want := range checks {
			got := r.FormValue(k)
			if got != want {
				t.Errorf("form param %s = %q, want %q", k, got, want)
			}
		}

		resp := map[string]interface{}{
			"access_token":  "acc-tok-123",
			"refresh_token": "ref-tok-456",
			"id_token":      fakeJWT,
			"expires_in":    3600,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	tokens, err := exchangeCodeForTokens(
		server.URL,
		"test-auth-code",
		"test-verifier",
		"http://localhost:1455/auth/callback",
	)
	if err != nil {
		t.Fatalf("exchangeCodeForTokens failed: %v", err)
	}

	if tokens.AccessToken != "acc-tok-123" {
		t.Errorf("AccessToken = %q, want %q", tokens.AccessToken, "acc-tok-123")
	}
	if tokens.RefreshToken != "ref-tok-456" {
		t.Errorf("RefreshToken = %q, want %q", tokens.RefreshToken, "ref-tok-456")
	}
	if tokens.Email != "test@example.com" {
		t.Errorf("Email = %q, want %q", tokens.Email, "test@example.com")
	}
	if tokens.ExpiresAt == 0 {
		t.Error("ExpiresAt should be non-zero")
	}
}

func TestExtractEmailFromJWT(t *testing.T) {
	tests := []struct {
		name string
		jwt  string
		want string
	}{
		{
			name: "valid jwt with email",
			jwt: func() string {
				h := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
				p := map[string]interface{}{
					"sub": "u-1",
					"https://api.openai.com/profile": map[string]string{
						"email": "alice@example.com",
					},
				}
				pj, _ := json.Marshal(p)
				return fmt.Sprintf("%s.%s.sig", h, base64.RawURLEncoding.EncodeToString(pj))
			}(),
			want: "alice@example.com",
		},
		{
			name: "missing profile claim",
			jwt: func() string {
				h := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
				p := map[string]interface{}{"sub": "u-2"}
				pj, _ := json.Marshal(p)
				return fmt.Sprintf("%s.%s.sig", h, base64.RawURLEncoding.EncodeToString(pj))
			}(),
			want: "",
		},
		{
			name: "not a jwt",
			jwt:  "not-a-jwt",
			want: "",
		},
		{
			name: "empty string",
			jwt:  "",
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractEmailFromJWT(tc.jwt)
			if got != tc.want {
				t.Errorf("extractEmailFromJWT() = %q, want %q", got, tc.want)
			}
		})
	}
}

// drainSession reads one message from a codexOAuthSession via waitCodexOAuthMsg.
// It blocks until a message arrives or the deadline is hit.
func drainSession(t *testing.T, session *codexOAuthSession, timeout time.Duration) interface{} {
	t.Helper()
	ch := make(chan interface{}, 1)
	go func() {
		msg, ok := <-session.msgs
		if !ok {
			ch <- CodexOAuthDoneMsg{Err: ErrCodexAuthCancelled}
			return
		}
		ch <- msg
	}()
	select {
	case msg := <-ch:
		return msg
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for OAuth session message after %s", timeout)
		return nil
	}
}

// TestStartOAuthFlow_Cancellable verifies that cancelling the supplied
// context tears down the listener and emits an ErrCodexAuthCancelled
// message with the caller's epoch echoed back. This is the load-bearing
// guarantee for the Del-cancel UX in FirstRunModel and LoginModel.
func TestStartOAuthFlow_Cancellable(t *testing.T) {
	const epoch uint64 = 42
	ctx, cancel := context.WithCancel(context.Background())
	session := startOAuthFlow(ctx, epoch, false)

	// Give the goroutine a moment to bind the listener, then cancel.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	// Drain messages until we get a terminal CodexOAuthDoneMsg.
	for {
		raw := drainSession(t, session, 3*time.Second)
		switch msg := raw.(type) {
		case CodexOAuthURLMsg:
			// Non-terminal: listener is ready; loop to the next message.
			if msg.Epoch != epoch {
				t.Errorf("URLMsg Epoch = %d, want %d", msg.Epoch, epoch)
			}
			continue
		case CodexOAuthDoneMsg:
			if msg.Epoch != epoch {
				t.Errorf("Epoch = %d, want %d", msg.Epoch, epoch)
			}
			if msg.Err == nil {
				t.Fatal("expected non-nil Err on cancellation")
			}
			if errors.Is(msg.Err, ErrCodexAuthCancelled) {
				return // success
			}
			// Listener bind failure — environment-dependent.
			t.Skipf("listener bind failed (likely ports 1455/1457 in use); cancellation path could not run: %v", msg.Err)
		}
	}
}

// TestStartOAuthFlow_LoopbackCallbackCompletesLegacyBrowserFlow verifies the
// same-machine browser OAuth path remains first-class: the localhost callback
// completes the legacy working flow without requiring terminal paste-back.
func TestStartOAuthFlow_LoopbackCallbackCompletesLegacyBrowserFlow(t *testing.T) {
	const epoch uint64 = 99
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	session := startOAuthFlow(ctx, epoch, false)
	first := drainSession(t, session, 3*time.Second)
	urlMsg, ok := first.(CodexOAuthURLMsg)
	if !ok {
		if done, ok := first.(CodexOAuthDoneMsg); ok && done.Err != nil {
			t.Skipf("listener bind failed (likely ports 1455/1457 in use): %v", done.Err)
		}
		t.Fatalf("expected CodexOAuthURLMsg, got %T", first)
	}

	authURL, err := url.Parse(urlMsg.AuthURL)
	if err != nil {
		t.Fatalf("parse AuthURL: %v", err)
	}
	state := authURL.Query().Get("state")
	if state == "" {
		t.Fatal("AuthURL did not include state")
	}

	resp, err := http.Get(urlMsg.RedirectURI + "?code=browser-code&state=" + url.QueryEscape(state))
	if err != nil {
		t.Fatalf("GET callback: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read callback body: %v", err)
	}
	if !strings.Contains(string(body), "Login successful") {
		t.Fatalf("callback body should confirm browser login success, got: %s", string(body))
	}

	for {
		raw := drainSession(t, session, 3*time.Second)
		switch msg := raw.(type) {
		case CodexOAuthDoneMsg:
			if msg.Err == nil && msg.Tokens == nil {
				t.Fatalf("terminal message had neither tokens nor error: %#v", msg)
			}
			return
		case CodexOAuthURLMsg:
			continue
		default:
			t.Fatalf("unexpected message after browser callback: %T", raw)
		}
	}
}

// TestStartOAuthFlow_EpochEchoed checks the error-emission path: when
// net.Listen fails (e.g. both ports in use), the returned message still
// carries the caller's epoch. That's the invariant the handler relies on
// to gate token-writes (Epoch must match m.codexLoginEpoch).
func TestStartOAuthFlow_EpochEchoed(t *testing.T) {
	l1, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", defaultPort))
	if err != nil {
		t.Skipf("cannot bind :%d to force listen-failure: %v", defaultPort, err)
	}
	defer l1.Close()
	l2, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", fallbackPort))
	if err != nil {
		t.Skipf("cannot bind :%d to force listen-failure: %v", fallbackPort, err)
	}
	defer l2.Close()

	const epoch uint64 = 7
	session := startOAuthFlow(context.Background(), epoch, false)
	raw := drainSession(t, session, 2*time.Second)
	msg, ok := raw.(CodexOAuthDoneMsg)
	if !ok {
		t.Fatalf("expected CodexOAuthDoneMsg, got %T", raw)
	}
	if msg.Epoch != epoch {
		t.Errorf("Epoch = %d, want %d", msg.Epoch, epoch)
	}
	if msg.Err == nil {
		t.Error("expected listen error, got nil Err")
	}
}

// TestWaitCodexOAuthMsg verifies that waitCodexOAuthMsg routes messages
// from the session channel as correct Bubble Tea message types.
func TestWaitCodexOAuthMsg(t *testing.T) {
	makeSession := func() (*codexOAuthSession, chan interface{}) {
		ch := make(chan interface{}, 2)
		s := &codexOAuthSession{msgs: ch}
		return s, ch
	}

	t.Run("CodexOAuthURLMsg passes through", func(t *testing.T) {
		s, ch := makeSession()
		ch <- CodexOAuthURLMsg{AuthURL: "https://example.com/auth", RedirectURI: "http://localhost:1455/auth/callback", Epoch: 1}
		cmd := waitCodexOAuthMsg(s)
		msg := cmd()
		urlMsg, ok := msg.(CodexOAuthURLMsg)
		if !ok {
			t.Fatalf("expected CodexOAuthURLMsg, got %T", msg)
		}
		if urlMsg.AuthURL != "https://example.com/auth" {
			t.Errorf("AuthURL = %q, want %q", urlMsg.AuthURL, "https://example.com/auth")
		}
	})

	t.Run("CodexOAuthDoneMsg passes through", func(t *testing.T) {
		s, ch := makeSession()
		ch <- CodexOAuthDoneMsg{Tokens: &CodexTokens{AccessToken: "tok"}, Epoch: 2}
		cmd := waitCodexOAuthMsg(s)
		msg := cmd()
		doneMsg, ok := msg.(CodexOAuthDoneMsg)
		if !ok {
			t.Fatalf("expected CodexOAuthDoneMsg, got %T", msg)
		}
		if doneMsg.Tokens == nil || doneMsg.Tokens.AccessToken != "tok" {
			t.Errorf("unexpected tokens: %+v", doneMsg.Tokens)
		}
	})

	t.Run("closed channel returns ErrCodexAuthCancelled", func(t *testing.T) {
		s, ch := makeSession()
		close(ch)
		cmd := waitCodexOAuthMsg(s)
		msg := cmd()
		doneMsg, ok := msg.(CodexOAuthDoneMsg)
		if !ok {
			t.Fatalf("expected CodexOAuthDoneMsg, got %T", msg)
		}
		if !errors.Is(doneMsg.Err, ErrCodexAuthCancelled) {
			t.Errorf("Err = %v, want ErrCodexAuthCancelled", doneMsg.Err)
		}
	})

	t.Run("nil session returns nil cmd", func(t *testing.T) {
		cmd := waitCodexOAuthMsg(nil)
		if cmd != nil {
			t.Error("expected nil cmd for nil session")
		}
	})
}

func TestRequestCodexDeviceCode(t *testing.T) {
	var sawClientID bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/accounts/deviceauth/usercode" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["client_id"] == codexClientID {
			sawClientID = true
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"device_auth_id":"dev-123","user_code":"ABCD-EFGH","interval":"1"}`)
	}))
	defer server.Close()

	got, err := requestCodexDeviceCode(context.Background(), server.Client(), server.URL, codexClientID)
	if err != nil {
		t.Fatalf("requestCodexDeviceCode: %v", err)
	}
	if !sawClientID {
		t.Fatal("request did not include codex client_id")
	}
	if got.VerificationURL != server.URL+"/codex/device" {
		t.Fatalf("VerificationURL = %q", got.VerificationURL)
	}
	if got.UserCode != "ABCD-EFGH" || got.DeviceAuthID != "dev-123" {
		t.Fatalf("unexpected device code: %#v", got)
	}
	if got.Interval != time.Second {
		t.Fatalf("Interval = %s, want 1s", got.Interval)
	}
}

func TestRequestCodexDeviceCodeRateLimitSurfaces(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/accounts/deviceauth/usercode" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, "Too Many Requests")
	}))
	defer server.Close()

	_, err := requestCodexDeviceCode(context.Background(), server.Client(), server.URL, codexClientID)
	if err == nil || !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "Too Many Requests") {
		t.Fatalf("err = %v, want 429 rate-limit detail", err)
	}
}

func TestPollCodexDeviceAuth(t *testing.T) {
	polls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/accounts/deviceauth/token":
			polls++
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode poll request: %v", err)
			}
			if body["device_auth_id"] != "dev-123" || body["user_code"] != "ABCD-EFGH" {
				t.Fatalf("poll body = %#v", body)
			}
			if polls == 1 {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"authorization_code":"auth-code","code_challenge":"challenge","code_verifier":"verifier"}`)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	got, err := pollCodexDeviceAuth(context.Background(), server.Client(), server.URL, codexDeviceCode{
		DeviceAuthID: "dev-123",
		UserCode:     "ABCD-EFGH",
		Interval:     time.Millisecond,
	})
	if err != nil {
		t.Fatalf("pollCodexDeviceAuth: %v", err)
	}
	if got.AuthorizationCode != "auth-code" || got.CodeVerifier != "verifier" {
		t.Fatalf("unexpected success response: %#v", got)
	}
	if polls != 2 {
		t.Fatalf("polls = %d, want 2", polls)
	}
}

func TestPollCodexDeviceAuthRateLimitSurfaces(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, "Too Many Requests")
	}))
	defer server.Close()

	_, err := pollCodexDeviceAuth(context.Background(), server.Client(), server.URL, codexDeviceCode{
		DeviceAuthID: "dev-123",
		UserCode:     "ABCD-EFGH",
		Interval:     time.Millisecond,
	})
	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("err = %v, want 429 rate-limit surface", err)
	}
}
