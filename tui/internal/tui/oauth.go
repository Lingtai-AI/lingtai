package tui

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// ErrCodexAuthRevoked is returned by refreshCodexTokens when OpenAI's
// token endpoint rejects the stored refresh token (401/403). The user
// must re-OAuth via the wizard. Distinct from transient errors (network,
// 5xx, timeout) which leave the local tokens untouched.
var ErrCodexAuthRevoked = errors.New("codex refresh token rejected — re-authenticate")

// ErrCodexAuthCancelled is delivered in CodexOAuthDoneMsg.Err when the
// caller cancels the OAuth flow via the supplied context (user pressed
// Del/Backspace on the Codex 凭据 row, or navigated away). Handlers use
// this to distinguish a user-initiated abort from a real failure.
var ErrCodexAuthCancelled = errors.New("codex oauth cancelled")

const (
	codexClientID = "app_EMoamEEZ73f0CkXaXp7hrann"
	codexAuthURL  = "https://auth.openai.com/oauth/authorize"
	codexTokenURL = "https://auth.openai.com/oauth/token"
	// codexScope must include the connector scopes — without them the
	// authorize page rejects the request immediately. Matches the official
	// Codex CLI scope string.
	codexScope = "openid profile email offline_access api.connectors.read api.connectors.invoke"
	// codexOriginator must match a value OpenAI's auth server accepts for
	// this client_id. The shared public client_id (used by Codex CLI,
	// Hermes, OpenClaw) is tied to an originator allowlist on the server
	// side; sending an unrecognized originator (e.g. "lingtai") causes the
	// authorize page to reject the request immediately. Use the official
	// Codex CLI's originator string.
	codexOriginator = "codex_cli_rs"
	callbackPath    = "/auth/callback"
	// OpenAI's allowlist registers exactly these two redirect URIs for
	// app_EMoamEEZ73f0CkXaXp7hrann: http://localhost:1455/auth/callback
	// and http://localhost:1457/auth/callback. Random ephemeral ports
	// would not match the allowlist and the flow fails immediately.
	defaultPort  = 1455
	fallbackPort = 1457
	oauthTimeout = 5 * time.Minute
)

// CodexTokens holds the token bundle written to disk.
type CodexTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"`
	Email        string `json:"email"`
}

// CodexOAuthDoneMsg is the Bubble Tea message emitted when OAuth completes.
// Epoch carries the caller-assigned session id passed to startOAuthFlow so
// handlers can drop late callbacks from a cancelled flow (the model bumps
// its epoch on cancel; tokens from a stale epoch must not overwrite
// codex-auth.json).
type CodexOAuthDoneMsg struct {
	Tokens *CodexTokens
	Err    error
	Epoch  uint64
}

// CodexOAuthURLMsg is emitted as soon as the local listener is ready and
// the authorization URL has been built. The UI shows AuthURL so users on a
// different computer can copy/open it manually instead of relying on the
// local browser launch. RedirectURI is shown as the expected callback host.
type CodexOAuthURLMsg struct {
	AuthURL     string
	RedirectURI string
	Epoch       uint64
}

// codexOAuthSession carries the OAuth message stream plus a manual callback
// input channel. SubmitCallback accepts the full localhost callback URL copied
// from the browser address bar, a raw query string containing code/state, or a
// raw authorization code. This is the remote-browser fallback for setups where
// the TUI and the human's browser are not on the same machine.
type codexOAuthSession struct {
	msgs     <-chan interface{}
	manualCh chan string
}

func (s *codexOAuthSession) SubmitCallback(raw string) {
	if s == nil || s.manualCh == nil {
		return
	}
	select {
	case s.manualCh <- raw:
	default:
	}
}

func waitCodexOAuthMsg(session *codexOAuthSession) tea.Cmd {
	if session == nil || session.msgs == nil {
		return nil
	}
	return func() tea.Msg {
		msg, ok := <-session.msgs
		if !ok {
			return CodexOAuthDoneMsg{Err: ErrCodexAuthCancelled}
		}
		switch m := msg.(type) {
		case CodexOAuthURLMsg:
			return m
		case CodexOAuthDoneMsg:
			return m
		default:
			return CodexOAuthDoneMsg{Err: fmt.Errorf("unknown OAuth message %T", msg)}
		}
	}
}

// generatePKCE creates a PKCE verifier and challenge pair.
// The verifier is 32 random bytes base64url-encoded (no padding).
// The challenge is the SHA-256 hash of the verifier, base64url-encoded (no padding).
func generatePKCE() (verifier, challenge string) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	verifier = base64.RawURLEncoding.EncodeToString(buf)

	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])
	return verifier, challenge
}

// generateState creates a 43-character base64url string from 32 random bytes.
// Matches the official Codex CLI's state format (base64url, no padding).
func generateState() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

