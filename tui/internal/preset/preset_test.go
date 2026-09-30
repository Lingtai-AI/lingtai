package preset

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func withTempPresets(t *testing.T, fn func()) {
	t.Helper()
	orig := os.Getenv("HOME")
	tmp := t.TempDir()
	os.Setenv("HOME", tmp)
	defer os.Setenv("HOME", orig)
	fn()
}

func TestList_EmptyDir(t *testing.T) {
	withTempPresets(t, func() {
		presets, err := List()
		if err != nil {
			t.Fatalf("List() error: %v", err)
		}
		if len(presets) != 0 {
			t.Errorf("expected 0 presets, got %d", len(presets))
		}
	})
}

func TestSaveAndLoad_Roundtrip(t *testing.T) {
	withTempPresets(t, func() {
		p := codexPreset()
		if err := Save(p); err != nil {
			t.Fatalf("Save() error: %v", err)
		}
		loaded, err := Load(p.Name)
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		if loaded.Name != p.Name {
			t.Errorf("name = %q, want %q", loaded.Name, p.Name)
		}
		if loaded.Description.Summary != p.Description.Summary {
			t.Errorf("description.summary = %q, want %q",
				loaded.Description.Summary, p.Description.Summary)
		}
	})
}

// TestLoad_CorruptedJSONSurfacesParseError verifies that a saved preset file
// containing invalid JSON reports the parse failure rather than collapsing to
// the generic "preset not found" message (issue #483). The caller needs to
// know the file exists but is broken, not that it's absent.
func TestLoad_CorruptedJSONSurfacesParseError(t *testing.T) {
	withTempPresets(t, func() {
		dir := SavedDir()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir saved: %v", err)
		}
		path := filepath.Join(dir, "broken.json")
		if err := os.WriteFile(path, []byte("{ this is not json"), 0o644); err != nil {
			t.Fatalf("write broken preset: %v", err)
		}

		_, err := Load("broken")
		if err == nil {
			t.Fatal("Load() on corrupted JSON returned nil error, want a parse error")
		}
		msg := err.Error()
		if strings.Contains(msg, "preset not found") {
			t.Errorf("Load() error = %q, must not collapse a parse failure to not-found", msg)
		}
		if !strings.Contains(msg, "parse preset") {
			t.Errorf("Load() error = %q, want it to mention the parse failure", msg)
		}
	})
}

// TestLoad_ReadErrorSurfacesReadError verifies that a non-ENOENT read failure
// (here: the preset "file" is actually a directory, a deterministic and
// root-proof failure) surfaces a read/path error rather than not-found.
func TestLoad_ReadErrorSurfacesReadError(t *testing.T) {
	withTempPresets(t, func() {
		dir := SavedDir()
		// Make saved/blocked.json a directory so os.ReadFile fails with a
		// non-ENOENT error on every platform, regardless of uid.
		if err := os.MkdirAll(filepath.Join(dir, "blocked.json"), 0o755); err != nil {
			t.Fatalf("mkdir saved/blocked.json: %v", err)
		}

		_, err := Load("blocked")
		if err == nil {
			t.Fatal("Load() on a directory-shaped preset returned nil error, want a read error")
		}
		msg := err.Error()
		if strings.Contains(msg, "preset not found") {
			t.Errorf("Load() error = %q, must not collapse a read failure to not-found", msg)
		}
		if !strings.Contains(msg, "read preset") {
			t.Errorf("Load() error = %q, want it to mention the read failure", msg)
		}
	})
}

// TestLoad_MissingReturnsNotFound verifies the original not-found behavior is
// preserved when neither a saved nor a template file exists for the name.
func TestLoad_MissingReturnsNotFound(t *testing.T) {
	withTempPresets(t, func() {
		_, err := Load("does-not-exist")
		if err == nil {
			t.Fatal("Load() for a missing preset returned nil error, want not-found")
		}
		if !strings.Contains(err.Error(), "preset not found") {
			t.Errorf("Load() error = %q, want it to contain \"preset not found\"", err.Error())
		}
	})
}

// TestLoad_SavedWinsOverCorruptTemplate verifies saved-over-template
// precedence is unchanged: a valid saved preset is returned even when a
// same-named template file is corrupt (saved wins, template is never read).
func TestLoad_SavedWinsOverCorruptTemplate(t *testing.T) {
	withTempPresets(t, func() {
		savedDir := SavedDir()
		tmplDir := TemplatesDir()
		if err := os.MkdirAll(savedDir, 0o755); err != nil {
			t.Fatalf("mkdir saved: %v", err)
		}
		if err := os.MkdirAll(tmplDir, 0o755); err != nil {
			t.Fatalf("mkdir templates: %v", err)
		}
		// Valid saved preset.
		writePresetFile(t, savedDir, "dupe", "minimax", "FOO_API_KEY")
		// Corrupt template with the same name — should never be reached.
		if err := os.WriteFile(filepath.Join(tmplDir, "dupe.json"), []byte("{ broken"), 0o644); err != nil {
			t.Fatalf("write broken template: %v", err)
		}

		p, err := Load("dupe")
		if err != nil {
			t.Fatalf("Load() error: %v (saved should win over corrupt template)", err)
		}
		if p.Source != SourceSaved {
			t.Errorf("Load() Source = %v, want SourceSaved", p.Source)
		}
	})
}

func TestLoadFromPath_NormalizesLegacyRootContextLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.json")
	data := map[string]interface{}{
		"name":        "legacy",
		"description": map[string]interface{}{"summary": "legacy"},
		"manifest": map[string]interface{}{
			"llm":           map[string]interface{}{"provider": "x", "model": "y"},
			"capabilities":  map[string]interface{}{},
			"context_limit": float64(300000),
		},
	}
	raw, _ := json.Marshal(data)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write preset: %v", err)
	}

	p, err := loadFromPath(path)
	if err != nil {
		t.Fatalf("loadFromPath() error: %v", err)
	}
	if _, ok := p.Manifest["context_limit"]; ok {
		t.Fatalf("legacy root context_limit still present: %#v", p.Manifest)
	}
	llm := p.Manifest["llm"].(map[string]interface{})
	if got := llm["context_limit"]; got != float64(300000) {
		t.Fatalf("manifest.llm.context_limit = %#v, want 300000", got)
	}
	if errs := p.Validate(); len(errs) != 0 {
		t.Fatalf("Validate() errors after normalization: %v", errs)
	}
}

