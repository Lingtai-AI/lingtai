package config

import (
	"fmt"
	"os"
	"path/filepath"
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

func TestEnsureConfigPersisted_CreatesIfMissing(t *testing.T) {
	// Regression test for issue #181: OAuth / no-key presets (codex)
	// skipped stepPresetKey entirely, so config.SaveConfig was never
	// called during first-run wizard completion. main.go uses
	// config.json existence as the first-run heuristic, so the next
	// launch re-triggered the recovery wizard every time.
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("precondition: expected config.json absent in TempDir, got err=%v", err)
	}

	EnsureConfigPersisted(dir)

	if _, err := os.Stat(configPath); err != nil {
		t.Errorf("config.json not created after EnsureConfigPersisted: %v", err)
	}
	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("load after ensure: %v", err)
	}
	if len(cfg.Keys) > 0 {
		t.Errorf("expected empty keys after ensure on fresh dir, got %v", cfg.Keys)
	}
}

func TestEnsureConfigPersisted_PreservesExisting(t *testing.T) {
	// When called on a dir that already has a populated config.json,
	// EnsureConfigPersisted must not clobber existing keys.
	dir := t.TempDir()
	seed := Config{Keys: map[string]string{"FOO_API_KEY": "bar"}}
	if err := SaveConfig(dir, seed); err != nil {
		t.Fatalf("seed save: %v", err)
	}

	EnsureConfigPersisted(dir)

	loaded, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := loaded.Keys["FOO_API_KEY"]; got != "bar" {
		t.Errorf("Keys[FOO_API_KEY] = %q, want %q (EnsureConfigPersisted clobbered existing)", got, "bar")
	}
}

func TestEnsureConfigPersisted_DoesNotOverwriteMalformed(t *testing.T) {
	// If config.json exists in a malformed/unreadable state (user-
	// edited, corrupted, half-written), EnsureConfigPersisted must
	// leave it alone — we only care that the file exists as a setup
	// sentinel, never about its content.
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	malformed := []byte("{this is not valid JSON")
	if err := os.WriteFile(configPath, malformed, 0o644); err != nil {
		t.Fatalf("seed malformed: %v", err)
	}

	EnsureConfigPersisted(dir)

	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read after ensure: %v", err)
	}
	if string(got) != string(malformed) {
		t.Errorf("config.json content was modified — want %q, got %q (must not overwrite existing)",
			string(malformed), string(got))
	}
}

func TestEnsureConfigPersisted_DoesNotTouchEnvFile(t *testing.T) {
	// SaveConfig also rewrites .env. EnsureConfigPersisted must not
	// go through SaveConfig, because a user may have populated .env
	// manually (proxy vars, custom env, etc.) that would be wiped.
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	userEnvContent := []byte("HTTPS_PROXY=http://example:8080\nCUSTOM_VAR=manual\n")
	if err := os.WriteFile(envPath, userEnvContent, 0o600); err != nil {
		t.Fatalf("seed .env: %v", err)
	}

	EnsureConfigPersisted(dir)

	got, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read .env after ensure: %v", err)
	}
	if string(got) != string(userEnvContent) {
		t.Errorf(".env was modified — want %q, got %q (must not touch user-edited .env)",
			string(userEnvContent), string(got))
	}
}

func TestDefaultTUIConfig_DisablesInsights(t *testing.T) {
	cfg := DefaultTUIConfig()
	if cfg.Insights {
		t.Fatal("DefaultTUIConfig().Insights = true, want false")
	}
}

func TestDefaultTUIConfig_NoToolCallTruncation(t *testing.T) {
	// Default must show full tool call content (no truncation). 0 means
	// "untruncated" in the rendering path.
	cfg := DefaultTUIConfig()
	if cfg.ToolCallTruncate != 0 {
		t.Fatalf("DefaultTUIConfig().ToolCallTruncate = %d, want 0 (no truncation)", cfg.ToolCallTruncate)
	}
}

func TestDefaultTUIConfig_MailPageSize(t *testing.T) {
	cfg := DefaultTUIConfig()
	if cfg.MailPageSize != 200 {
		t.Fatalf("DefaultTUIConfig().MailPageSize = %d, want 200 (finite UI batch default)", cfg.MailPageSize)
	}
}

func TestLoadTUIConfig_MigratesLegacySmallMailPageSizeToDefault(t *testing.T) {
	dir := t.TempDir()
	payload := []byte(`{"language":"en","mail_page_size":50}`)
	if err := os.WriteFile(filepath.Join(dir, "tui_config.json"), payload, 0o644); err != nil {
		t.Fatalf("write tui_config.json: %v", err)
	}
	if cfg := LoadTUIConfig(dir); cfg.MailPageSize != 200 {
		t.Fatalf("legacy small mail_page_size loaded as %d, want 200 (the finite default)", cfg.MailPageSize)
	}
}

func TestLoadTUIConfig_PreservesHundredMailPageSize(t *testing.T) {
	dir := t.TempDir()
	payload := []byte(`{"language":"en","mail_page_size":100}`)
	if err := os.WriteFile(filepath.Join(dir, "tui_config.json"), payload, 0o644); err != nil {
		t.Fatalf("write tui_config.json: %v", err)
	}
	if cfg := LoadTUIConfig(dir); cfg.MailPageSize != 100 {
		t.Fatalf("mail_page_size=100 loaded as %d, want 100 (explicit finite value preserved)", cfg.MailPageSize)
	}
}

