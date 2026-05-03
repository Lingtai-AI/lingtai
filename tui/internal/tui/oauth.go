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
)

// ErrCodexAuthRevoked is returned by refreshCodexTokens when OpenAI's
// token endpoint rejects the stored refresh token (401/403). The user
// must re-OAuth via the wizard. Distinct from transient errors (network,
// 5xx, timeout) which leave the local tokens untouched.
var ErrCodexAuthRevoked = errors.New("codex refresh token rejected — re-authenticate")

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
type CodexOAuthDoneMsg struct {
	Tokens *CodexTokens
	Err    error
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
func startOAuthFlow() <-chan CodexOAuthDoneMsg {
	ch := make(chan CodexOAuthDoneMsg, 1)

	go func() {
		defer close(ch)

		verifier, challenge := generatePKCE()
		state := generateState()

		// Try default port (1455), then fallback (1457). Both are on
		// OpenAI's redirect_uri allowlist for this client_id; random
		// ports would be rejected.
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", defaultPort))
		if err != nil {
			listener, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", fallbackPort))
			if err != nil {
				ch <- CodexOAuthDoneMsg{Err: fmt.Errorf("listen on :%d or :%d: %w", defaultPort, fallbackPort, err)}
				return
			}
		}

		port := listener.Addr().(*net.TCPAddr).Port
		// Bind is on 127.0.0.1 but the redirect_uri must be "localhost"
		// — that's the exact string OpenAI's allowlist matches against.
		redirectURI := fmt.Sprintf("http://localhost:%d%s", port, callbackPath)

		// Channel for the authorization code from the callback handler.
		codeCh := make(chan string, 1)
		errCh := make(chan error, 1)

		mux := http.NewServeMux()
		mux.HandleFunc(callbackPath, func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()

			// Check for OAuth error response.
			if oauthErr := q.Get("error"); oauthErr != "" {
				desc := q.Get("error_description")
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprintf(w, "<html><body><h1>Login failed</h1><p>%s: %s</p></body></html>", html.EscapeString(oauthErr), html.EscapeString(desc))
				errCh <- fmt.Errorf("oauth error: %s: %s", oauthErr, desc)
				return
			}

			// Validate state.
			if q.Get("state") != state {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, "<html><body><h1>Login failed</h1><p>State mismatch.</p></body></html>")
				errCh <- fmt.Errorf("state mismatch")
				return
			}

			// Extract code.
			code := q.Get("code")
			if code == "" {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, "<html><body><h1>Login failed</h1><p>Missing authorization code.</p></body></html>")
				errCh <- fmt.Errorf("missing authorization code")
				return
			}

			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "<html><body><h1>Login successful!</h1><p>You can close this tab and return to the terminal.</p></body></html>")
			codeCh <- code
		})

		server := &http.Server{Handler: mux}

		// Serve in background.
		go func() {
			if serveErr := server.Serve(listener); serveErr != nil && serveErr != http.ErrServerClosed {
				errCh <- fmt.Errorf("http serve: %w", serveErr)
			}
		}()

		// Always shut down the server when done.
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = server.Shutdown(ctx)
		}()

		authURL := buildAuthorizeURL(redirectURI, challenge, state)

		openBrowser(authURL)

		// Wait for code, error, or timeout.
		timer := time.NewTimer(oauthTimeout)
		defer timer.Stop()

		var code string
		select {
		case code = <-codeCh:
			// got authorization code
		case e := <-errCh:
			ch <- CodexOAuthDoneMsg{Err: e}
			return
		case <-timer.C:
			ch <- CodexOAuthDoneMsg{Err: fmt.Errorf("oauth timed out after %s", oauthTimeout)}
			return
		}

		// Exchange code for tokens.
		tokens, err := exchangeCodeForTokens(codexTokenURL, code, verifier, redirectURI)
		if err != nil {
			ch <- CodexOAuthDoneMsg{Err: fmt.Errorf("token exchange: %w", err)}
			return
		}

		ch <- CodexOAuthDoneMsg{Tokens: tokens}
	}()

	return ch
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