func TestLoadFromPath_PreservesCanonicalContextLimitFloatCompatibility(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "canonical.json")
	data := []byte(`{"name":"canonical","description":{"summary":"canonical"},"manifest":{"llm":{"provider":"x","model":"y","context_limit":300000},"capabilities":{}}}`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write preset: %v", err)
	}

	p, err := loadFromPath(path)
	if err != nil {
		t.Fatalf("loadFromPath() error: %v", err)
	}
	llm := p.Manifest["llm"].(map[string]interface{})
	if got, ok := llm["context_limit"].(float64); !ok || got != 300000 {
		t.Fatalf("manifest.llm.context_limit = %#v (%T), want float64(300000)", llm["context_limit"], llm["context_limit"])
	}
}

func TestValidate_ConflictingLegacyRootContextLimitPreservesLLM(t *testing.T) {
	p := Preset{
		Name:        "conflict",
		Description: PresetDescription{Summary: "conflict"},
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{
				"provider":      "x",
				"model":         "y",
				"context_limit": float64(1000000),
			},
			"capabilities":  map[string]interface{}{},
			"context_limit": float64(300000),
		},
	}

	if errs := p.Validate(); len(errs) != 0 {
		t.Fatalf("Validate() errors: %v", errs)
	}
	if _, ok := p.Manifest["context_limit"]; ok {
		t.Fatalf("legacy root context_limit still present: %#v", p.Manifest)
	}
	llm := p.Manifest["llm"].(map[string]interface{})
	if got := llm["context_limit"]; got != float64(1000000) {
		t.Fatalf("manifest.llm.context_limit = %#v, want canonical 1000000", got)
	}
}

func TestRefreshTemplates_CreatesAllTemplates(t *testing.T) {
	withTempPresets(t, func() {
		if err := RefreshTemplates(); err != nil {
			t.Fatalf("RefreshTemplates() error: %v", err)
		}
		presets, _ := List()
		var names []string
		for _, p := range presets {
			names = append(names, p.Name)
			if p.Source != SourceTemplate {
				t.Errorf("preset %q: Source = %v, want SourceTemplate", p.Name, p.Source)
			}
		}
		// List returns templates in BuiltinPresets() order: codex (the
		// first-run default) first.
		want := []string{"codex", "claude", "openai", "anthropic"}
		if strings.Join(names, ",") != strings.Join(want, ",") {
			t.Fatalf("template list = %v, want %v", names, want)
		}
	})
}

// TestRefreshTemplates_PrunesRetiredVendorTemplates pins the upgrade path from
// the per-vendor template era: a retired template file (e.g. minimax.json,
// custom.json) left in templates/ is deleted on the next refresh, while a
// user's saved preset with the same name is never touched.
func TestRefreshTemplates_PrunesRetiredVendorTemplates(t *testing.T) {
	withTempPresets(t, func() {
		if err := os.MkdirAll(TemplatesDir(), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(SavedDir(), 0o755); err != nil {
			t.Fatal(err)
		}
		retired := []string{"minimax", "zhipu", "mimo", "deepseek", "gemini", "kimi", "grok", "nvidia", "openrouter", "custom"}
		legacy := []byte(`{"name":"x","description":{"summary":"legacy"},"manifest":{"llm":{"provider":"custom","model":"m"}}}`)
		for _, name := range retired {
			if err := os.WriteFile(filepath.Join(TemplatesDir(), name+".json"), legacy, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		savedPath := filepath.Join(SavedDir(), "minimax.json")
		if err := os.WriteFile(savedPath, legacy, 0o644); err != nil {
			t.Fatal(err)
		}

		if err := RefreshTemplates(); err != nil {
			t.Fatalf("RefreshTemplates() error: %v", err)
		}
		for _, name := range retired {
			if _, err := os.Stat(filepath.Join(TemplatesDir(), name+".json")); !os.IsNotExist(err) {
				t.Errorf("retired template %s.json survived refresh (err=%v)", name, err)
			}
		}
		if data, err := os.ReadFile(savedPath); err != nil || string(data) != string(legacy) {
			t.Fatalf("saved preset must be untouched by refresh: data=%q err=%v", data, err)
		}
	})
}

// writePresetFile writes a minimal valid preset JSON to dir/<name>.json with
// the given provider and api_key_env, and returns its absolute path. Values
// are placeholders only — no real secrets.
func writePresetFile(t *testing.T, dir, name, provider, apiKeyEnv string) string {
	t.Helper()
	manifest := map[string]interface{}{
		"llm": map[string]interface{}{
			"provider":    provider,
			"model":       "test-model",
			"api_key_env": apiKeyEnv,
		},
	}
	doc := map[string]interface{}{
		"description": map[string]interface{}{"summary": "test preset"},
		"manifest":    manifest,
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal preset: %v", err)
	}
	path := filepath.Join(dir, name+".json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write preset: %v", err)
	}
	return path
}

// TestResolveRefs_ValidityGuard locks in the defensive rule: a preset is only
// valid (HasKey) when its credential is actually configured. A preset with no
// configured API key AND no Codex OAuth must NOT be valid. Concretely: a
// keyed preset is valid only when its env var has a value; a codex preset
// (OAuth, no api_key_env) is valid only when Codex OAuth is configured; a
// preset with an empty api_key_env that is not codex is invalid.
func TestResolveRefs_ValidityGuard(t *testing.T) {
	dir := t.TempDir()
	codexRef := writePresetFile(t, dir, "codex", "codex", "")
	legacyCodexDir := t.TempDir()
	legacyCodexRef := writeCodexPresetWithAuthPath(t, legacyCodexDir, "codex", "")
	claudeRef := writePresetFile(t, dir, "claude", "claude-code", "CLAUDE_CODE_OAUTH_TOKEN")
	claudeLegacyRef := writePresetFile(t, dir, "claude-legacy", "claude-code", "")
	claudeUnderscoreRef := writePresetFile(t, dir, "claude_agent_sdk", "claude_agent_sdk", "")
	customRef := writePresetFile(t, dir, "openai-nokey", "openai", "")
	keyedRef := writePresetFile(t, dir, "openai-keyed", "openai", "FOO_API_KEY")
	missingRef := filepath.Join(dir, "nope.json")

	keysWith := map[string]string{"FOO_API_KEY": "placeholder-value"}
	keysClaude := map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "placeholder-token"}
	keysEmpty := map[string]string{}

	cases := []struct {
		name       string
		ref        string
		keys       map[string]string
		auth       AuthState
		wantExists bool
		wantHasKey bool
	}{
		// When CodexAuthDir is empty, codex validity falls back to the legacy
		// global bool for backward compatibility with callers that do not set the dir.
		{"codex no OAuth", codexRef, keysEmpty, AuthState{}, true, false},
		{"codex with OAuth", codexRef, keysEmpty, AuthState{CodexOAuthConfigured: true}, true, true},
		// Keep the original no-dir fixture and nil key-map inputs in both bool
		// directions as explicit legacy-compat rows.
		{"codex legacy global bool false without dir", legacyCodexRef, nil, AuthState{CodexOAuthConfigured: false}, true, false},
		{"codex legacy global bool true without dir", legacyCodexRef, nil, AuthState{CodexOAuthConfigured: true}, true, true},
		// claude-code: a stored setup-token wins; otherwise the local CLI
		// login decides; neither leaves the preset without a credential.
		{"claude-code no token no CLI login", claudeRef, keysEmpty, AuthState{}, true, false},
		{"claude-code setup-token without CLI login", claudeRef, keysClaude, AuthState{}, true, true},
		{"claude-code CLI login without token", claudeRef, keysEmpty, AuthState{ClaudeCodeAuthConfigured: true}, true, true},
		{"claude-code token and CLI login", claudeRef, keysClaude, AuthState{ClaudeCodeAuthConfigured: true}, true, true},
		{"claude-code legacy empty slot reads default token", claudeLegacyRef, keysClaude, AuthState{}, true, true},
		{"claude_agent_sdk alias with CLI auth", claudeUnderscoreRef, keysEmpty, AuthState{ClaudeCodeAuthConfigured: true}, true, true},
		{"claude-code ignores codex OAuth", claudeRef, keysEmpty, AuthState{CodexOAuthConfigured: true}, true, false},
		{"keyless API-key family is invalid", customRef, keysEmpty, AuthState{}, true, false},
		{"keyed with key present", keyedRef, keysWith, AuthState{}, true, true},
		{"keyed with key absent", keyedRef, keysEmpty, AuthState{}, true, false},
		{"missing file", missingRef, keysEmpty, AuthState{}, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveRefsWithAuth([]string{tc.ref}, tc.keys, tc.auth)
			if len(got) != 1 {
				t.Fatalf("expected 1 resolved ref, got %d", len(got))
			}
			rr := got[0]
			if rr.Exists != tc.wantExists {
				t.Errorf("Exists = %v, want %v", rr.Exists, tc.wantExists)
			}
			if rr.HasKey != tc.wantHasKey {
				t.Errorf("HasKey = %v, want %v", rr.HasKey, tc.wantHasKey)
			}
		})
	}
}

