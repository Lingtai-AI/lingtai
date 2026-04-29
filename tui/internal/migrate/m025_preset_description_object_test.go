package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// helper: write a preset JSON directly to the global library, simulating
// the on-disk shape an older TUI would have produced.
func writePresetRaw(t *testing.T, presetsDir, name string, body map[string]interface{}) {
	t.Helper()
	if err := os.MkdirAll(presetsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join(presetsDir, name+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func readPresetRaw(t *testing.T, presetsDir, name string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(presetsDir, name+".json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parse: %v", err)
	}
	return raw
}

func TestM025WrapsStringDescription(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	presetsDir := filepath.Join(tmp, ".lingtai-tui", "presets")

	writePresetRaw(t, presetsDir, "openrouter", map[string]interface{}{
		"name":        "openrouter",
		"description": "OpenAI-compatible API — full capabilities",
		"manifest": map[string]interface{}{
			"llm": map[string]interface{}{
				"provider": "custom", "model": "z-ai/glm-5.1",
			},
			"capabilities": map[string]interface{}{},
		},
	})

	if err := migratePresetDescriptionObject(filepath.Join(tmp, ".lingtai")); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	got := readPresetRaw(t, presetsDir, "openrouter")
	desc, ok := got["description"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected description to be an object, got %T", got["description"])
	}
	if desc["summary"] != "OpenAI-compatible API — full capabilities" {
		t.Errorf("summary = %v, want the original string", desc["summary"])
	}
}

func TestM025FoldsTierTagIntoDescription(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	presetsDir := filepath.Join(tmp, ".lingtai-tui", "presets")

	writePresetRaw(t, presetsDir, "deepseek_pro", map[string]interface{}{
		"name":        "deepseek_pro",
		"description": "DeepSeek V4 Pro",
		"tags":        []interface{}{"tier:4", "specialty:code"},
		"manifest": map[string]interface{}{
			"llm": map[string]interface{}{
				"provider": "deepseek", "model": "deepseek-v4-pro",
			},
			"capabilities": map[string]interface{}{},
		},
	})

	if err := migratePresetDescriptionObject(filepath.Join(tmp, ".lingtai")); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	got := readPresetRaw(t, presetsDir, "deepseek_pro")
	desc := got["description"].(map[string]interface{})
	if desc["summary"] != "DeepSeek V4 Pro" {
		t.Errorf("summary = %v, want preserved", desc["summary"])
	}
	if desc["tier"] != "4" {
		t.Errorf("tier = %v, want '4' folded from tags", desc["tier"])
	}
	if _, ok := got["tags"]; ok {
		t.Errorf("tags key should have been deleted, got %v", got["tags"])
	}
}

func TestM025SynthesizesEmptySummaryWhenMissing(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	presetsDir := filepath.Join(tmp, ".lingtai-tui", "presets")

	// No description at all.
	writePresetRaw(t, presetsDir, "nodesc", map[string]interface{}{
		"name": "nodesc",
		"manifest": map[string]interface{}{
			"llm":          map[string]interface{}{"provider": "p", "model": "m"},
			"capabilities": map[string]interface{}{},
		},
	})

	if err := migratePresetDescriptionObject(filepath.Join(tmp, ".lingtai")); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	got := readPresetRaw(t, presetsDir, "nodesc")
	desc, ok := got["description"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected description object, got %T", got["description"])
	}
	if s, _ := desc["summary"].(string); s != "" {
		t.Errorf("summary = %q, want empty (operator must fill in)", s)
	}
}

func TestM025LeavesAlreadyMigratedAlone(t *testing.T) {
	// Already in the new shape — no rewrite.
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	presetsDir := filepath.Join(tmp, ".lingtai-tui", "presets")

	writePresetRaw(t, presetsDir, "blank", map[string]interface{}{
		"name":        "blank",
		"description": map[string]interface{}{"summary": "ok", "tier": "3"},
		"manifest": map[string]interface{}{
			"llm":          map[string]interface{}{"provider": "p", "model": "m"},
			"capabilities": map[string]interface{}{},
		},
	})
	pre, _ := os.Stat(filepath.Join(presetsDir, "blank.json"))
	preMtime := pre.ModTime()

	if err := migratePresetDescriptionObject(filepath.Join(tmp, ".lingtai")); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	post, _ := os.Stat(filepath.Join(presetsDir, "blank.json"))
	if !post.ModTime().Equal(preMtime) {
		t.Errorf("file was rewritten despite already migrated: pre=%v post=%v",
			preMtime, post.ModTime())
	}
}

func TestM025IsIdempotent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	presetsDir := filepath.Join(tmp, ".lingtai-tui", "presets")

	writePresetRaw(t, presetsDir, "p1", map[string]interface{}{
		"name":        "p1",
		"description": "stringy",
		"tags":        []interface{}{"tier:2"},
		"manifest":    map[string]interface{}{"llm": map[string]interface{}{"provider": "x", "model": "y"}, "capabilities": map[string]interface{}{}},
	})

	if err := migratePresetDescriptionObject(filepath.Join(tmp, ".lingtai")); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	post1, _ := os.Stat(filepath.Join(presetsDir, "p1.json"))
	mtime1 := post1.ModTime()

	if err := migratePresetDescriptionObject(filepath.Join(tmp, ".lingtai")); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	post2, _ := os.Stat(filepath.Join(presetsDir, "p1.json"))
	if !post2.ModTime().Equal(mtime1) {
		t.Errorf("second run rewrote the file: %v vs %v", mtime1, post2.ModTime())
	}
}

func TestM025SkipsKernelMetaFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	presetsDir := filepath.Join(tmp, ".lingtai-tui", "presets")
	if err := os.MkdirAll(presetsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	metaPath := filepath.Join(presetsDir, "_kernel_meta.json")
	metaContent := `{"version":1}`
	if err := os.WriteFile(metaPath, []byte(metaContent), 0o644); err != nil {
		t.Fatalf("write meta: %v", err)
	}

	if err := migratePresetDescriptionObject(filepath.Join(tmp, ".lingtai")); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	got, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("re-read meta: %v", err)
	}
	if string(got) != metaContent {
		t.Errorf("kernel meta file was modified: %q", string(got))
	}
}

