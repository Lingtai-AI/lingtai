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

func TestRefreshTemplates_CreatesAllTemplates(t *testing.T) {
	withTempPresets(t, func() {
		if err := RefreshTemplates(); err != nil {
			t.Fatalf("RefreshTemplates() error: %v", err)
		}
		presets, _ := List()
		if len(presets) != 7 {
			t.Fatalf("expected 7 presets, got %d", len(presets))
		}
		names := map[string]bool{}
		for _, p := range presets {
			names[p.Name] = true
			if p.Source != SourceTemplate {
				t.Errorf("preset %q: Source = %v, want SourceTemplate", p.Name, p.Source)
			}
		}
		for _, want := range []string{"minimax", "zhipu", "mimo", "deepseek", "openrouter", "codex", "custom"} {
			if !names[want] {
				t.Errorf("missing preset %q", want)
			}
		}
	})
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

		// Check .agent.json exists
		agentPath := filepath.Join(lingtaiDir, "test-agent", ".agent.json")
		if _, err := os.Stat(agentPath); err != nil {
			t.Errorf(".agent.json not created: %v", err)
		}
	})
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