// TestResolveRefs_ConservativeDefault verifies the legacy ResolveRefs entry
// point assumes no OAuth: a codex preset resolves to HasKey=false through it.
func TestResolveRefs_ConservativeDefault(t *testing.T) {
	dir := t.TempDir()
	codexRef := writePresetFile(t, dir, "codex", "codex", "")
	got := ResolveRefs([]string{codexRef}, nil)
	if len(got) != 1 {
		t.Fatalf("expected 1 resolved ref, got %d", len(got))
	}
	if got[0].HasKey {
		t.Errorf("codex via ResolveRefs: HasKey = true, want false (conservative default)")
	}

	claudeRef := writePresetFile(t, dir, "claude", "claude-code", "")
	got = ResolveRefs([]string{claudeRef}, nil)
	if len(got) != 1 {
		t.Fatalf("expected 1 resolved ref, got %d", len(got))
	}
	if got[0].HasKey {
		t.Errorf("claude-code via ResolveRefs: HasKey = true, want false (conservative default)")
	}
}

// writeCodexPresetWithAuthPath writes a codex preset whose llm.codex_auth_path
// is set to authRef (may be ""), returning its absolute path.
func writeCodexPresetWithAuthPath(t *testing.T, dir, name, authRef string) string {
	t.Helper()
	llm := map[string]interface{}{
		"provider":    "codex",
		"model":       "gpt-5.6-sol",
		"api_key_env": "",
	}
	if authRef != "" {
		llm["codex_auth_path"] = authRef
	}
	doc := map[string]interface{}{
		"description": map[string]interface{}{"summary": "codex preset"},
		"manifest":    map[string]interface{}{"llm": llm},
	}
	raw, _ := json.MarshalIndent(doc, "", "  ")
	path := filepath.Join(dir, name+".json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write preset: %v", err)
	}
	return path
}

// writeCodexPresetWithRawAuthPath writes a codex preset whose llm.codex_auth_path
// is set to an arbitrary JSON value (number/object/null/whitespace string),
// returning its absolute path.
func writeCodexPresetWithRawAuthPath(t *testing.T, dir, name string, raw interface{}) string {
	t.Helper()
	llm := map[string]interface{}{
		"provider":    "codex",
		"model":       "gpt-5.6-sol",
		"api_key_env": "",
	}
	llm["codex_auth_path"] = raw
	doc := map[string]interface{}{
		"description": map[string]interface{}{"summary": "codex preset"},
		"manifest":    map[string]interface{}{"llm": llm},
	}
	data, _ := json.MarshalIndent(doc, "", "  ")
	path := filepath.Join(dir, name+".json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write preset: %v", err)
	}
	return path
}

func writeStubTokenFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"refresh_token":"stub-refresh"}`), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
}

// writeMalformedTokenFile writes a token file whose refresh_token is
// whitespace-only, which the canonical account store rejects and the unbound
// preset fallback must therefore ignore.
func writeMalformedTokenFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"refresh_token":"   "}`), 0o600); err != nil {
		t.Fatalf("write malformed token: %v", err)
	}
}

// TestResolveRefs_PerAccountCodexAuth verifies that, when AuthState.CodexAuthDir
// is set, each codex preset's validity is judged by ITS OWN bound token file
// (manifest.llm.codex_auth_path), with an empty path falling back to the legacy
// account. One missing account never invalidates a different, valid one.
func TestResolveRefs_PerAccountCodexAuth(t *testing.T) {
	presetDir := t.TempDir()
	authDir := t.TempDir()

	// Two accounts on disk: legacy (valid) and a per-account file (valid).
	writeStubTokenFile(t, filepath.Join(authDir, "codex-auth.json"))
	writeStubTokenFile(t, filepath.Join(authDir, "codex-auth", "work.json"))

	legacyBound := writeCodexPresetWithAuthPath(t, presetDir, "codex-legacy", "")
	workBound := writeCodexPresetWithAuthPath(t, presetDir, "codex-work", "~/never-used-home")
	// Re-point work-bound preset at the real per-account file via a relative
	// ref so it resolves under authDir (avoids depending on $HOME in tests).
	workBound = writeCodexPresetWithAuthPath(t, presetDir, "codex-work", "codex-auth/work.json")
	missingBound := writeCodexPresetWithAuthPath(t, presetDir, "codex-missing", "codex-auth/gone.json")

	auth := AuthState{CodexAuthDir: authDir}

	cases := []struct {
		name    string
		ref     string
		wantKey bool
	}{
		{"legacy-bound valid", legacyBound, true},
		{"work-bound valid", workBound, true},
		{"missing-account invalid", missingBound, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveRefsWithAuth([]string{tc.ref}, nil, auth)
			if len(got) != 1 {
				t.Fatalf("expected 1 resolved ref, got %d", len(got))
			}
			if got[0].HasKey != tc.wantKey {
				t.Errorf("HasKey = %v, want %v (CodexAuthRef=%q)", got[0].HasKey, tc.wantKey, got[0].CodexAuthRef)
			}
		})
	}

	// CodexAuthRef should echo the preset's bound path verbatim.
	got := ResolveRefsWithAuth([]string{workBound}, nil, auth)
	if got[0].CodexAuthRef != "codex-auth/work.json" {
		t.Errorf("CodexAuthRef = %q, want the preset's codex_auth_path", got[0].CodexAuthRef)
	}
}

