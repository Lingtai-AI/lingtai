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
	"strconv"
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
	codexClientID      = "app_EMoamEEZ73f0CkXaXp7hrann"
	codexAuthIssuerURL = "https://auth.openai.com"
	codexAuthURL       = codexAuthIssuerURL + "/oauth/authorize"
	codexTokenURL      = codexAuthIssuerURL + "/oauth/token"
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
	defaultPort       = 1455
	fallbackPort      = 1457
	oauthTimeout      = 5 * time.Minute
	deviceAuthTimeout = 15 * time.Minute
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

// CodexDeviceCodeMsg is emitted as soon as the device-code login has a
// verification URL and one-time code to show. The terminal remains on this
// state while the background goroutine polls OpenAI for completion.
type CodexDeviceCodeMsg struct {
	VerificationURL string
	UserCode        string
	Epoch           uint64
}

// codexOAuthSession carries the login message stream for both browser OAuth
// and device-code login flows.
type codexOAuthSession struct {
	msgs <-chan interface{}
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
		case CodexDeviceCodeMsg:
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

// startOAuthFlow initiates the Codex browser OAuth PKCE flow.
// It starts a localhost HTTP server, opens the browser, completes when the
// browser reaches the allowlisted localhost callback, exchanges the code for
// tokens, and returns the result on the channel. This is the legacy same-machine
// path: no terminal paste-back is required.
//
// The flow honours ctx — cancellation tears down the listener and emits
// CodexOAuthDoneMsg{Err: ErrCodexAuthCancelled, Epoch: epoch} promptly so
// the caller can stop showing the "logging in" state. epoch is echoed
// back on the message so a handler can ignore late callbacks from a
// cancelled session (see FirstRunModel.codexLoginEpoch).
//
// forceLogin is forwarded to buildAuthorizeURL: pass true only when the user
// is adding a NEW Codex account (so OpenAI shows the login page instead of
// reusing the active session), false for first/bootstrap login and existing-
// account re-auth.
func startOAuthFlow(ctx context.Context, epoch uint64, forceLogin bool) *codexOAuthSession {
	ch := make(chan interface{}, 2)

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

		codeCh := make(chan string, 1)
		errCh := make(chan error, 1)

		mux := http.NewServeMux()
		mux.HandleFunc(callbackPath, func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()

			w.Header().Set("Content-Type", "text/html; charset=utf-8")

			if oauthErr := q.Get("error"); oauthErr != "" {
				desc := q.Get("error_description")
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprintf(w, "<html><body><h1>Login failed</h1><p>%s: %s</p></body></html>", html.EscapeString(oauthErr), html.EscapeString(desc))
				select {
				case errCh <- fmt.Errorf("oauth error: %s: %s", oauthErr, desc):
				default:
				}
				return
			}

			if q.Get("state") != state {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, "<html><body><h1>Login failed</h1><p>State mismatch.</p></body></html>")
				select {
				case errCh <- fmt.Errorf("state mismatch"):
				default:
				}
				return
			}

			code := q.Get("code")
			if code == "" {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, "<html><body><h1>Login failed</h1><p>Missing authorization code.</p></body></html>")
				select {
				case errCh <- fmt.Errorf("missing authorization code"):
				default:
				}
				return
			}

			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "<html><body><h1>Login successful!</h1><p>You can close this tab and return to the terminal.</p></body></html>")
			select {
			case codeCh <- code:
			default:
			}
		})
		server := &http.Server{Handler: mux}

		// Serve in background.
		go func() {
			if serveErr := server.Serve(listener); serveErr != nil && serveErr != http.ErrServerClosed {
				select {
				case errCh <- fmt.Errorf("http serve: %w", serveErr):
				default:
				}
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

		authURL := buildAuthorizeURL(redirectURI, challenge, state, forceLogin)

		ch <- CodexOAuthURLMsg{AuthURL: authURL, RedirectURI: redirectURI, Epoch: epoch}
		openBrowser(authURL)

		// Wait for browser callback, server error, timeout, or cancellation.
		timer := time.NewTimer(oauthTimeout)
		defer timer.Stop()

		var code string
		select {
		case code = <-codeCh:
			// got authorization code
		case e := <-errCh:
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

	return &codexOAuthSession{msgs: ch}
}

// codexDeviceCode carries the server-issued one-time device login state.
type codexDeviceCode struct {
	VerificationURL string
	UserCode        string
	DeviceAuthID    string
	Interval        time.Duration
}

type codexDeviceCodeSuccess struct {
	AuthorizationCode string `json:"authorization_code"`
	CodeChallenge     string `json:"code_challenge"`
	CodeVerifier      string `json:"code_verifier"`
}

// startDeviceAuthFlow initiates the official Codex device-code login flow.
// It shows the user a verification URL + one-time code, polls for approval,
// then exchanges the issued authorization_code for the same token bundle used
// by the browser OAuth path.
func startDeviceAuthFlow(ctx context.Context, epoch uint64) *codexOAuthSession {
	ch := make(chan interface{}, 2)

	go func() {
		defer close(ch)
		emitDone := func(msg CodexOAuthDoneMsg) {
			msg.Epoch = epoch
			ch <- msg
		}

		client := &http.Client{Timeout: 15 * time.Second}
		deviceCode, err := requestCodexDeviceCode(ctx, client, codexAuthIssuerURL, codexClientID)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, ErrCodexAuthCancelled) {
				emitDone(CodexOAuthDoneMsg{Err: ErrCodexAuthCancelled})
			} else {
				emitDone(CodexOAuthDoneMsg{Err: fmt.Errorf("device code request: %w", err)})
			}
			return
		}

		ch <- CodexDeviceCodeMsg{VerificationURL: deviceCode.VerificationURL, UserCode: deviceCode.UserCode, Epoch: epoch}

		tokens, err := completeCodexDeviceAuth(ctx, client, codexAuthIssuerURL, codexTokenURL, deviceCode)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, ErrCodexAuthCancelled) {
				emitDone(CodexOAuthDoneMsg{Err: ErrCodexAuthCancelled})
			} else {
				emitDone(CodexOAuthDoneMsg{Err: fmt.Errorf("device auth: %w", err)})
			}
			return
		}

		emitDone(CodexOAuthDoneMsg{Tokens: tokens})
	}()

	return &codexOAuthSession{msgs: ch}
}

