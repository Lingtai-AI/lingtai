package preset

import (
	"encoding/json"
	"os"
	"path/filepath"
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
		p := DefaultPreset()
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
		if len(presets) != 12 {
			t.Fatalf("expected 12 presets, got %d", len(presets))
		}
		names := map[string]bool{}
		for _, p := range presets {
			names[p.Name] = true
			if p.Source != SourceTemplate {
				t.Errorf("preset %q: Source = %v, want SourceTemplate", p.Name, p.Source)
			}
		}
		for _, want := range []string{"minimax", "zhipu", "mimo", "deepseek", "gemini", "kimi", "nvidia", "openrouter", "codex", "codex-pool", "claude-agent-sdk", "custom"} {
			if !names[want] {
				t.Errorf("missing preset %q", want)
			}
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
	claudeRef := writePresetFile(t, dir, "claude-agent-sdk", "claude-agent-sdk", "")
	claudeUnderscoreRef := writePresetFile(t, dir, "claude_agent_sdk", "claude_agent_sdk", "")
	customRef := writePresetFile(t, dir, "custom", "custom", "")
	keyedRef := writePresetFile(t, dir, "minimax", "minimax", "FOO_API_KEY")
	missingRef := filepath.Join(dir, "nope.json")

	keysWith := map[string]string{"FOO_API_KEY": "placeholder-value"}
	keysEmpty := map[string]string{}

	cases := []struct {
		name       string
		ref        string
		keys       map[string]string
		auth       AuthState
		wantExists bool
		wantHasKey bool
	}{
		{"codex no OAuth", codexRef, keysEmpty, AuthState{}, true, false},
		{"codex with OAuth", codexRef, keysEmpty, AuthState{CodexOAuthConfigured: true}, true, true},
		{"claude-agent-sdk no CLI auth", claudeRef, keysEmpty, AuthState{}, true, false},
		{"claude-agent-sdk with CLI auth", claudeRef, keysEmpty, AuthState{ClaudeCodeAuthConfigured: true}, true, true},
		{"claude_agent_sdk alias with CLI auth", claudeUnderscoreRef, keysEmpty, AuthState{ClaudeCodeAuthConfigured: true}, true, true},
		{"claude-agent-sdk ignores codex OAuth", claudeRef, keysEmpty, AuthState{CodexOAuthConfigured: true}, true, false},
		{"keyless non-codex is invalid", customRef, keysEmpty, AuthState{}, true, false},
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

	claudeRef := writePresetFile(t, dir, "claude-agent-sdk", "claude-agent-sdk", "")
	got = ResolveRefs([]string{claudeRef}, nil)
	if len(got) != 1 {
		t.Fatalf("expected 1 resolved ref, got %d", len(got))
	}
	if got[0].HasKey {
		t.Errorf("claude-agent-sdk via ResolveRefs: HasKey = true, want false (conservative default)")
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

func writeStubTokenFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"refresh_token":"stub-refresh"}`), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
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

// TestResolveRefs_CodexFallsBackToGlobalBoolWithoutDir verifies that when
// CodexAuthDir is empty, codex validity falls back to the legacy global bool
// (backward compatibility for callers that don't set the dir).
func TestResolveRefs_CodexFallsBackToGlobalBoolWithoutDir(t *testing.T) {
	dir := t.TempDir()
	codexRef := writeCodexPresetWithAuthPath(t, dir, "codex", "")
	if got := ResolveRefsWithAuth([]string{codexRef}, nil, AuthState{CodexOAuthConfigured: true}); !got[0].HasKey {
		t.Error("with CodexOAuthConfigured=true and no dir, codex should be valid")
	}
	if got := ResolveRefsWithAuth([]string{codexRef}, nil, AuthState{CodexOAuthConfigured: false}); got[0].HasKey {
		t.Error("with CodexOAuthConfigured=false and no dir, codex should be invalid")
	}
}

func TestGenerateInitJSON_ProducesValidJSON(t *testing.T) {
	withTempPresets(t, func() {
		p := DefaultPreset()
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
		{"zhipu", zhipuPreset(), "GLM-5.2"},
		{"deepseek", deepseekPreset(), "deepseek-v4-pro"},
		{"codex", codexPreset(), "gpt-5.6-sol"},
		{"codex-pool", codexPoolPreset(), "gpt-5.6-sol"},
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

func TestClaudeAgentSDKPresetShape(t *testing.T) {
	p := claudeAgentSDKPreset()
	if p.Name != "claude-agent-sdk" {
		t.Fatalf("name = %q, want claude-agent-sdk", p.Name)
	}
	llm, ok := p.Manifest["llm"].(map[string]interface{})
	if !ok {
		t.Fatalf("manifest.llm missing or wrong type: %T", p.Manifest["llm"])
	}
	if got := llm["provider"]; got != "claude-agent-sdk" {
		t.Errorf("llm.provider = %v, want claude-agent-sdk", got)
	}
	// Default to the CLI alias, never a dated API model id.
	if got := llm["model"]; got != "opus" {
		t.Errorf("llm.model = %v, want opus", got)
	}
	// Authenticates via the local Claude CLI: no api_key, no api_key_env.
	if got, ok := llm["api_key"]; !ok || got != nil {
		t.Errorf("llm.api_key = %v (present=%v), want nil", got, ok)
	}
	if got := llm["api_key_env"]; got != "" {
		t.Errorf("llm.api_key_env = %v, want empty string", got)
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
		t.Errorf("capabilities.web_search should be absent for claude-agent-sdk")
	}
	if _, ok := caps["vision"]; ok {
		t.Errorf("capabilities.vision should be absent for claude-agent-sdk")
	}
}

func TestClaudeAgentSDKPresetIsBuiltin(t *testing.T) {
	if !IsBuiltin("claude-agent-sdk") {
		t.Errorf("IsBuiltin(claude-agent-sdk) = false, want true")
	}
	found := false
	for _, p := range BuiltinPresets() {
		if p.Name == "claude-agent-sdk" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("claude-agent-sdk not present in BuiltinPresets()")
	}
}

func TestDelete_RemovesFile(t *testing.T) {
	withTempPresets(t, func() {
		p := DefaultPreset()
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
		Save(DefaultPreset())
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

	p := minimaxPreset()
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
	// Templates resolve to presets/templates/<name>.json; minimaxPreset()
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
			name:   "minimax CN, no existing → _1_",
			preset: pp("minimax", "https://api.minimaxi.com/anthropic"),
			want:   "MINIMAX_CN_1_API_KEY",
		},
		{
			name:   "minimax INTL, no existing",
			preset: pp("minimax", "https://api.minimax.io/anthropic"),
			want:   "MINIMAX_INTL_1_API_KEY",
		},
		{
			name:     "minimax CN with _1_ taken → gap-fill _2_",
			preset:   pp("minimax", "https://api.minimaxi.com/anthropic"),
			existing: map[string]string{"MINIMAX_CN_1_API_KEY": "k"},
			want:     "MINIMAX_CN_2_API_KEY",
		},
		{
			name:   "minimax CN with _1_ and _2_ taken, _3_ free",
			preset: pp("minimax", "https://api.minimaxi.com/anthropic"),
			existing: map[string]string{
				"MINIMAX_CN_1_API_KEY": "k",
				"MINIMAX_CN_2_API_KEY": "k",
			},
			want: "MINIMAX_CN_3_API_KEY",
		},
		{
			name:   "gap fill: _1_ taken, _2_ free, _3_ taken → returns _2_",
			preset: pp("minimax", "https://api.minimaxi.com/anthropic"),
			existing: map[string]string{
				"MINIMAX_CN_1_API_KEY": "k",
				"MINIMAX_CN_3_API_KEY": "k",
			},
			want: "MINIMAX_CN_2_API_KEY",
		},
		{
			name:   "deepseek has no region",
			preset: pp("deepseek", "https://api.deepseek.com"),
			want:   "DEEPSEEK_1_API_KEY",
		},
		{
			name:   "non-numeric existing entries (e.g. legacy) ignored",
			preset: pp("deepseek", "https://api.deepseek.com"),
			existing: map[string]string{
				"DEEPSEEK_API_KEY":      "legacy",
				"DEEPSEEK_PROD_API_KEY": "legacy",
			},
			want: "DEEPSEEK_1_API_KEY",
		},
		{
			name:   "zhipu CN default",
			preset: pp("zhipu", "https://open.bigmodel.cn/api/coding/paas/v4"),
			want:   "ZHIPU_CN_1_API_KEY",
		},
		{
			name:   "zhipu INTL via api.z.ai",
			preset: pp("zhipu", "https://api.z.ai/api/coding/paas/v4"),
			want:   "ZHIPU_INTL_1_API_KEY",
		},
		{
			name:   "no provider → empty",
			preset: Preset{Manifest: map[string]interface{}{"llm": map[string]interface{}{}}},
			want:   "",
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

func TestMiniMaxPresetCapabilitiesUseApiKeyEnv(t *testing.T) {
	p := minimaxPreset()
	manifest := p.Manifest
	caps, ok := manifest["capabilities"].(map[string]interface{})
	if !ok {
		t.Fatalf("manifest.capabilities missing or wrong type: %T", manifest["capabilities"])
	}
	for _, name := range []string{"web_search", "vision"} {
		capCfg, ok := caps[name].(map[string]interface{})
		if !ok {
			t.Fatalf("capability %s missing or wrong type: %T", name, caps[name])
		}
		if provider, _ := capCfg["provider"].(string); provider != "minimax" {
			t.Errorf("%s.provider = %v, want minimax", name, capCfg["provider"])
		}
		if env, _ := capCfg["api_key_env"].(string); env != "MINIMAX_API_KEY" {
			t.Errorf("%s.api_key_env = %v, want MINIMAX_API_KEY", name, capCfg["api_key_env"])
		}
	}
}