func TestResolveRefs_CodexDefaultAuthAcceptsAnyStoredAccount(t *testing.T) {
	presetDir := t.TempDir()

	t.Run("legacy file valid", func(t *testing.T) {
		authDir := t.TempDir()
		writeStubTokenFile(t, filepath.Join(authDir, "codex-auth.json"))
		ref := writeCodexPresetWithAuthPath(t, presetDir, "codex-legacy-default", "")

		got := ResolveRefsWithAuth([]string{ref}, nil, AuthState{CodexAuthDir: authDir})
		if len(got) != 1 || !got[0].HasKey {
			t.Fatalf("default codex preset with legacy auth = %#v, want HasKey=true", got)
		}
	})

	t.Run("per-account file valid", func(t *testing.T) {
		authDir := t.TempDir()
		writeStubTokenFile(t, filepath.Join(authDir, "codex-auth", "work.json"))
		ref := writeCodexPresetWithAuthPath(t, presetDir, "codex-per-account-default", "")

		got := ResolveRefsWithAuth([]string{ref}, nil, AuthState{CodexAuthDir: authDir})
		if len(got) != 1 || !got[0].HasKey {
			t.Fatalf("default codex preset with per-account auth = %#v, want HasKey=true", got)
		}
	})

	t.Run("no credentials", func(t *testing.T) {
		authDir := t.TempDir()
		ref := writeCodexPresetWithAuthPath(t, presetDir, "codex-no-auth-default", "")

		got := ResolveRefsWithAuth([]string{ref}, nil, AuthState{CodexAuthDir: authDir})
		if len(got) != 1 || got[0].HasKey {
			t.Fatalf("default codex preset with no auth = %#v, want HasKey=false", got)
		}
	})

	t.Run("whitespace-only per-account token ignored with valid one present", func(t *testing.T) {
		authDir := t.TempDir()
		writeStubTokenFile(t, filepath.Join(authDir, "codex-auth", "work.json"))
		writeMalformedTokenFile(t, filepath.Join(authDir, "codex-auth", "bad.json"))
		ref := writeCodexPresetWithAuthPath(t, presetDir, "codex-bad-plus-good-default", "")

		got := ResolveRefsWithAuth([]string{ref}, nil, AuthState{CodexAuthDir: authDir})
		if len(got) != 1 || !got[0].HasKey {
			t.Fatalf("default codex preset with bad+valid per-account auth = %#v, want HasKey=true", got)
		}
	})

	t.Run("whitespace-only per-account token alone is not valid", func(t *testing.T) {
		authDir := t.TempDir()
		writeMalformedTokenFile(t, filepath.Join(authDir, "codex-auth", "bad.json"))
		ref := writeCodexPresetWithAuthPath(t, presetDir, "codex-bad-only-default", "")

		got := ResolveRefsWithAuth([]string{ref}, nil, AuthState{CodexAuthDir: authDir})
		if len(got) != 1 || got[0].HasKey {
			t.Fatalf("default codex preset with only whitespace token = %#v, want HasKey=false", got)
		}
	})

	t.Run("explicit auth path remains exact", func(t *testing.T) {
		authDir := t.TempDir()
		writeStubTokenFile(t, filepath.Join(authDir, "codex-auth", "work.json"))
		ref := writeCodexPresetWithAuthPath(t, presetDir, "codex-explicit-missing", "codex-auth/missing.json")

		got := ResolveRefsWithAuth([]string{ref}, nil, AuthState{CodexAuthDir: authDir})
		if len(got) != 1 || got[0].HasKey {
			t.Fatalf("explicit codex preset with different account auth = %#v, want HasKey=false", got)
		}
		if got[0].CodexAuthRef != "codex-auth/missing.json" {
			t.Fatalf("CodexAuthRef = %q, want explicit ref", got[0].CodexAuthRef)
		}
	})
}

// TestResolveRefs_CodexMalformedExplicitAuthFailsClosed verifies that a present
// but malformed manifest.llm.codex_auth_path (wrong JSON type or whitespace-only
// string) never fails open to the unbound fallback: the preset must resolve
// HasKey=false even when a valid stored account exists.
func TestResolveRefs_CodexMalformedExplicitAuthFailsClosed(t *testing.T) {
	presetDir := t.TempDir()
	authDir := t.TempDir()
	writeStubTokenFile(t, filepath.Join(authDir, "codex-auth.json"))
	auth := AuthState{CodexAuthDir: authDir}

	cases := []struct {
		name string
		raw  interface{}
	}{
		{"number", 42},
		{"object", map[string]interface{}{"k": "v"}},
		{"null", nil},
		{"whitespace-only", "   "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ref := writeCodexPresetWithRawAuthPath(t, presetDir, "codex-malformed-"+tc.name, tc.raw)
			got := ResolveRefsWithAuth([]string{ref}, nil, auth)
			if len(got) != 1 || got[0].HasKey {
				t.Fatalf("malformed explicit codex_auth_path %#v = %#v, want HasKey=false", tc.raw, got)
			}
		})
	}
}