// startOAuthFlow initiates the Codex OAuth PKCE flow.
// It starts a local HTTP server, opens the browser, waits for the callback,
// exchanges the code for tokens, and returns the result on the channel.
//
// The flow honours ctx — cancellation tears down the listener and emits
// CodexOAuthDoneMsg{Err: ErrCodexAuthCancelled, Epoch: epoch} promptly so
// the caller can stop showing the "logging in" state. epoch is echoed
// back on the message so a handler can ignore late callbacks from a
// cancelled session (see FirstRunModel.codexLoginEpoch).
func startOAuthFlow(ctx context.Context, epoch uint64) *codexOAuthSession {
	ch := make(chan interface{}, 2)
	manualCh := make(chan string, 1)

	go func() {
		defer close(ch)

		// emitDone sends a terminal result tagged with this session's epoch.
		// The message channel also carries one non-terminal CodexOAuthURLMsg.
		emitDone := func(msg CodexOAuthDoneMsg) {
			msg.Epoch = epoch
			ch <- msg
		}

		verifier, challenge := generatePKCE()
		state := generateState()

		// Try default port (1455), then fallback (1457). Both are on
		// OpenAI's redirect_uri allowlist for this client_id; random
		// ports would be rejected.
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", defaultPort))
		if err != nil {
			listener, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", fallbackPort))
			if err != nil {
				emitDone(CodexOAuthDoneMsg{Err: fmt.Errorf("listen on :%d or :%d: %w", defaultPort, fallbackPort, err)})
				return
			}
		}

		port := listener.Addr().(*net.TCPAddr).Port
		// Bind is on 127.0.0.1 but the redirect_uri must be "localhost"
		// — that's the exact string OpenAI's allowlist matches against.
		redirectURI := fmt.Sprintf("http://localhost:%d%s", port, callbackPath)

		// The loopback callback only renders instructions. It must not
		// complete OAuth under the hood: the user has to copy the code
		// or callback URL back into the TUI textarea so success is visible.
		serveErrCh := make(chan error, 1)

		mux := http.NewServeMux()
		mux.HandleFunc(callbackPath, func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			callbackURL := "http://" + r.Host + r.URL.RequestURI()
			if r.Host == "" {
				callbackURL = r.URL.String()
			}

			w.Header().Set("Content-Type", "text/html; charset=utf-8")

			if oauthErr := q.Get("error"); oauthErr != "" {
				desc := q.Get("error_description")
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprintf(w, "<html><body><h1>Login needs terminal confirmation</h1><p>OAuth returned an error: %s %s</p><p>Copy this URL back to the terminal so it can show the error.</p><pre>%s</pre></body></html>", html.EscapeString(oauthErr), html.EscapeString(desc), html.EscapeString(callbackURL))
				return
			}

			code := q.Get("code")
			if q.Get("state") != state || code == "" {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, "<html><body><h1>Login needs terminal confirmation</h1><p>The callback is missing a code or has the wrong state.</p><p>Copy this URL back to the terminal so it can show the exact error.</p><pre>")
				fmt.Fprint(w, html.EscapeString(callbackURL))
				fmt.Fprint(w, "</pre></body></html>")
				return
			}

			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "<html><body><h1>Authorization received</h1><p>Return to the terminal and paste this code into the OAuth textarea. Nothing has been completed in the terminal yet.</p><pre>%s</pre><p>You may also paste the full callback URL from the address bar.</p></body></html>", html.EscapeString(code))
		})
		server := &http.Server{Handler: mux}

		// Serve in background.
		go func() {
			if serveErr := server.Serve(listener); serveErr != nil && serveErr != http.ErrServerClosed {
				serveErrCh <- fmt.Errorf("http serve: %w", serveErr)
			}
		}()

		// Always shut down the server when done. The 2s grace lets any
		// in-flight callback finish its response; on cancel the parent
		// ctx is already Done, so we use a fresh background ctx here.
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = server.Shutdown(shutdownCtx)
		}()

		authURL := buildAuthorizeURL(redirectURI, challenge, state)

		ch <- CodexOAuthURLMsg{AuthURL: authURL, RedirectURI: redirectURI, Epoch: epoch}
		openBrowser(authURL)

		// Wait for explicit textarea input, server error, timeout, or cancellation.
		timer := time.NewTimer(oauthTimeout)
		defer timer.Stop()

		var code string
		select {
		case raw := <-manualCh:
			manualCode, err := extractOAuthCode(raw, state)
			if err != nil {
				emitDone(CodexOAuthDoneMsg{Err: err})
				return
			}
			code = manualCode
		case e := <-serveErrCh:
			emitDone(CodexOAuthDoneMsg{Err: e})
			return
		case <-timer.C:
			emitDone(CodexOAuthDoneMsg{Err: fmt.Errorf("oauth timed out after %s", oauthTimeout)})
			return
		case <-ctx.Done():
			emitDone(CodexOAuthDoneMsg{Err: ErrCodexAuthCancelled})
			return
		}

		// Exchange code for tokens. Also honour cancellation here —
		// the user may Del between the browser callback and the token
		// POST (network slow, user changed their mind).
		select {
		case <-ctx.Done():
			emitDone(CodexOAuthDoneMsg{Err: ErrCodexAuthCancelled})
			return
		default:
		}
		tokens, err := exchangeCodeForTokens(codexTokenURL, code, verifier, redirectURI)
		if err != nil {
			emitDone(CodexOAuthDoneMsg{Err: fmt.Errorf("token exchange: %w", err)})
			return
		}

		emitDone(CodexOAuthDoneMsg{Tokens: tokens})
	}()

	return &codexOAuthSession{msgs: ch, manualCh: manualCh}
}

