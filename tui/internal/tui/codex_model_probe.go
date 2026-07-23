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

// probeCodexModel is the eligibility probe for the ChatGPT-backed Codex
// providers. It intentionally does not use the models catalogue: that only
// proves token reachability, not that this model/account can serve a real
// Responses request. Pool candidates are the same token paths the kernel can
// select, and a non-empty pool never falls back silently to the legacy token.
func probeCodexModel(provider, model, baseURL, globalDir, authRef string) (probeStatus, string) {
	if strings.TrimSpace(model) == "" {
		return probeUnknown, "selected Codex model is missing"
	}
	if strings.TrimSpace(globalDir) == "" {
		return probeNoKey, "Codex credential directory is unavailable"
	}

	paths := []string{}
	if provider == "codex-pool" || provider == "codex_pool" {
		pool, err := loadCodexPool(globalDir)
		if err != nil {
			return probeUnknown, "Codex pool is unreadable"
		}
		if pool.Models == nil {
			accounts := codexPoolAccountsRepresentable(pool.Accounts)
			if len(accounts) == 0 {
				paths = append(paths, legacyCodexAuthPath(globalDir))
			} else {
				for _, account := range accounts {
					paths = append(paths, resolveCodexPoolRef(globalDir, account.Path))
				}
			}
		} else {
			accounts, present := (*pool.Models)[model]
			if !present || len(accounts) == 0 {
				paths = append(paths, legacyCodexAuthPath(globalDir))
			} else if representable := codexPoolAccountsRepresentable(accounts); len(representable) == 0 {
				paths = append(paths, legacyCodexAuthPath(globalDir))
			} else {
				for _, account := range representable {
					paths = append(paths, resolveCodexPoolRef(globalDir, account.Path))
				}
			}
		}
	} else {
		paths = append(paths, resolveCodexAuthPath(globalDir, authRef))
	}
	if len(paths) == 0 {
		return probeAuthError, fmt.Sprintf("no eligible Codex account for model %s", model)
	}

	var lastStatus probeStatus = probeAuthError
	var lastDetail string
	for _, path := range paths {
		tokens, ok := readCodexTokenFile(path)
		if !ok || strings.TrimSpace(tokens.AccessToken) == "" {
			lastStatus, lastDetail = probeAuthError, "Codex OAuth credential is missing or unusable"
			continue
		}
		status, detail := probeCodexResponses(path, tokens.AccessToken, model, baseURL)
		if status == probeOK {
			return status, ""
		}
		lastStatus, lastDetail = status, detail
	}
	if provider == "codex-pool" || provider == "codex_pool" {
		return lastStatus, fmt.Sprintf("no eligible Codex pool account served model %s: %s", model, lastDetail)
	}
	return lastStatus, lastDetail
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