// TestResolveRefs_CodexExplicitAuthNeverUsesAggregateBool verifies that a preset
// with an explicit non-empty codex_auth_path is never validated by the aggregate
// CodexOAuthConfigured bool, even when CodexAuthDir is empty. Absolute and
// ~/-prefixed refs remain exactly checkable without a dir; relative refs fail
// closed because they cannot be resolved.
func TestResolveRefs_CodexExplicitAuthNeverUsesAggregateBool(t *testing.T) {
	presetDir := t.TempDir()

	t.Run("relative missing ref fails closed with aggregate true", func(t *testing.T) {
		ref := writeCodexPresetWithAuthPath(t, presetDir, "codex-rel-missing", "codex-auth/missing.json")
		got := ResolveRefsWithAuth([]string{ref}, nil, AuthState{CodexOAuthConfigured: true})
		if len(got) != 1 || got[0].HasKey {
			t.Fatalf("explicit relative missing ref + aggregate true = %#v, want HasKey=false", got)
		}
	})

	t.Run("absolute valid ref resolves without dir", func(t *testing.T) {
		authDir := t.TempDir()
		absPath := filepath.Join(authDir, "codex-auth", "abs.json")
		writeStubTokenFile(t, absPath)
		ref := writeCodexPresetWithAuthPath(t, presetDir, "codex-abs-valid", absPath)
		got := ResolveRefsWithAuth([]string{ref}, nil, AuthState{CodexOAuthConfigured: true})
		if len(got) != 1 || !got[0].HasKey {
			t.Fatalf("explicit absolute valid ref without dir = %#v, want HasKey=true", got)
		}
	})

	t.Run("absolute missing ref fails closed without dir", func(t *testing.T) {
		absPath := filepath.Join(t.TempDir(), "codex-auth", "missing.json")
		ref := writeCodexPresetWithAuthPath(t, presetDir, "codex-abs-missing", absPath)
		got := ResolveRefsWithAuth([]string{ref}, nil, AuthState{CodexOAuthConfigured: true})
		if len(got) != 1 || got[0].HasKey {
			t.Fatalf("explicit absolute missing ref without dir = %#v, want HasKey=false", got)
		}
	})
}

func TestGenerateInitJSON_ProducesValidJSON(t *testing.T) {
	withTempPresets(t, func() {
		p := codexPreset()
		tmpDir := t.TempDir()
		lingtaiDir := filepath.Join(tmpDir, ".lingtai")
		os.MkdirAll(lingtaiDir, 0o755)

		globalDir := filepath.Join(tmpDir, ".lingtai-global")
		Bootstrap(globalDir)
		if err := GenerateInitJSON(p, "test-agent", "test-agent", lingtaiDir, globalDir); err != nil {
			t.Fatalf("GenerateInitJSON() error: %v", err)
		}

		// Check init.json exists and is valid
		initPath := filepath.Join(lingtaiDir, "test-agent", "init.json")
		data, err := os.ReadFile(initPath)
		if err != nil {
			t.Fatalf("read init.json: %v", err)
		}
		var initJSON map[string]interface{}
		if err := json.Unmarshal(data, &initJSON); err != nil {
			t.Fatalf("parse init.json: %v", err)
		}

		// Check required fields
		manifest, ok := initJSON["manifest"].(map[string]interface{})
		if !ok {
			t.Fatal("manifest not a map")
		}
		for _, key := range []string{"agent_name", "language", "llm", "capabilities", "admin", "streaming", "max_turns"} {
			if _, exists := manifest[key]; !exists {
				t.Errorf("manifest missing key %q", key)
			}
		}
		if manifest["agent_name"] != "test-agent" {
			t.Errorf("agent_name = %v, want %q", manifest["agent_name"], "test-agent")
		}
		if got, want := manifest["max_turns"], float64(500); got != want {
			t.Errorf("max_turns = %v, want %v", got, want)
		}

		// Check .agent.json exists
		agentPath := filepath.Join(lingtaiDir, "test-agent", ".agent.json")
		if _, err := os.Stat(agentPath); err != nil {
			t.Errorf(".agent.json not created: %v", err)
		}
	})
}

func TestBuiltinPresetRequestedDefaultModels(t *testing.T) {
	cases := []struct {
		name      string
		preset    Preset
		wantModel string
	}{
		{"codex", codexPreset(), "gpt-5.6-sol"},
		// claude-code carries no model: Claude Code runs its own default.
		{"claude", claudePreset(), ""},
		// The two bring-your-own-endpoint families have no universal model.
		{"openai", openaiPreset(), ""},
		{"anthropic", anthropicPreset(), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			llm, ok := tc.preset.Manifest["llm"].(map[string]interface{})
			if !ok {
				t.Fatalf("%s manifest.llm missing or wrong type: %T", tc.name, tc.preset.Manifest["llm"])
			}
			if got, _ := llm["model"].(string); got != tc.wantModel {
				t.Fatalf("%s default model = %q, want %q", tc.name, got, tc.wantModel)
			}
		})
	}
}

// TestBuiltinPresetsAreTheFourProviderFamilies pins the template inventory to
// exactly one template per provider family the kernel accepts, and the
// manifest shape of each.
func TestBuiltinPresetsAreTheFourProviderFamilies(t *testing.T) {
	want := []struct {
		name, provider, apiKeyEnv string
	}{
		{"codex", ProviderCodex, ""},
		{"claude", ProviderClaudeCode, "CLAUDE_CODE_OAUTH_TOKEN"},
		{"openai", ProviderOpenAI, "OPENAI_API_KEY"},
		{"anthropic", ProviderAnthropic, "ANTHROPIC_API_KEY"},
	}
	got := BuiltinPresets()
	if len(got) != len(want) {
		t.Fatalf("BuiltinPresets() has %d templates, want %d", len(got), len(want))
	}
	for i, w := range want {
		p := got[i]
		if p.Name != w.name {
			t.Fatalf("BuiltinPresets()[%d] = %q, want %q", i, p.Name, w.name)
		}
		if !IsBuiltin(w.name) {
			t.Errorf("IsBuiltin(%q) = false, want true", w.name)
		}
		llm := p.Manifest["llm"].(map[string]interface{})
		if got := llm["provider"]; got != w.provider {
			t.Errorf("%s provider = %#v, want %q", w.name, got, w.provider)
		}
		if got := llm["api_key_env"]; got != w.apiKeyEnv {
			t.Errorf("%s api_key_env = %#v, want %q", w.name, got, w.apiKeyEnv)
		}
		if got := DefaultAPIKeyEnv(w.provider); got != w.apiKeyEnv {
			t.Errorf("DefaultAPIKeyEnv(%q) = %q, want %q", w.provider, got, w.apiKeyEnv)
		}
		if _, ok := llm["api_compat"]; ok {
			t.Errorf("%s template must not write the retired api_compat field", w.name)
		}
		if p.Description.Summary == "" {
			t.Errorf("%s template has an empty summary", w.name)
		}
	}
	for _, retired := range []string{"minimax", "zhipu", "mimo", "deepseek", "gemini", "kimi", "grok", "nvidia", "openrouter", "custom"} {
		if IsBuiltin(retired) {
			t.Errorf("IsBuiltin(%q) = true, want false (per-vendor templates are retired)", retired)
		}
	}
}