// TestLoadTUIConfig_PreservesExplicitTwoHundred proves the default bump does not
// rewrite a stored 200 (an explicit user choice from the previous default era).
func TestLoadTUIConfig_PreservesExplicitTwoHundred(t *testing.T) {
	dir := t.TempDir()
	payload := []byte(`{"language":"en","mail_page_size":200}`)
	if err := os.WriteFile(filepath.Join(dir, "tui_config.json"), payload, 0o644); err != nil {
		t.Fatalf("write tui_config.json: %v", err)
	}
	if cfg := LoadTUIConfig(dir); cfg.MailPageSize != 200 {
		t.Fatalf("explicit mail_page_size=200 loaded as %d, want 200 (must not migrate to 2000)", cfg.MailPageSize)
	}
}

func TestLoadTUIConfig_NormalizesUnsupportedMailPageSizes(t *testing.T) {
	for _, value := range []int{-1, 0, 50, 300, 2001, 5000, 999999} {
		t.Run(fmt.Sprintf("value_%d", value), func(t *testing.T) {
			dir := t.TempDir()
			payload := []byte(fmt.Sprintf(`{"language":"en","mail_page_size":%d}`, value))
			if err := os.WriteFile(filepath.Join(dir, "tui_config.json"), payload, 0o644); err != nil {
				t.Fatal(err)
			}
			if cfg := LoadTUIConfig(dir); cfg.MailPageSize != 200 {
				t.Fatalf("unsupported mail_page_size=%d loaded as %d, want 200", value, cfg.MailPageSize)
			}
		})
	}
}

func TestSaveTUIConfig_NormalizesUnsupportedMailPageSize(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultTUIConfig()
	cfg.MailPageSize = 5000
	if err := SaveTUIConfig(dir, cfg); err != nil {
		t.Fatal(err)
	}
	if loaded := LoadTUIConfig(dir); loaded.MailPageSize != 200 {
		t.Fatalf("saved unsupported page size loaded as %d, want 200", loaded.MailPageSize)
	}
}

func TestLoadTUIConfig_RemovedUnlimitedSentinelUsesFiniteDefault(t *testing.T) {
	dir := t.TempDir()
	payload := []byte(`{"language":"en","mail_page_size":0}`)
	if err := os.WriteFile(filepath.Join(dir, "tui_config.json"), payload, 0o644); err != nil {
		t.Fatalf("write tui_config.json: %v", err)
	}
	if cfg := LoadTUIConfig(dir); cfg.MailPageSize != 200 {
		t.Fatalf("removed mail_page_size=0 loaded as %d, want finite default 200", cfg.MailPageSize)
	}
}

func TestLoadTUIConfig_MissingDefaultsToNoTruncation(t *testing.T) {
	dir := t.TempDir()
	if cfg := LoadTUIConfig(dir); cfg.ToolCallTruncate != 0 {
		t.Fatalf("missing tui_config.json set ToolCallTruncate=%d; want 0", cfg.ToolCallTruncate)
	}
}

func TestSaveAndLoadTUIConfig_PreservesToolCallTruncate(t *testing.T) {
	dir := t.TempDir()
	tc := DefaultTUIConfig()
	tc.ToolCallTruncate = 200
	if err := SaveTUIConfig(dir, tc); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded := LoadTUIConfig(dir)
	if loaded.ToolCallTruncate != 200 {
		t.Errorf("ToolCallTruncate = %d after round-trip, want 200", loaded.ToolCallTruncate)
	}
}

func TestLoadTUIConfig_MissingOrAbsentInsightsDisablesInsights(t *testing.T) {
	dir := t.TempDir()
	if cfg := LoadTUIConfig(dir); cfg.Insights {
		t.Fatal("missing tui_config.json enabled insights; want false")
	}

	payload := []byte(`{"language":"en","mail_page_size":100}`)
	if err := os.WriteFile(filepath.Join(dir, "tui_config.json"), payload, 0o644); err != nil {
		t.Fatalf("write tui_config.json: %v", err)
	}
	if cfg := LoadTUIConfig(dir); cfg.Insights {
		t.Fatal("tui_config.json without insights enabled insights; want false")
	}
}

// TestLoadConfigReadOnlyPreservesModeWhileLoadConfigTightensMalformed proves
// the draft loader differs only by the permission mutation and that the normal
// loader preserves its historical chmod-before-unmarshal ordering even when
// config.json is malformed.
func TestLoadConfigReadOnlyPreservesModeWhileLoadConfigTightensMalformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("seed malformed config: %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("force seed mode: %v", err)
	}

	if _, err := LoadConfigReadOnly(dir); err == nil {
		t.Fatal("LoadConfigReadOnly unexpectedly accepted malformed JSON")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after read-only load: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("LoadConfigReadOnly changed mode to %o, want 0644", got)
	}

	if _, err := LoadConfig(dir); err == nil {
		t.Fatal("LoadConfig unexpectedly accepted malformed JSON")
	}
	info, err = os.Stat(path)
	if err != nil {
		t.Fatalf("stat after mutating load: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("LoadConfig mode = %o, want historical 0600 migration", got)
	}
}