func requestCodexDeviceCode(ctx context.Context, client *http.Client, issuerURL, clientID string) (codexDeviceCode, error) {
	apiBaseURL := strings.TrimRight(issuerURL, "/") + "/api/accounts"
	body, err := json.Marshal(map[string]string{"client_id": clientID})
	if err != nil {
		return codexDeviceCode{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBaseURL+"/deviceauth/usercode", strings.NewReader(string(body)))
	if err != nil {
		return codexDeviceCode{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return codexDeviceCode{}, ErrCodexAuthCancelled
		}
		return codexDeviceCode{}, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return codexDeviceCode{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusNotFound {
			return codexDeviceCode{}, fmt.Errorf("device code login is not enabled for this Codex server; use browser OAuth")
		}
		return codexDeviceCode{}, fmt.Errorf("device code request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var raw struct {
		DeviceAuthID string          `json:"device_auth_id"`
		UserCode     string          `json:"user_code"`
		UserCodeAlt  string          `json:"usercode"`
		Interval     json.RawMessage `json:"interval"`
	}
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return codexDeviceCode{}, err
	}
	if raw.UserCode == "" {
		raw.UserCode = raw.UserCodeAlt
	}
	if raw.DeviceAuthID == "" || raw.UserCode == "" {
		return codexDeviceCode{}, fmt.Errorf("device code response missing device_auth_id or user_code")
	}
	interval := parseDeviceAuthInterval(raw.Interval)
	return codexDeviceCode{
		VerificationURL: strings.TrimRight(issuerURL, "/") + "/codex/device",
		UserCode:        raw.UserCode,
		DeviceAuthID:    raw.DeviceAuthID,
		Interval:        interval,
	}, nil
}

func parseDeviceAuthInterval(raw json.RawMessage) time.Duration {
	if len(raw) == 0 || string(raw) == "null" {
		return 5 * time.Second
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if n, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil && n > 0 {
			return time.Duration(n * float64(time.Second))
		}
	}
	var n float64
	if err := json.Unmarshal(raw, &n); err == nil && n > 0 {
		return time.Duration(n * float64(time.Second))
	}
	return 5 * time.Second
}

func completeCodexDeviceAuth(ctx context.Context, client *http.Client, issuerURL, tokenURL string, deviceCode codexDeviceCode) (*CodexTokens, error) {
	pollCtx, cancel := context.WithTimeout(ctx, deviceAuthTimeout)
	defer cancel()

	codeResp, err := pollCodexDeviceAuth(pollCtx, client, issuerURL, deviceCode)
	if err != nil {
		return nil, err
	}
	redirectURI := strings.TrimRight(issuerURL, "/") + "/deviceauth/callback"
	return exchangeCodeForTokens(tokenURL, codeResp.AuthorizationCode, codeResp.CodeVerifier, redirectURI)
}

func pollCodexDeviceAuth(ctx context.Context, client *http.Client, issuerURL string, deviceCode codexDeviceCode) (codexDeviceCodeSuccess, error) {
	apiBaseURL := strings.TrimRight(issuerURL, "/") + "/api/accounts"
	interval := deviceCode.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}

	for {
		body, err := json.Marshal(map[string]string{
			"device_auth_id": deviceCode.DeviceAuthID,
			"user_code":      deviceCode.UserCode,
		})
		if err != nil {
			return codexDeviceCodeSuccess{}, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBaseURL+"/deviceauth/token", strings.NewReader(string(body)))
		if err != nil {
			return codexDeviceCodeSuccess{}, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return codexDeviceCodeSuccess{}, fmt.Errorf("device auth timed out after %s", deviceAuthTimeout)
			}
			if errors.Is(err, context.Canceled) {
				return codexDeviceCodeSuccess{}, ErrCodexAuthCancelled
			}
			return codexDeviceCodeSuccess{}, err
		}
		respBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return codexDeviceCodeSuccess{}, readErr
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			var out codexDeviceCodeSuccess
			if err := json.Unmarshal(respBody, &out); err != nil {
				return codexDeviceCodeSuccess{}, err
			}
			if out.AuthorizationCode == "" || out.CodeVerifier == "" {
				return codexDeviceCodeSuccess{}, fmt.Errorf("device auth response missing authorization_code or code_verifier")
			}
			return out, nil
		}

		// Official Codex treats 403/404 as "not approved yet" until timeout.
		if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusNotFound {
			return codexDeviceCodeSuccess{}, fmt.Errorf("device auth failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
		}

		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return codexDeviceCodeSuccess{}, fmt.Errorf("device auth timed out after %s", deviceAuthTimeout)
			}
			return codexDeviceCodeSuccess{}, ErrCodexAuthCancelled
		case <-time.After(interval):
		}
	}
}

// buildAuthorizeURL assembles the OAuth authorize URL with the parameter
// set OpenAI's allowlist requires for the shared Codex client_id. Every
// param here is load-bearing — see oauth_test.go for the rationale.
//
// forceLogin controls account selection. When false (re-auth of an existing
// account, or the first/bootstrap login) the URL carries no prompt param, so
// the browser silently reuses any active ChatGPT session — the right thing
// when the user is re-authenticating the account they're already signed into.
// When true (the "Add another Codex account" path) we add prompt=login so the
// OpenAI auth server shows the login page instead of reusing the existing
// session; without it the second add silently re-adds the same account
// (Jason's post-#415 bug). prompt=login is the standard OIDC parameter and
// is purely additive to the allowlisted set.
func buildAuthorizeURL(redirectURI, challenge, state string, forceLogin bool) string {
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
	if forceLogin {
		params.Set("prompt", "login")
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
