package preset

import (
	"encoding/json"
	"os"
	"path/filepath"
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
		if len(presets) != 10 {
			t.Fatalf("expected 10 presets, got %d", len(presets))
		}
		names := map[string]bool{}
		for _, p := range presets {
			names[p.Name] = true
			if p.Source != SourceTemplate {
				t.Errorf("preset %q: Source = %v, want SourceTemplate", p.Name, p.Source)
			}
		}
		for _, want := range []string{"minimax", "zhipu", "mimo", "deepseek", "gemini", "kimi", "nvidia", "openrouter", "codex", "custom"} {
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

func TestCodexPresetDefaultOmitsServiceTier(t *testing.T) {
	p := codexPreset()
	llm, ok := p.Manifest["llm"].(map[string]interface{})
	if !ok {
		t.Fatalf("codex manifest.llm missing or wrong type: %T", p.Manifest["llm"])
	}
	if _, ok := llm["service_tier"]; ok {
		t.Fatalf("codex preset default should omit llm.service_tier; got %#v", llm["service_tier"])
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
