package tui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/preset"
)

// recordedRequest is one request seen by a fake provider endpoint.
type recordedRequest struct {
	Method, Path            string
	Authorization, APIKey   string
	AnthropicVersionPresent bool
}

// fakeFamilyEndpoint serves the models listing and one generation route and
// records every request. modelsStatus is the status of the models listing.
func fakeFamilyEndpoint(t *testing.T, modelsStatus int) (*httptest.Server, func() []recordedRequest) {
	t.Helper()
	var mu sync.Mutex
	var seen []recordedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, recordedRequest{
			Method:                  r.Method,
			Path:                    r.URL.Path,
			Authorization:           r.Header.Get("Authorization"),
			APIKey:                  r.Header.Get("x-api-key"),
			AnthropicVersionPresent: r.Header.Get("anthropic-version") != "",
		})
		mu.Unlock()
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/models"):
			w.WriteHeader(modelsStatus)
			_, _ = w.Write([]byte(`{"data":[]}`))
		case strings.HasSuffix(r.URL.Path, "/chat/completions"):
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hi"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
		case strings.HasSuffix(r.URL.Path, "/messages"):
			_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":1,"output_tokens":1}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() []recordedRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]recordedRequest(nil), seen...)
	}
}

func TestProbeLLMLoginFamiliesNeedNoNetwork(t *testing.T) {
	for _, provider := range []string{"codex", "claude-code"} {
		status, _ := probeLLM(llmConfig{Provider: provider, Model: "m", BaseURL: "http://127.0.0.1:1"})
		if status != probeOAuth {
			t.Fatalf("probeLLM(%s) = %v, want probeOAuth", provider, status)
		}
	}
}

// Retired vendor providers are rejected by the kernel, so /doctor reports
// them as unsupported instead of probing (this also replaces the old
// "unknown provider: openrouter" dead end).
func TestProbeLLMRetiredProviderIsUnsupported(t *testing.T) {
	for _, provider := range []string{"custom", "deepseek", "openrouter", "minimax", "gemini", ""} {
		status, detail := probeLLM(llmConfig{Provider: provider, Model: "m", APIKey: "k", BaseURL: "http://127.0.0.1:1"})
		if status != probeUnsupportedProvider {
			t.Fatalf("probeLLM(%q) = %v %q, want probeUnsupportedProvider", provider, status, detail)
		}
	}
}

func TestProbeLLMAPIKeyFamilyWithoutKeyIsNoKey(t *testing.T) {
	for _, provider := range []string{"openai", "anthropic"} {
		if status, _ := probeLLM(llmConfig{Provider: provider, Model: "m"}); status != probeNoKey {
			t.Fatalf("probeLLM(%s, no key) = %v, want probeNoKey", provider, status)
		}
	}
}

func TestProbeLLMOpenAIFamilyUsesBearerModelsThenChatCompletions(t *testing.T) {
	srv, seen := fakeFamilyEndpoint(t, http.StatusOK)
	status, detail := probeLLM(llmConfig{Provider: "openai", Model: "gpt-test", APIKey: "sk-test", BaseURL: srv.URL + "/v1"})
	if status != probeOK {
		t.Fatalf("probeLLM(openai) = %v %q, want probeOK", status, detail)
	}
	reqs := seen()
	if len(reqs) != 2 {
		t.Fatalf("requests = %+v, want models + chat/completions", reqs)
	}
	if reqs[0].Method != http.MethodGet || reqs[0].Path != "/v1/models" || reqs[0].Authorization != "Bearer sk-test" {
		t.Fatalf("first request = %+v, want GET /v1/models with Bearer key", reqs[0])
	}
	if reqs[1].Method != http.MethodPost || reqs[1].Path != "/v1/chat/completions" {
		t.Fatalf("second request = %+v, want POST {base}/chat/completions", reqs[1])
	}
}

// A Responses-wire endpoint (e.g. an account pool) need not serve Chat
// Completions, so the doctor must not report it broken for lacking one.
func TestProbeLLMOpenAIResponsesWireSkipsChatCompletions(t *testing.T) {
	srv, seen := fakeFamilyEndpoint(t, http.StatusOK)
	status, detail := probeLLM(llmConfig{Provider: "openai", Model: "gpt-test", APIKey: "sk-test", BaseURL: srv.URL + "/v1", WireAPI: "responses"})
	if status != probeOK {
		t.Fatalf("probeLLM(openai responses) = %v %q, want probeOK", status, detail)
	}
	for _, r := range seen() {
		if strings.HasSuffix(r.Path, "/chat/completions") {
			t.Fatalf("Responses-wire probe called chat/completions: %+v", seen())
		}
	}
}

func TestProbeLLMAnthropicFamilyUsesAPIKeyHeaders(t *testing.T) {
	srv, seen := fakeFamilyEndpoint(t, http.StatusOK)
	status, detail := probeLLM(llmConfig{Provider: "anthropic", Model: "claude-test", APIKey: "sk-ant", BaseURL: srv.URL})
	if status != probeOK {
		t.Fatalf("probeLLM(anthropic) = %v %q, want probeOK", status, detail)
	}
	reqs := seen()
	if len(reqs) != 2 {
		t.Fatalf("requests = %+v, want models + messages", reqs)
	}
	first := reqs[0]
	if first.Method != http.MethodGet || first.Path != "/v1/models" || first.APIKey != "sk-ant" || !first.AnthropicVersionPresent {
		t.Fatalf("first request = %+v, want GET /v1/models with x-api-key + anthropic-version", first)
	}
	if first.Authorization != "" {
		t.Fatalf("anthropic models listing must not send a Bearer header: %+v", first)
	}
	if reqs[1].Method != http.MethodPost || reqs[1].Path != "/v1/messages" {
		t.Fatalf("second request = %+v, want POST {base}/v1/messages", reqs[1])
	}
}

func TestProbeLLMRejectedKeyIsAuthError(t *testing.T) {
	for _, provider := range []string{"openai", "anthropic"} {
		srv, _ := fakeFamilyEndpoint(t, http.StatusUnauthorized)
		if status, _ := probeLLM(llmConfig{Provider: provider, Model: "m", APIKey: "bad", BaseURL: srv.URL}); status != probeAuthError {
			t.Fatalf("probeLLM(%s, 401) = %v, want probeAuthError", provider, status)
		}
	}
}

func TestFamilyModelsRequestDefaultsToOfficialEndpoints(t *testing.T) {
	url, headers := familyModelsRequest("openai", "", "k")
	if url != preset.OpenAIDefaultBaseURL+"/models" || headers["Authorization"] != "Bearer k" {
		t.Fatalf("openai default request = %q %v", url, headers)
	}
	url, headers = familyModelsRequest("anthropic", "", "k")
	if url != preset.AnthropicDefaultBaseURL+"/v1/models" || headers["x-api-key"] != "k" || headers["anthropic-version"] == "" {
		t.Fatalf("anthropic default request = %q %v", url, headers)
	}
	url, _ = familyModelsRequest("openai", "http://127.0.0.1:8080/v1/", "k")
	if url != "http://127.0.0.1:8080/v1/models" {
		t.Fatalf("custom openai base request = %q, want trailing slash trimmed", url)
	}
}
