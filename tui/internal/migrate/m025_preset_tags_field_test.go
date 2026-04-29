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

func TestM025BackfillsTagsOnLegacyPreset(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	presetsDir := filepath.Join(tmp, ".lingtai-tui", "presets")

	// Legacy preset with no `tags` field at all.
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

	// Run the migration. lingtaiDir is unused by m025, but the signature
	// requires it — match the convention of other migrations.
	if err := migratePresetTagsField(filepath.Join(tmp, ".lingtai")); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	got := readPresetRaw(t, presetsDir, "openrouter")
	tags, ok := got["tags"].([]interface{})
	if !ok {
		t.Fatalf("expected `tags` to be a list, got %T (%v)", got["tags"], got["tags"])
	}
	if len(tags) != 0 {
		t.Errorf("expected empty tags list, got %v", tags)
	}
}

func TestM025LeavesPresetsWithExistingTagsAlone(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	presetsDir := filepath.Join(tmp, ".lingtai-tui", "presets")

	// Preset that already declares tags — even non-empty — should be untouched.
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

	if err := migratePresetTagsField(filepath.Join(tmp, ".lingtai")); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	got := readPresetRaw(t, presetsDir, "deepseek_pro")
	tags, ok := got["tags"].([]interface{})
	if !ok {
		t.Fatalf("tags missing or wrong type: %v", got["tags"])
	}
	if len(tags) != 2 || tags[0] != "tier:4" || tags[1] != "specialty:code" {
		t.Errorf("tags were modified: %v", tags)
	}
}

func TestM025LeavesEmptyExistingTagsAlone(t *testing.T) {
	// Same as above but with explicitly-empty existing tags. Migration must
	// not rewrite the file (idempotent).
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	presetsDir := filepath.Join(tmp, ".lingtai-tui", "presets")

	writePresetRaw(t, presetsDir, "blank", map[string]interface{}{
		"name": "blank",
		"tags": []interface{}{},
		"manifest": map[string]interface{}{
			"llm":          map[string]interface{}{"provider": "p", "model": "m"},
			"capabilities": map[string]interface{}{},
		},
	})
	pre, _ := os.Stat(filepath.Join(presetsDir, "blank.json"))
	preMtime := pre.ModTime()

	if err := migratePresetTagsField(filepath.Join(tmp, ".lingtai")); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	post, _ := os.Stat(filepath.Join(presetsDir, "blank.json"))
	if !post.ModTime().Equal(preMtime) {
		t.Errorf("file was rewritten despite already having tags: pre=%v post=%v",
			preMtime, post.ModTime())
	}
}

func TestM025IsIdempotent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	presetsDir := filepath.Join(tmp, ".lingtai-tui", "presets")

	writePresetRaw(t, presetsDir, "p1", map[string]interface{}{
		"name":     "p1",
		"manifest": map[string]interface{}{"llm": map[string]interface{}{"provider": "x", "model": "y"}, "capabilities": map[string]interface{}{}},
	})

	// First run backfills.
	if err := migratePresetTagsField(filepath.Join(tmp, ".lingtai")); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	post1, _ := os.Stat(filepath.Join(presetsDir, "p1.json"))
	mtime1 := post1.ModTime()

	// Second run must be a no-op (file unchanged).
	if err := migratePresetTagsField(filepath.Join(tmp, ".lingtai")); err != nil {
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

	// _kernel_meta.json sits in the same dir but is not a preset.
	metaPath := filepath.Join(presetsDir, "_kernel_meta.json")
	metaContent := `{"version":1}`
	if err := os.WriteFile(metaPath, []byte(metaContent), 0o644); err != nil {
		t.Fatalf("write meta: %v", err)
	}

	if err := migratePresetTagsField(filepath.Join(tmp, ".lingtai")); err != nil {
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
	// No ~/.lingtai-tui/presets/ directory at all — fresh install.
	if err := migratePresetTagsField(filepath.Join(tmp, ".lingtai")); err != nil {
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
	// Plant a corrupt file alongside a valid one.
	os.WriteFile(filepath.Join(presetsDir, "broken.json"), []byte("{ not json"), 0o644)
	writePresetRaw(t, presetsDir, "good", map[string]interface{}{
		"name":     "good",
		"manifest": map[string]interface{}{"llm": map[string]interface{}{"provider": "x", "model": "y"}, "capabilities": map[string]interface{}{}},
	})

	if err := migratePresetTagsField(filepath.Join(tmp, ".lingtai")); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// The good preset still gets backfilled.
	got := readPresetRaw(t, presetsDir, "good")
	if _, ok := got["tags"]; !ok {
		t.Errorf("good preset missing tags field after migration")
	}
}
