package preset

import "testing"

// TestCodexPoolStandalonePresetExists verifies the new codex-pool-standalone
// builtin is additive: it is a separate preset from the old builtin
// "codex-pool" OAuth multi-account pool, routes through the kernel's ordinary
// generic OpenAI-compatible Responses adapter (provider "custom", api_compat
// "openai", wire_api "responses"), points at a local loopback base_url by
// default, and sources its key from an environment variable reference rather
// than an embedded key.
func TestCodexPoolStandalonePresetExists(t *testing.T) {
	p, ok := findBuiltin("codex-pool-standalone")
	if !ok {
		t.Fatal("codex-pool-standalone preset should be a builtin")
	}
	llm := llmOf(p)
	if llm == nil {
		t.Fatal("codex-pool-standalone preset must have an llm map")
	}
	// Must NOT reuse the old builtin "codex-pool" provider string — that
	// provider name is the OAuth multi-account pool route and would keep
	// old builtin account/pool logic hidden behind the new preset name.
	if prov, _ := llm["provider"].(string); prov != "custom" {
		t.Errorf("codex-pool-standalone provider = %q, want %q (the generic OpenAI-compatible route)", prov, "custom")
	}
	if compat, _ := llm["api_compat"].(string); compat != "openai" {
		t.Errorf("codex-pool-standalone api_compat = %q, want %q", compat, "openai")
	}
	if wire, _ := llm["wire_api"].(string); wire != "responses" {
		t.Errorf("codex-pool-standalone wire_api = %q, want %q", wire, "responses")
	}
	if threshold, present := llm["compact_threshold"]; !present || threshold != nil {
		t.Errorf("standalone preset must explicitly disable generic context_management, got %#v", threshold)
	}
	if base, _ := llm["base_url"].(string); base != "http://127.0.0.1:8765/v1" {
		t.Errorf("codex-pool-standalone base_url = %q, want the local codex-pool serve default", base)
	}
	if key, present := llm["api_key"]; present && key != nil {
		t.Errorf("codex-pool-standalone must not embed a literal api_key; got %#v", key)
	}
	if env, _ := llm["api_key_env"].(string); env != "CODEX_POOL_API_KEY" {
		t.Errorf("codex-pool-standalone local key env = %q, want CODEX_POOL_API_KEY shared with the service", env)
	}
}

// TestCodexPoolStandaloneDistinctFromOldPool pins that the two "pool" presets
// stay independent: old accounts/behavior are untouched by the new preset.
func TestCodexPoolStandaloneDistinctFromOldPool(t *testing.T) {
	oldPool, ok := findBuiltin("codex-pool")
	if !ok {
		t.Fatal("old codex-pool preset should still be a builtin")
	}
	newPool, ok := findBuiltin("codex-pool-standalone")
	if !ok {
		t.Fatal("codex-pool-standalone preset should be a builtin")
	}
	oldProvider, _ := llmOf(oldPool)["provider"].(string)
	newProvider, _ := llmOf(newPool)["provider"].(string)
	if oldProvider != "codex-pool" {
		t.Errorf("old codex-pool preset provider changed to %q, want unchanged %q", oldProvider, "codex-pool")
	}
	if newProvider == oldProvider {
		t.Errorf("codex-pool-standalone must not share the old codex-pool provider route; got %q for both", newProvider)
	}
	if !IsBuiltin("codex-pool-standalone") {
		t.Error("IsBuiltin(\"codex-pool-standalone\") should be true")
	}
	if !IsBuiltin("codex-pool") {
		t.Error("IsBuiltin(\"codex-pool\") should remain true (old preset retained)")
	}
}
