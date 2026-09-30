package tui

import (
	"net/http"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/anthropics/lingtai-tui/internal/preset"
)

func familyPreset(name, provider, apiKeyEnv, baseURL string) preset.Preset {
	llm := map[string]interface{}{
		"provider":    provider,
		"model":       "test-model",
		"api_key_env": apiKeyEnv,
	}
	if baseURL != "" {
		llm["base_url"] = baseURL
	}
	return preset.Preset{
		Name:        name,
		Description: preset.PresetDescription{Summary: "credential health test preset"},
		Manifest:    map[string]interface{}{"llm": llm},
	}
}

// Config.Keys is keyed by env-var name, so the endpoint for a stored key comes
// from the presets that declare that env var — never from the env-var name
// itself (the old providerBaseURL(envName) lookup matched nothing, so every
// API-key row reported "no endpoint").
func TestAPIKeyProbeTargetDerivesEndpointFromPresets(t *testing.T) {
	presets := []preset.Preset{
		familyPreset("legacy", "deepseek", "SHARED_API_KEY", "https://legacy.example"),
		familyPreset("gateway", "openai", "SHARED_API_KEY", "https://gw.example/v1"),
		familyPreset("claude-api", "anthropic", "ANTHROPIC_1_API_KEY", ""),
		familyPreset("codex", "codex", "", "https://chatgpt.com/backend-api/codex"),
	}
	cases := []struct {
		env, family, base string
		ok                bool
	}{
		// A retired-provider preset using the same slot is skipped.
		{"SHARED_API_KEY", "openai", "https://gw.example/v1", true},
		{"ANTHROPIC_1_API_KEY", "anthropic", "", true},
		{"ORPHAN_API_KEY", "", "", false},
	}
	for _, tc := range cases {
		family, base, ok := apiKeyProbeTarget(tc.env, presets)
		if family != tc.family || base != tc.base || ok != tc.ok {
			t.Fatalf("apiKeyProbeTarget(%s) = %q %q %v, want %q %q %v", tc.env, family, base, ok, tc.family, tc.base, tc.ok)
		}
	}
}

func TestNewLoginModelProbesStoredKeysAgainstTheirPresetEndpoint(t *testing.T) {
	tempTestHome(t)
	globalDir := t.TempDir()
	if err := preset.Save(familyPreset("gateway-1", "openai", "GATEWAY_1_API_KEY", "https://gw.example/v1")); err != nil {
		t.Fatalf("save preset: %v", err)
	}
	if err := config.SaveConfig(globalDir, config.Config{Keys: map[string]string{
		"GATEWAY_1_API_KEY": "sk-gateway-key",
		"ORPHAN_API_KEY":    "sk-orphan-key",
	}}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	m := NewLoginModel("", globalDir)
	byEnv := map[string]loginEntry{}
	for _, e := range m.entries {
		byEnv[e.Provider] = e
	}
	gw, ok := byEnv["GATEWAY_1_API_KEY"]
	if !ok || gw.Family != "openai" || gw.BaseURL != "https://gw.example/v1" {
		t.Fatalf("gateway entry = %+v (ok=%v), want openai family at the preset's base_url", gw, ok)
	}
	orphan, ok := byEnv["ORPHAN_API_KEY"]
	if !ok || orphan.Family != "" {
		t.Fatalf("orphan entry = %+v (ok=%v), want no probe family", orphan, ok)
	}
	// No preset uses the orphan key: it is shown as stored-but-unverified,
	// without a network call, rather than the old "no endpoint" failure.
	if got := checkHealth(orphan); got.Status != loginUnverified {
		t.Fatalf("orphan health = %+v, want loginUnverified", got)
	}
}

func TestCheckHealthAPIKeyEntryUsesFamilyProbe(t *testing.T) {
	cases := []struct {
		name         string
		family       string
		modelsStatus int
		baseSuffix   string
		want         loginStatus
		wantPath     string
	}{
		{"openai ok", "openai", http.StatusOK, "/v1", loginValid, "/v1/models"},
		{"anthropic ok", "anthropic", http.StatusOK, "", loginValid, "/v1/models"},
		{"rejected key", "openai", http.StatusUnauthorized, "/v1", loginInvalid, "/v1/models"},
		{"no model listing", "anthropic", http.StatusNotFound, "", loginUnverified, "/v1/models"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, seen := fakeFamilyEndpoint(t, tc.modelsStatus)
			entry := loginEntry{Provider: "EXAMPLE_API_KEY", Family: tc.family, BaseURL: srv.URL + tc.baseSuffix, Key: "sk-example"}
			got := checkHealth(entry)
			if got.Status != tc.want || got.Provider != "EXAMPLE_API_KEY" {
				t.Fatalf("checkHealth = %+v, want status %v", got, tc.want)
			}
			reqs := seen()
			if len(reqs) != 1 || reqs[0].Path != tc.wantPath {
				t.Fatalf("requests = %+v, want one GET %s", reqs, tc.wantPath)
			}
			switch tc.family {
			case "openai":
				if reqs[0].Authorization != "Bearer sk-example" {
					t.Fatalf("openai probe auth = %+v, want Bearer key", reqs[0])
				}
			case "anthropic":
				if reqs[0].APIKey != "sk-example" || !reqs[0].AnthropicVersionPresent {
					t.Fatalf("anthropic probe headers = %+v, want x-api-key + anthropic-version", reqs[0])
				}
			}
		})
	}
}

func TestCheckHealthAPIKeyEntryWithoutKeyIsInvalid(t *testing.T) {
	got := checkHealth(loginEntry{Provider: "EMPTY_API_KEY", Family: "openai", BaseURL: "http://127.0.0.1:1/v1"})
	if got.Status != loginInvalid {
		t.Fatalf("checkHealth(no key) = %+v, want loginInvalid", got)
	}
}