func TestFamilyTemplatesEndpointFields(t *testing.T) {
	openai := openaiPreset().Manifest["llm"].(map[string]interface{})
	if v, ok := openai["base_url"]; !ok || v != nil {
		t.Fatalf("openai template base_url = %#v (present=%v), want nil (official endpoint)", v, ok)
	}
	if got := openai["wire_api"]; got != WireAPIChatCompletions {
		t.Fatalf("openai template wire_api = %#v, want chat_completions", got)
	}
	anthropic := anthropicPreset().Manifest["llm"].(map[string]interface{})
	if v, ok := anthropic["base_url"]; !ok || v != nil {
		t.Fatalf("anthropic template base_url = %#v (present=%v), want nil (official endpoint)", v, ok)
	}
	if _, ok := anthropic["wire_api"]; ok {
		t.Fatalf("anthropic template must not carry wire_api")
	}
	if got := DefaultBaseURL(ProviderOpenAI); got != "https://api.openai.com/v1" {
		t.Fatalf("DefaultBaseURL(openai) = %q", got)
	}
	if got := DefaultBaseURL(ProviderAnthropic); got != "https://api.anthropic.com" {
		t.Fatalf("DefaultBaseURL(anthropic) = %q", got)
	}
	for _, provider := range []string{ProviderCodex, ProviderClaudeCode, "custom", ""} {
		if got := DefaultBaseURL(provider); got != "" {
			t.Fatalf("DefaultBaseURL(%q) = %q, want empty", provider, got)
		}
	}
	for _, provider := range []string{ProviderCodex, "custom", ""} {
		if got := DefaultAPIKeyEnv(provider); got != "" {
			t.Fatalf("DefaultAPIKeyEnv(%q) = %q, want empty", provider, got)
		}
	}
}

