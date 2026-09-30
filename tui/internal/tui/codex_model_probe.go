package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// freshCodexAccessToken returns an access token guaranteed not to be stale
// for the eligibility probe. readCodexTokenFile has no expiry check, so a
// token that was valid at TUI startup but has since expired (the TUI only
// refreshes once, at launch, in ValidateCodexAuthOnStartup) would otherwise
// be sent to /responses as-is, draw a 401, and be misreported as an
// ineligible account/model — a false negative for a perfectly valid Codex
// credential. The actual expiry check, refresh, and write-back are owned by
// ensureFreshCodexTokens (oauth.go) — the same helper the startup validator
// uses — so this function only translates that outcome into a probeStatus:
//   - refreshed or already-fresh -> probeOK with the token to use.
//   - ErrCodexAuthRevoked (refresh_token rejected server-side) -> a hard
//     probeAuthError; a dead grant must still block Save.
//   - ErrCodexAuthTransient (network/5xx/timeout/write failure) ->
//     probeOverloaded. A known-stale token must never be sent to /responses
//     and have its resulting 401 misread as deterministic ineligibility.
func freshCodexAccessToken(path string, tokens CodexTokens) (string, probeStatus, string) {
	fresh, err := ensureFreshCodexTokens(path, tokens)
	switch err {
	case nil:
		return fresh.AccessToken, probeOK, ""
	case ErrCodexAuthRevoked:
		return "", probeAuthError, "Codex OAuth credential was revoked — re-authenticate"
	default: // ErrCodexAuthTransient
		return "", probeOverloaded, "Codex OAuth token refresh did not complete — try again"
	}
}

// probeCodexModel is the eligibility probe for the ChatGPT-backed Codex
// provider. It intentionally does not use the models catalogue: that only
// proves token reachability, not that this model/account can serve a real
// Responses request. The candidate is the token file the preset's
// codex_auth_path resolves to (the legacy token when unbound).
func probeCodexModel(model, baseURL, globalDir, authRef string) (probeStatus, string) {
	if strings.TrimSpace(model) == "" {
		return probeUnknown, "selected Codex model is missing"
	}
	if strings.TrimSpace(globalDir) == "" {
		return probeNoKey, "Codex credential directory is unavailable"
	}

	path := resolveCodexAuthPath(globalDir, authRef)
	tokens, ok := readCodexTokenFile(path)
	if !ok || strings.TrimSpace(tokens.AccessToken) == "" {
		return probeAuthError, "Codex OAuth credential is missing or unusable"
	}
	accessToken, status, detail := freshCodexAccessToken(path, tokens)
	if status != probeOK {
		return status, detail
	}
	return probeCodexResponses(path, accessToken, model, baseURL)
}

func probeCodexResponses(authPath, accessToken, model, baseURL string) (probeStatus, string) {
	base := strings.TrimRight(baseURL, "/")
	if base == "" {
		base = "https://chatgpt.com/backend-api/codex"
	}
	endpoint := base
	if !strings.HasSuffix(endpoint, "/responses") {
		endpoint += "/responses"
	}
	payload := map[string]interface{}{
		"model":        model,
		"instructions": "Reply with OK.",
		"input": []interface{}{map[string]interface{}{
			"role": "user",
			"content": []interface{}{map[string]interface{}{
				"type": "input_text",
				"text": "Reply with OK.",
			}},
		}},
		// The Codex backend's Responses path is served in streaming mode;
		// keep this request on the same wire contract as the runtime.
		"stream": true,
		"store":  false,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return probeUnknown, "could not construct Codex Responses request"
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return probeUnknown, "could not construct Codex Responses request"
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return probeNetworkError, "Codex Responses request failed"
	}
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return probeAuthError, "Codex account or model is not eligible"
	case resp.StatusCode == http.StatusTooManyRequests:
		return probeRateLimit, "Codex Responses request was rate-limited"
	case resp.StatusCode >= 500:
		return probeOverloaded, "Codex Responses service is unavailable"
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return probeUnknown, fmt.Sprintf("Codex Responses returned HTTP %d", resp.StatusCode)
	case len(bytes.TrimSpace(responseBody)) == 0:
		return probeEmptyResponse, "Codex Responses returned an empty response"
	}
	// A successful HTTP status is not enough if the endpoint returned an error
	// envelope. Accept the normal non-stream Responses envelope or completion
	// event, without retaining response text or any credential material.
	trimmed := bytes.TrimSpace(responseBody)
	if bytes.Contains(trimmed, []byte(`"error"`)) ||
		(!bytes.Contains(trimmed, []byte(`"response"`)) &&
			!bytes.Contains(trimmed, []byte(`response.completed`)) &&
			!bytes.Contains(trimmed, []byte(`"output"`))) {
		return probeUnknown, "Codex Responses returned no completed response"
	}
	_ = authPath
	return probeOK, ""
}