func TestM025NoLibraryDirIsNoOp(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if err := migratePresetDescriptionObject(filepath.Join(tmp, ".lingtai")); err != nil {
		t.Fatalf("migrate should not error on missing library: %v", err)
	}
}

func TestM025SkipsUnparseablePreset(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	presetsDir := filepath.Join(tmp, ".lingtai-tui", "presets")
	if err := os.MkdirAll(presetsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	os.WriteFile(filepath.Join(presetsDir, "broken.json"), []byte("{ not json"), 0o644)
	writePresetRaw(t, presetsDir, "good", map[string]interface{}{
		"name":        "good",
		"description": "fine",
		"manifest":    map[string]interface{}{"llm": map[string]interface{}{"provider": "x", "model": "y"}, "capabilities": map[string]interface{}{}},
	})

	if err := migratePresetDescriptionObject(filepath.Join(tmp, ".lingtai")); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	got := readPresetRaw(t, presetsDir, "good")
	desc, ok := got["description"].(map[string]interface{})
	if !ok {
		t.Errorf("good preset description not promoted: %T", got["description"])
	}
	if desc["summary"] != "fine" {
		t.Errorf("summary not preserved: %v", desc["summary"])
	}
}

func TestM025RewritesJsoncPresets(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	presetsDir := filepath.Join(tmp, ".lingtai-tui", "presets")
	if err := os.MkdirAll(presetsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	body := map[string]interface{}{
		"name":        "jc",
		"description": "jsonc preset",
		"manifest": map[string]interface{}{
			"llm":          map[string]interface{}{"provider": "x", "model": "y"},
			"capabilities": map[string]interface{}{},
		},
	}
	data, _ := json.MarshalIndent(body, "", "  ")
	jcPath := filepath.Join(presetsDir, "jc.jsonc")
	if err := os.WriteFile(jcPath, data, 0o644); err != nil {
		t.Fatalf("write jsonc: %v", err)
	}

	if err := migratePresetDescriptionObject(filepath.Join(tmp, ".lingtai")); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	got, err := os.ReadFile(jcPath)
	if err != nil {
		t.Fatalf("re-read jsonc: %v", err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(got, &raw); err != nil {
		t.Fatalf("re-parse jsonc: %v", err)
	}
	desc, ok := raw["description"].(map[string]interface{})
	if !ok {
		t.Errorf(".jsonc preset description not promoted: %T", raw["description"])
	}
	if desc["summary"] != "jsonc preset" {
		t.Errorf("jsonc summary not preserved: %v", desc["summary"])
	}
}