// extractOAuthCode accepts the manual fallback input from the user. The browser
// address bar may contain the full localhost callback URL, or the user may paste
// only the raw query string. State is still validated exactly as the loopback
// handler validates it, so a stale or unrelated callback cannot be exchanged.
func extractOAuthCode(raw, wantState string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("missing OAuth callback URL or code")
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		u, err := url.Parse(raw)
		if err != nil {
			return "", fmt.Errorf("parse OAuth callback URL: %w", err)
		}
		return extractOAuthCodeFromValues(u.Query(), wantState)
	}
	if strings.Contains(raw, "=") {
		q, err := url.ParseQuery(strings.TrimPrefix(raw, "?"))
		if err != nil {
			return "", fmt.Errorf("parse OAuth callback query: %w", err)
		}
		return extractOAuthCodeFromValues(q, wantState)
	}
	return raw, nil
}

func extractOAuthCodeFromValues(q url.Values, wantState string) (string, error) {
	if oauthErr := q.Get("error"); oauthErr != "" {
		desc := q.Get("error_description")
		return "", fmt.Errorf("oauth error: %s: %s", oauthErr, desc)
	}
	if got := q.Get("state"); got != "" && got != wantState {
		return "", fmt.Errorf("state mismatch")
	}
	code := q.Get("code")
	if code == "" {
		return "", fmt.Errorf("missing authorization code")
	}
	return code, nil
}

// buildAuthorizeURL assembles the OAuth authorize URL with the parameter
// set OpenAI's allowlist requires for the shared Codex client_id. Every
// param here is load-bearing — see oauth_test.go for the rationale.
func buildAuthorizeURL(redirectURI, challenge, state string) string {
	params := url.Values{
		"response_type":              {"code"},
		"client_id":                  {codexClientID},
		"redirect_uri":               {redirectURI},
		"scope":                      {codexScope},
		"code_challenge":             {challenge},
		"code_challenge_method":      {"S256"},
		"id_token_add_organizations": {"true"},
		"codex_cli_simplified_flow":  {"true"},
		"state":                      {state},
		"originator":                 {codexOriginator},
	}
	return codexAuthURL + "?" + params.Encode()
}

// exchangeCodeForTokens POSTs to the token endpoint and returns parsed tokens.
// tokenURL is parameterized so tests can substitute a mock server.
func exchangeCodeForTokens(tokenURL, code, verifier, redirectURI string) (*CodexTokens, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {codexClientID},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {redirectURI},
	}

	resp, err := http.PostForm(tokenURL, form)
	if err != nil {
		return nil, fmt.Errorf("POST token endpoint: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var raw struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}

	email := extractEmailFromJWT(raw.IDToken)

	return &CodexTokens{
		AccessToken:  raw.AccessToken,
		RefreshToken: raw.RefreshToken,
		ExpiresAt:    time.Now().Unix() + raw.ExpiresIn,
		Email:        email,
	}, nil
}

// extractEmailFromJWT extracts the email from the OpenAI ID token.
// It looks for the "https://api.openai.com/profile" claim in the JWT payload.
// Returns empty string on any error.
func extractEmailFromJWT(jwt string) string {
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		return ""
	}

	// Base64url decode the payload (index 1). Add padding if needed.
	payload := parts[1]
	if m := len(payload) % 4; m != 0 {
		payload += strings.Repeat("=", 4-m)
	}

	decoded, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return ""
	}

	var claims map[string]json.RawMessage
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return ""
	}

	profileRaw, ok := claims["https://api.openai.com/profile"]
	if !ok {
		return ""
	}

	var profile struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(profileRaw, &profile); err != nil {
		return ""
	}
	return profile.Email
}

// openBrowser is defined in app.go — reused here for the OAuth flow.

// refreshCodexTokens exchanges a refresh_token for a fresh access token
// against auth.openai.com. Returns the merged token bundle (preserving
// fields like email that the refresh response doesn't include — caller
// supplies them via existing). Returns ErrCodexAuthRevoked on 401/403
// (grant invalidated server-side; user must re-OAuth). Other errors are
// transient (network/5xx/timeout) — caller should leave local tokens
// untouched.
func refreshCodexTokens(refreshToken string, existing CodexTokens) (*CodexTokens, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {codexClientID},
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.PostForm(codexTokenURL, form)
	if err != nil {
		return nil, fmt.Errorf("POST token endpoint: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, ErrCodexAuthRevoked
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var raw struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}

	merged := existing
	merged.AccessToken = raw.AccessToken
	if raw.RefreshToken != "" {
		merged.RefreshToken = raw.RefreshToken
	}
	merged.ExpiresAt = time.Now().Unix() + raw.ExpiresIn
	if email := extractEmailFromJWT(raw.IDToken); email != "" {
		merged.Email = email
	}
	return &merged, nil
}
