package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	tests := []struct {
		name  string
		value int
	}{
		{name: "value_-1", value: -1},
		// Zero was the removed "unlimited" sentinel; loading it must use the
		// finite default rather than resurrecting unbounded pagination.
		{name: "removed_unlimited_sentinel_0", value: 0},
		// Fifty was the legacy small page size; loading it migrates to the
		// current finite default.
		{name: "legacy_small_page_size_50", value: 50},
		{name: "value_300", value: 300},
		{name: "value_2001", value: 2001},
		{name: "value_5000", value: 5000},
		{name: "value_999999", value: 999999},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			payload := []byte(fmt.Sprintf(`{"language":"en","mail_page_size":%d}`, tc.value))
			if err := os.WriteFile(filepath.Join(dir, "tui_config.json"), payload, 0o644); err != nil {
				t.Fatal(err)
			}
			if cfg := LoadTUIConfig(dir); cfg.MailPageSize != 200 {
				t.Fatalf("unsupported mail_page_size=%d loaded as %d, want 200", tc.value, cfg.MailPageSize)
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

func TestResolveKeysEnvAuthoritative(t *testing.T) {
	dir := t.TempDir()
	// .env is the agent source; config.json has a STALE value for the same key
	// plus a key only it knows.
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("DEEPSEEK_API_KEY=env-correct\nLINGTAI_SOUL_FLOW_ENABLED=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Write config.json directly (not via SaveConfig, which would merge/sync
	// .env and mask the drift scenario we are testing).
	cfgJSON := `{"keys": {"DEEPSEEK_API_KEY": "cfg-stale", "ZHIPU_API_KEY": "cfg-only"}}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(cfgJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	keys, configOK := ResolveKeys(dir)
	if !configOK {
		t.Fatalf("configOK = false, want true (config.json exists)")
	}
	// .env wins for the shared key; config.json fills gaps; non-key env vars excluded.
	if keys["DEEPSEEK_API_KEY"] != "env-correct" {
		t.Fatalf("DEEPSEEK_API_KEY = %q, want env-correct (env authoritative)", keys["DEEPSEEK_API_KEY"])
	}
	if keys["ZHIPU_API_KEY"] != "cfg-only" {
		t.Fatalf("ZHIPU_API_KEY = %q, want cfg-only (config fills gaps)", keys["ZHIPU_API_KEY"])
	}
	// ResolveKeys returns the full resolved env (including non-key vars);
	// callers filter API keys via HasAPIKeys or the *_API_KEY suffix.
	if keys["LINGTAI_SOUL_FLOW_ENABLED"] != "1" {
		t.Fatalf("LINGTAI_SOUL_FLOW_ENABLED = %q, want 1 (env vars preserved)", keys["LINGTAI_SOUL_FLOW_ENABLED"])
	}
	if !HasAPIKeys(keys) {
		t.Fatalf("HasAPIKeys(%v) = false, want true", keys)
	}
}

func TestResolveKeysMissingConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("DEEPSEEK_API_KEY=only-env\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	keys, configOK := ResolveKeys(dir)
	if configOK {
		t.Fatalf("configOK = true, want false (config.json missing)")
	}
	if keys["DEEPSEEK_API_KEY"] != "only-env" {
		t.Fatalf("DEEPSEEK_API_KEY = %q, want only-env", keys["DEEPSEEK_API_KEY"])
	}
}

func TestReadEnvKeysNormalizesCommonForms(t *testing.T) {
	// fable F5: export prefix, surrounding quotes, and inline comments must
	// normalize so the startup gate sees the real key name and self-heal does
	// not mirror quoted values verbatim.
	env := []byte(
		"export ZHIPU_API_KEY=sk-exported\n" +
			"DEEPSEEK_API_KEY=\"sk-quoted\"\n" +
			"MOON_API_KEY=sk-inline # trailing comment\n" +
			"export GROK_API_KEY='sk-single'\n" +
			"LINGTAI_SOUL_FLOW_ENABLED=1\n")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), env, 0o600); err != nil {
		t.Fatal(err)
	}
	keys := ReadEnvKeys(dir)
	cases := []struct {
		name string
		want string
	}{
		{"ZHIPU_API_KEY", "sk-exported"},
		{"DEEPSEEK_API_KEY", "sk-quoted"},
		{"MOON_API_KEY", "sk-inline"},
		{"GROK_API_KEY", "sk-single"},
	}
	for _, tc := range cases {
		if got := keys[tc.name]; got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, got, tc.want)
		}
	}
	if keys["LINGTAI_SOUL_FLOW_ENABLED"] != "1" {
		t.Errorf("non-key var preserved = %q, want 1", keys["LINGTAI_SOUL_FLOW_ENABLED"])
	}
	if !HasAPIKeys(keys) {
		t.Errorf("HasAPIKeys after normalization = false, want true (export case must count)")
	}
}

func TestResolveKeysMalformedConfig(t *testing.T) {
	// fable F6: configOK must report present AND readable; a corrupt mirror
	// must not report healthy while contributing no keys.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("DEEPSEEK_API_KEY=env-x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("{ this is not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	keys, configOK := ResolveKeys(dir)
	if configOK {
		t.Fatalf("configOK = true, want false (config.json present but corrupt)")
	}
	if keys["DEEPSEEK_API_KEY"] != "env-x" {
		t.Fatalf("DEEPSEEK_API_KEY = %q, want env-x (env still resolves)", keys["DEEPSEEK_API_KEY"])
	}
}

func TestHasAPIKeys(t *testing.T) {
	cases := []struct {
		name string
		keys map[string]string
		want bool
	}{
		{"api key present", map[string]string{"DEEPSEEK_API_KEY": "sk-x"}, true},
		{"api key empty", map[string]string{"DEEPSEEK_API_KEY": ""}, false},
		{"no keys", map[string]string{}, false},
		{"non-key vars only", map[string]string{"LINGTAI_SOUL_FLOW_ENABLED": "1"}, false},
	}
	for _, tc := range cases {
		if got := HasAPIKeys(tc.keys); got != tc.want {
			t.Errorf("%s: HasAPIKeys(%v) = %v, want %v", tc.name, tc.keys, got, tc.want)
		}
	}
}

// home_telemetry_display is an optional presentation preference the user hand-
// edits. Loading it must never cost them their other preferences, and never
// setting it must leave no key behind.
func TestTUIConfigHomeTelemetryDisplayFailsClosedAndStaysOmitted(t *testing.T) {
	write := func(t *testing.T, body string) string {
		t.Helper()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "tui_config.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return dir
	}

	t.Run("wrong type falls back to absent without resetting other preferences", func(t *testing.T) {
		// A hand-edited string where a list belongs. Decoding this field as a
		// plain []string would fail the WHOLE file and silently reset language,
		// theme, and mail_page_size to defaults.
		dir := write(t, `{
  "language": "zh",
  "theme": "ink-light",
  "mail_page_size": 500,
  "tool_call_truncate": 400,
  "home_telemetry_display": "context"
}`)
		tc := LoadTUIConfig(dir)
		if tc.HomeTelemetryDisplay != nil {
			t.Errorf("wrong-typed home_telemetry_display = %v, want nil (fall back to default)", tc.HomeTelemetryDisplay)
		}
		if tc.Language != "zh" || tc.Theme != "ink-light" || tc.MailPageSize != 500 || tc.ToolCallTruncate != 400 {
			t.Fatalf("one malformed presentation preference reset the others: %+v", tc)
		}

		// Saving back must not reintroduce the malformed value, and must still
		// carry the surviving preferences.
		if err := SaveTUIConfig(dir, tc); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(dir, "tui_config.json"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "home_telemetry_display") {
			t.Errorf("save re-wrote the invalid preference:\n%s", data)
		}
		if reloaded := LoadTUIConfig(dir); reloaded.Language != "zh" || reloaded.Theme != "ink-light" || reloaded.MailPageSize != 500 || reloaded.ToolCallTruncate != 400 {
			t.Fatalf("preferences did not survive the load/save round trip: %+v", reloaded)
		}
	})

	t.Run("a well-formed but invalid list is discarded and never re-saved", func(t *testing.T) {
		// `ctx` is a near-miss of `context`. The list decodes fine, so nothing
		// upstream rejects it — NormalizeHomeTelemetryDisplay is what has to,
		// wholesale, on BOTH load and save. Otherwise changing an unrelated
		// preference in /settings re-writes the typo as durable config.
		for _, body := range []string{
			`{"language": "zh", "theme": "ink-light", "mail_page_size": 500, "home_telemetry_display": ["context", "ctx"]}`,
			`{"language": "zh", "theme": "ink-light", "mail_page_size": 500, "home_telemetry_display": ["context", "context"]}`,
			`{"language": "zh", "theme": "ink-light", "mail_page_size": 500, "home_telemetry_display": []}`,
			`{"language": "zh", "theme": "ink-light", "mail_page_size": 500,
			  "home_telemetry_display": ["session", "llm", "api", "tokens", "cache", "context", "session"]}`,
		} {
			dir := write(t, body)
			tc := LoadTUIConfig(dir)
			if tc.HomeTelemetryDisplay != nil {
				t.Errorf("invalid home_telemetry_display in %s loaded as %v, want nil", body, tc.HomeTelemetryDisplay)
			}
			if tc.Language != "zh" || tc.Theme != "ink-light" || tc.MailPageSize != 500 {
				t.Fatalf("discarding the invalid expression cost the other preferences: %+v", tc)
			}
			if err := SaveTUIConfig(dir, tc); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(dir, "tui_config.json"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "home_telemetry_display") {
				t.Errorf("save re-wrote the invalid preference:\n%s", data)
			}
			if !strings.Contains(string(data), `"language": "zh"`) || !strings.Contains(string(data), `"mail_page_size": 500`) {
				t.Errorf("save dropped the surviving preferences:\n%s", data)
			}
		}
	})

	t.Run("a valid expression survives load and save", func(t *testing.T) {
		dir := write(t, `{"language": "en", "home_telemetry_display": ["context", "session"]}`)
		tc := LoadTUIConfig(dir)
		if len(tc.HomeTelemetryDisplay) != 2 || tc.HomeTelemetryDisplay[0] != "context" || tc.HomeTelemetryDisplay[1] != "session" {
			t.Fatalf("home_telemetry_display = %v, want [context session]", tc.HomeTelemetryDisplay)
		}
		if err := SaveTUIConfig(dir, tc); err != nil {
			t.Fatal(err)
		}
		reloaded := LoadTUIConfig(dir)
		if len(reloaded.HomeTelemetryDisplay) != 2 || reloaded.HomeTelemetryDisplay[0] != "context" || reloaded.HomeTelemetryDisplay[1] != "session" {
			t.Fatalf("valid home_telemetry_display did not round trip: %v", reloaded.HomeTelemetryDisplay)
		}
	})

	t.Run("absent stays absent on save", func(t *testing.T) {
		dir := t.TempDir()
		if err := SaveTUIConfig(dir, DefaultTUIConfig()); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(dir, "tui_config.json"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "home_telemetry_display") {
			t.Errorf("the default (absent) preference wrote a key:\n%s", data)
		}
	})
}
