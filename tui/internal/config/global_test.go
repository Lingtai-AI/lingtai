package config

import (
	"testing"
)

func TestLoadConfig_Missing(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Keys != nil && len(cfg.Keys) > 0 {
		t.Error("expected empty keys")
	}
}

func TestSaveAndLoadConfig_EnvVarKeyed(t *testing.T) {
	// Post-refactor (2026-04), Config.Keys is keyed by env var name,
	// not provider name. Each preset's manifest.llm.api_key_env says
	// which env var holds its key, so two presets sharing a provider
	// can have distinct keys.
	dir := t.TempDir()
	cfg := Config{Keys: map[string]string{
		"MINIMAX_API_KEY":      "test-minimax-key",
		"MINIMAX_PERSONAL_KEY": "second-minimax-key",
		"LLM_API_KEY":          "test-custom-key",
	}}
	if err := SaveConfig(dir, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Keys == nil {
		t.Fatal("Keys is nil after load")
	}
	for k, want := range cfg.Keys {
		if loaded.Keys[k] != want {
			t.Errorf("Keys[%q] = %q, want %q", k, loaded.Keys[k], want)
		}
	}
}

func TestLoadConfig_LegacyProviderKeysMigrated(t *testing.T) {
	// Pre-refactor configs stored Keys keyed by lowercase provider
	// name. LoadConfig should migrate those to canonical env var
	// names on read so the rest of the codebase only ever sees the
	// new shape.
	dir := t.TempDir()
	legacy := Config{Keys: map[string]string{
		"minimax":    "minimax-secret",
		"zhipu":      "zhipu-secret",
		"deepseek":   "deepseek-secret",
		"openrouter": "openrouter-secret",
		"mimo":       "mimo-secret",
	}}
	if err := SaveConfig(dir, legacy); err != nil {
		t.Fatalf("save legacy: %v", err)
	}
	loaded, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	expected := map[string]string{
		"MINIMAX_API_KEY":    "minimax-secret",
		"ZHIPU_API_KEY":      "zhipu-secret",
		"DEEPSEEK_API_KEY":   "deepseek-secret",
		"OPENROUTER_API_KEY": "openrouter-secret",
		"MIMO_API_KEY":       "mimo-secret",
	}
	for k, want := range expected {
		if loaded.Keys[k] != want {
			t.Errorf("Keys[%q] = %q, want %q (legacy migration)", k, loaded.Keys[k], want)
		}
	}
	for legacyName := range legacy.Keys {
		if _, still := loaded.Keys[legacyName]; still {
			t.Errorf("legacy provider key %q still present after migration", legacyName)
		}
	}
}

func TestLoadConfig_LegacyMigrationPreservesNewEntry(t *testing.T) {
	// If both the legacy provider-keyed entry AND the new env-var-
	// keyed entry exist (e.g. user wrote a custom env var manually
	// and the old TUI also wrote a provider-keyed shadow), the
	// new entry wins — never clobber an explicit env-var-keyed value.
	dir := t.TempDir()
	cfg := Config{Keys: map[string]string{
		"minimax":         "legacy-secret",
		"MINIMAX_API_KEY": "explicit-secret",
	}}
	if err := SaveConfig(dir, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Keys["MINIMAX_API_KEY"] != "explicit-secret" {
		t.Errorf("MINIMAX_API_KEY = %q, want %q (explicit entry should win)",
			loaded.Keys["MINIMAX_API_KEY"], "explicit-secret")
	}
}


