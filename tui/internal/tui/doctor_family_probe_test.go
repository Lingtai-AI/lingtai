package tui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	status, _ := probeLLM(llmConfig{Provider: "codex", Model: "m", BaseURL: "http://127.0.0.1:1"})
	if status != probeOAuth {
		t.Fatalf("probeLLM(codex) = %v, want probeOAuth", status)
	}
}

// claude-code is judged by presence only — never a network or model call: a
// stored setup-token wins without consulting the CLI, else the local login
// (the stubbed non-billed `claude auth status`), else neither with the env
// name for the setup-token hint.
func TestProbeLLMClaudeCodeReportsActiveAuthPath(t *testing.T) {
	srv, seen := fakeFamilyEndpoint(t, http.StatusOK)

	calls := stubClaudeLogin(t, claudeCodeAuthInfo{LoggedIn: true, Email: "user@example.com"})
	status, detail := probeLLM(llmConfig{Provider: "claude-code", APIKey: "sk-ant-oat-token", APIKeyEnv: "CLAUDE_CODE_OAUTH_TOKEN", BaseURL: srv.URL})
	if status != probeClaudeToken || detail != "CLAUDE_CODE_OAUTH_TOKEN" {
		t.Fatalf("token: probeLLM = %v %q, want probeClaudeToken CLAUDE_CODE_OAUTH_TOKEN", status, detail)
	}
	if *calls != 0 {
		t.Fatalf("token present must not consult the CLI; probe calls = %d", *calls)
	}

	status, detail = probeLLM(llmConfig{Provider: "claude-code", APIKeyEnv: "CLAUDE_CODE_OAUTH_TOKEN", BaseURL: srv.URL})
	if status != probeClaudeLogin || detail != "user@example.com" {
		t.Fatalf("login: probeLLM = %v %q, want probeClaudeLogin with account", status, detail)
	}

	stubClaudeLogin(t, claudeCodeAuthInfo{})
	status, detail = probeLLM(llmConfig{Provider: "claude-code", BaseURL: srv.URL})
	if status != probeClaudeNoAuth || detail != "CLAUDE_CODE_OAUTH_TOKEN" {
		t.Fatalf("neither: probeLLM = %v %q, want probeClaudeNoAuth with the default token env", status, detail)
	}
	if got := seen(); len(got) != 0 {
		t.Fatalf("claude-code probe made network requests: %#v", got)
	}
}

// readLLMConfig resolves a claude-code agent's token from its api_key_env —
// or from CLAUDE_CODE_OAUTH_TOKEN when a legacy preset declares none — and
// runDoctor turns a missing credential into the setup-token hint.
func TestDoctorClaudeCodeTokenPresenceAndHint(t *testing.T) {
	stubClaudeLogin(t, claudeCodeAuthInfo{})
	orch := t.TempDir()
	envFile := filepath.Join(t.TempDir(), ".env")
	writeInit := func(apiKeyEnv string) {
		t.Helper()
		init := `{"env_file": "` + envFile + `", "manifest": {"llm": {"provider": "claude-code", "api_key_env": "` + apiKeyEnv + `"}}}`
		if err := os.WriteFile(filepath.Join(orch, "init.json"), []byte(init), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	os.Unsetenv("CLAUDE_CODE_OAUTH_TOKEN")

	writeInit("")
	if err := os.WriteFile(envFile, []byte("OTHER=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := readLLMConfig(orch)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKeyEnv != "CLAUDE_CODE_OAUTH_TOKEN" || cfg.APIKey != "" {
		t.Fatalf("legacy claude config = env %q key-present %v, want default env and no key", cfg.APIKeyEnv, cfg.APIKey != "")
	}
	if status, _ := probeLLM(cfg); status != probeClaudeNoAuth {
		t.Fatalf("no token, no login: status = %v, want probeClaudeNoAuth", status)
	}
	res := runDoctorLLMLinesForTest(cfg)
	if !strings.Contains(res, "claude setup-token") {
		t.Fatalf("doctor lines missing the setup-token hint:\n%s", res)
	}

	if err := os.WriteFile(envFile, []byte("CLAUDE_CODE_OAUTH_TOKEN=placeholder-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _ = readLLMConfig(orch)
	if status, detail := probeLLM(cfg); status != probeClaudeToken || detail != "CLAUDE_CODE_OAUTH_TOKEN" {
		t.Fatalf("stored token: status = %v %q, want probeClaudeToken", status, detail)
	}
	if res := runDoctorLLMLinesForTest(cfg); strings.Contains(res, "placeholder-token") {
		t.Fatalf("doctor output leaked the token:\n%s", res)
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

// runDoctorLLMLinesForTest renders the /doctor LLM lines for cfg as text.
func runDoctorLLMLinesForTest(cfg llmConfig) string {
	status, detail := probeLLM(cfg)
	var b strings.Builder
	for _, line := range llmProbeLines(status, detail, cfg.Provider, cfg.Model, "") {
		b.WriteString(line.Text + "\n")
	}
	return b.String()
}