func TestCodexPresetDefaultOmitsServiceTierAndSetsThinking(t *testing.T) {
	p := codexPreset()
	llm, ok := p.Manifest["llm"].(map[string]interface{})
	if !ok {
		t.Fatalf("codex manifest.llm missing or wrong type: %T", p.Manifest["llm"])
	}
	if _, ok := llm["service_tier"]; ok {
		t.Fatalf("codex preset default should omit llm.service_tier; got %#v", llm["service_tier"])
	}
	// LingTai is the primary brain, so the default Codex preset carries
	// reasoning effort xhigh explicitly (not a UI-only fallback) so the
	// running session actually receives it.
	if got, ok := llm["thinking"].(string); !ok || got != "xhigh" {
		t.Fatalf("codex preset default should set llm.thinking=xhigh; got %#v", llm["thinking"])
	}

	tmpDir := t.TempDir()
	lingtaiDir := filepath.Join(tmpDir, ".lingtai")
	globalDir := filepath.Join(tmpDir, "global")
	if err := os.MkdirAll(lingtaiDir, 0o755); err != nil {
		t.Fatalf("create lingtai dir: %v", err)
	}
	if err := GenerateInitJSON(p, "codex-agent", "codex-agent", lingtaiDir, globalDir); err != nil {
		t.Fatalf("GenerateInitJSON() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(lingtaiDir, "codex-agent", "init.json"))
	if err != nil {
		t.Fatalf("read init.json: %v", err)
	}
	var initJSON map[string]interface{}
	if err := json.Unmarshal(data, &initJSON); err != nil {
		t.Fatalf("parse init.json: %v", err)
	}
	manifest := initJSON["manifest"].(map[string]interface{})
	generatedLLM := manifest["llm"].(map[string]interface{})
	if _, ok := generatedLLM["service_tier"]; ok {
		t.Fatalf("generated codex init.json should omit llm.service_tier; got %#v", generatedLLM["service_tier"])
	}
	if got, ok := generatedLLM["thinking"].(string); !ok || got != "xhigh" {
		t.Fatalf("generated codex init.json should set llm.thinking=xhigh; got %#v", generatedLLM["thinking"])
	}
}

func TestClaudePresetShape(t *testing.T) {
	p := claudePreset()
	if p.Name != "claude" {
		t.Fatalf("name = %q, want claude", p.Name)
	}
	llm, ok := p.Manifest["llm"].(map[string]interface{})
	if !ok {
		t.Fatalf("manifest.llm missing or wrong type: %T", p.Manifest["llm"])
	}
	if got := llm["provider"]; got != "claude-code" {
		t.Errorf("llm.provider = %v, want claude-code", got)
	}
	// No model and no thinking: Claude Code picks its own default model and
	// effort, so the template carries neither key.
	for _, key := range []string{"model", "thinking", "base_url"} {
		if v, ok := llm[key]; ok {
			t.Errorf("llm.%s = %#v, want absent", key, v)
		}
	}
	// The local Claude login works as-is; a `claude setup-token` token, when
	// stored, lives in the shared CLAUDE_CODE_OAUTH_TOKEN slot.
	if got, ok := llm["api_key"]; !ok || got != nil {
		t.Errorf("llm.api_key = %v (present=%v), want nil", got, ok)
	}
	if got := llm["api_key_env"]; got != ClaudeCodeOAuthTokenEnv {
		t.Errorf("llm.api_key_env = %v, want %s", got, ClaudeCodeOAuthTokenEnv)
	}
	// The model-less template is still a valid preset.
	if errs := p.Validate(); len(errs) != 0 {
		t.Errorf("claude template Validate() = %v, want no violations", errs)
	}
	// Conservative capabilities: keep LingTai skills, do NOT wire
	// web_search/vision through this provider.
	caps, ok := p.Manifest["capabilities"].(map[string]interface{})
	if !ok {
		t.Fatalf("manifest.capabilities missing or wrong type: %T", p.Manifest["capabilities"])
	}
	if _, ok := caps["skills"]; !ok {
		t.Errorf("capabilities.skills should be present (LingTai skills default)")
	}
	if _, ok := caps["web_search"]; ok {
		t.Errorf("capabilities.web_search should be absent for claude")
	}
	if _, ok := caps["vision"]; ok {
		t.Errorf("capabilities.vision should be absent for claude")
	}
}

func TestClaudePresetIsBuiltin(t *testing.T) {
	if !IsBuiltin("claude") {
		t.Errorf("IsBuiltin(claude) = false, want true")
	}
	found := false
	for _, p := range BuiltinPresets() {
		if p.Name == "claude" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("claude not present in BuiltinPresets()")
	}
}

func TestDelete_RemovesFile(t *testing.T) {
	withTempPresets(t, func() {
		p := codexPreset()
		Save(p)
		if err := Delete(p.Name); err != nil {
			t.Fatalf("Delete() error: %v", err)
		}
		presets, _ := List()
		if len(presets) != 0 {
			t.Errorf("expected 0 presets after delete, got %d", len(presets))
		}
	})
}

func TestHasAny(t *testing.T) {
	withTempPresets(t, func() {
		if HasAny() {
			t.Error("HasAny() = true, want false on empty dir")
		}
		Save(codexPreset())
		if !HasAny() {
			t.Error("HasAny() = false, want true after save")
		}
	})
}

func TestGenerateInitJSONWritesPresetBlock(t *testing.T) {
	tmp := t.TempDir()
	globalDir := filepath.Join(tmp, "global")
	lingtaiDir := filepath.Join(tmp, "project", ".lingtai")
	os.MkdirAll(lingtaiDir, 0o755)

	p := openaiPreset()
	if err := GenerateInitJSON(p, "alice", "alice", lingtaiDir, globalDir); err != nil {
		t.Fatalf("GenerateInitJSON: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(lingtaiDir, "alice", "init.json"))
	if err != nil {
		t.Fatalf("read init.json: %v", err)
	}

	var init map[string]interface{}
	if err := json.Unmarshal(data, &init); err != nil {
		t.Fatalf("parse init.json: %v", err)
	}

	manifest := init["manifest"].(map[string]interface{})
	preset, ok := manifest["preset"].(map[string]interface{})
	if !ok {
		t.Fatalf("manifest.preset block missing")
	}
	// Templates resolve to presets/templates/<name>.json; openaiPreset()
	// is a template per IsBuiltin, even without Source set.
	wantRef := "~/.lingtai-tui/presets/templates/" + p.Name + ".json"
	if active, _ := preset["active"].(string); active != wantRef {
		t.Errorf("manifest.preset.active = %v, want %s", preset["active"], wantRef)
	}
	if def, _ := preset["default"].(string); def != wantRef {
		t.Errorf("manifest.preset.default = %v, want %s", preset["default"], wantRef)
	}
	allowed, ok := preset["allowed"].([]interface{})
	if !ok {
		t.Fatalf("manifest.preset.allowed missing or wrong type: %T", preset["allowed"])
	}
	if len(allowed) != 1 {
		t.Errorf("manifest.preset.allowed len=%d, want 1; got %v", len(allowed), allowed)
	}
	if first, _ := allowed[0].(string); first != wantRef {
		t.Errorf("manifest.preset.allowed[0] = %v, want %s", allowed[0], wantRef)
	}
}

func TestAutoEnvVarName(t *testing.T) {
	pp := func(provider, baseURL string) Preset {
		return Preset{Manifest: map[string]interface{}{
			"llm": map[string]interface{}{
				"provider": provider,
				"base_url": baseURL,
			},
		}}
	}

	cases := []struct {
		name     string
		preset   Preset
		existing map[string]string
		want     string
	}{
		{
			name:   "openai, no existing → _1_",
			preset: pp("openai", ""),
			want:   "OPENAI_1_API_KEY",
		},
		{
			name:   "anthropic, no existing → _1_",
			preset: pp("anthropic", ""),
			want:   "ANTHROPIC_1_API_KEY",
		},
		{
			name:     "openai with _1_ taken → gap-fill _2_",
			preset:   pp("openai", "https://gateway.example.com/v1"),
			existing: map[string]string{"OPENAI_1_API_KEY": "k"},
			want:     "OPENAI_2_API_KEY",
		},
		{
			name:   "gap fill: _1_ taken, _2_ free, _3_ taken → returns _2_",
			preset: pp("anthropic", ""),
			existing: map[string]string{
				"ANTHROPIC_1_API_KEY": "k",
				"ANTHROPIC_3_API_KEY": "k",
			},
			want: "ANTHROPIC_2_API_KEY",
		},
		{
			// The endpoint never changes the slot name: there is no region
			// suffix in the four-family model.
			name:   "base_url does not add a suffix",
			preset: pp("anthropic", "https://api.minimaxi.com/anthropic"),
			want:   "ANTHROPIC_1_API_KEY",
		},
		{
			name:   "non-numeric existing entries (e.g. legacy) ignored",
			preset: pp("openai", ""),
			existing: map[string]string{
				"OPENAI_API_KEY":      "legacy",
				"OPENAI_PROD_API_KEY": "legacy",
			},
			want: "OPENAI_1_API_KEY",
		},
		{
			name:   "no provider → empty",
			preset: Preset{Manifest: map[string]interface{}{"llm": map[string]interface{}{}}},
			want:   "",
		},
		{
			// A setup-token belongs to the Claude account, so every Claude
			// preset shares one slot — never CLAUDE-CODE_1_API_KEY.
			name:     "claude-code → shared setup-token slot, not numbered",
			preset:   pp("claude-code", ""),
			existing: map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "t"},
			want:     "CLAUDE_CODE_OAUTH_TOKEN",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := AutoEnvVarName(c.preset, c.existing)
			if got != c.want {
				t.Errorf("AutoEnvVarName: got %q, want %q", got, c.want)
			}
		})
	}
}

// TestFamilyTemplateCapabilities pins the template capability defaults: no
// per-vendor capability providers — web_search uses DuckDuckGo and vision
// inherits the agent's own provider — for every template that can serve
// them. The claude template stays skills-only.
func TestFamilyTemplateCapabilities(t *testing.T) {
	for _, p := range BuiltinPresets() {
		caps, ok := p.Manifest["capabilities"].(map[string]interface{})
		if !ok {
			t.Fatalf("%s manifest.capabilities missing or wrong type: %T", p.Name, p.Manifest["capabilities"])
		}
		if _, ok := caps["skills"]; !ok {
			t.Errorf("%s must keep the skills default", p.Name)
		}
		if p.Name == "claude" {
			if len(caps) != 1 {
				t.Errorf("claude capabilities = %#v, want skills only", caps)
			}
			continue
		}
		if got := caps["web_search"]; !reflect.DeepEqual(got, map[string]interface{}{"provider": "duckduckgo"}) {
			t.Errorf("%s web_search = %#v, want duckduckgo", p.Name, got)
		}
		if got := caps["vision"]; !reflect.DeepEqual(got, map[string]interface{}{"provider": "inherit"}) {
			t.Errorf("%s vision = %#v, want inherit", p.Name, got)
		}
	}
}

// TestGenerateInitJSONStripsRetiredAPICompat proves init.json generation never
// writes the retired manifest.llm.api_compat field, even from a legacy saved
// preset that still carries it.
func TestGenerateInitJSONStripsRetiredAPICompat(t *testing.T) {
	tmp := t.TempDir()
	lingtaiDir := filepath.Join(tmp, ".lingtai")
	globalDir := filepath.Join(tmp, "global")
	p := openaiPreset()
	llm := p.Manifest["llm"].(map[string]interface{})
	llm["model"] = "test-model"
	llm["api_compat"] = "openai"
	if err := GenerateInitJSON(p, "alice", "alice", lingtaiDir, globalDir); err != nil {
		t.Fatalf("GenerateInitJSON: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(lingtaiDir, "alice", "init.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "api_compat") {
		t.Fatalf("generated init.json carries api_compat:\n%s", data)
	}
	if !strings.Contains(string(data), `"wire_api": "chat_completions"`) {
		t.Fatalf("generated init.json lost wire_api:\n%s", data)
	}
}

func TestCredentialFamilyAliases(t *testing.T) {
	tests := []struct {
		provider string
		want     CredentialFamily
	}{
		{"codex", CredentialFamilyCodexSingle},
		{"codex_oauth", CredentialFamilyCodexSingle},
		{"codex-pool", CredentialFamilyOther},
		{"codex_pool", CredentialFamilyOther},
		{"claude-code", CredentialFamilyClaude},
		{"claude_code", CredentialFamilyClaude},
		{"claude-agent-sdk", CredentialFamilyClaude},
		{"claude_agent_sdk", CredentialFamilyClaude},
		{"codex.json", CredentialFamilyOther},
		{"custom", CredentialFamilyOther},
	}
	for _, tc := range tests {
		t.Run(tc.provider, func(t *testing.T) {
			if got := ClassifyCredentialFamily(tc.provider); got != tc.want {
				t.Fatalf("ClassifyCredentialFamily(%q) = %q, want %q", tc.provider, got, tc.want)
			}
		})
	}
}

func TestResolveRefs_ManifestFamilyDispatchAndFailClosed(t *testing.T) {
	dir := t.TempDir()
	authDir := t.TempDir()
	writeStubTokenFile(t, filepath.Join(authDir, "codex-auth.json"))

	codexOAuthRef := writePresetFile(t, dir, "codex-oauth", "codex_oauth", "STALE_CODEX_KEY")
	got := ResolveRefsWithAuth([]string{codexOAuthRef}, map[string]string{"STALE_CODEX_KEY": "must-not-be-used"}, AuthState{CodexAuthDir: authDir})
	if len(got) != 1 || got[0].Family != CredentialFamilyCodexSingle || got[0].Provider != "codex_oauth" || !got[0].HasKey {
		t.Fatalf("codex_oauth resolution = %#v, want manifest-owned CodexSingle auth", got)
	}

	malformed := filepath.Join(dir, "codex.json")
	if err := os.WriteFile(malformed, []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	got = ResolveRefsWithAuth([]string{malformed}, nil, AuthState{CodexOAuthConfigured: true})
	if len(got) != 1 {
		t.Fatalf("malformed resolution length = %d, want 1", len(got))
	}
	if got[0].ManifestValid || got[0].Family != CredentialFamilyOther || got[0].HasKey {
		t.Fatalf("malformed codex path failed open: %#v", got[0])
	}
}

func TestResolvePresetWithAuthPreservesKeyedAndLocalBehavior(t *testing.T) {
	keyed := Preset{Manifest: map[string]interface{}{"llm": map[string]interface{}{
		"provider": "custom", "model": "local", "api_key_env": "CUSTOM_TEST_KEY",
	}}}
	got := ResolvePresetWithAuth(keyed, map[string]string{"CUSTOM_TEST_KEY": "present"}, AuthState{})
	if got.Family != CredentialFamilyOther || !got.HasKey {
		t.Fatalf("keyed custom resolution = %#v, want key-backed Other", got)
	}
	local := Preset{Manifest: map[string]interface{}{"llm": map[string]interface{}{
		"provider": "custom", "model": "local",
	}}}
	got = ResolvePresetWithAuth(local, nil, AuthState{})
	if got.Family != CredentialFamilyOther || got.HasKey {
		t.Fatalf("keyless custom/local resolution = %#v, want unchanged no-key behavior", got)
	}
}

// TestValidateAcceptsEmptyBaseURLForEveryFamily: base_url is optional in the
// four-family model — openai/anthropic fall back to the official endpoint and
// codex/claude-code own their routes — so an empty or absent endpoint is never
// a structural violation.
func TestValidateAcceptsEmptyBaseURLForEveryFamily(t *testing.T) {
	for _, provider := range []string{ProviderOpenAI, ProviderAnthropic, ProviderCodex, ProviderClaudeCode} {
		for _, baseURL := range []interface{}{nil, "", "https://gateway.example.com/v1"} {
			llm := map[string]interface{}{"provider": provider, "model": "some-model"}
			if baseURL != nil {
				llm["base_url"] = baseURL
			}
			p := Preset{
				Name:        provider,
				Description: PresetDescription{Summary: "test preset"},
				Manifest:    map[string]interface{}{"llm": llm},
			}
			if errs := p.Validate(); len(errs) != 0 {
				t.Errorf("%s with base_url %#v = %v, want no violations", provider, baseURL, errs)
			}
		}
	}
}

// TestValidateModelOptionalOnlyForClaudeCode: claude-code runs Claude Code's
// own default model, so an absent model is valid there and nowhere else.
func TestValidateModelOptionalOnlyForClaudeCode(t *testing.T) {
	for _, tc := range []struct {
		provider string
		wantErr  bool
	}{
		{ProviderClaudeCode, false},
		{ProviderOpenAI, true},
		{ProviderAnthropic, true},
		{ProviderCodex, true},
	} {
		p := Preset{
			Name:        "p",
			Description: PresetDescription{Summary: "test preset"},
			Manifest:    map[string]interface{}{"llm": map[string]interface{}{"provider": tc.provider}},
		}
		gotErr := false
		for _, err := range p.Validate() {
			if err.Error() == "manifest.llm.model must be non-empty" {
				gotErr = true
			}
		}
		if gotErr != tc.wantErr {
			t.Errorf("%s without model: model error = %v, want %v", tc.provider, gotErr, tc.wantErr)
		}
	}
}

func TestAPIKeyEnvName(t *testing.T) {
	for _, tc := range []struct {
		name string
		llm  map[string]interface{}
		want string
	}{
		{"declared slot wins", map[string]interface{}{"provider": "openai", "api_key_env": "OPENAI_1_API_KEY"}, "OPENAI_1_API_KEY"},
		{"keyless openai stays empty", map[string]interface{}{"provider": "openai"}, ""},
		{"claude declared slot", map[string]interface{}{"provider": "claude-code", "api_key_env": "CLAUDE_WORK_TOKEN"}, "CLAUDE_WORK_TOKEN"},
		{"claude legacy empty slot → default", map[string]interface{}{"provider": "claude-code", "api_key_env": ""}, ClaudeCodeOAuthTokenEnv},
		{"codex never uses an env slot", map[string]interface{}{"provider": "codex", "api_key_env": "STALE_KEY"}, ""},
	} {
		if got := APIKeyEnvName(tc.llm); got != tc.want {
			t.Errorf("%s: APIKeyEnvName = %q, want %q", tc.name, got, tc.want)
		}
	}
}
