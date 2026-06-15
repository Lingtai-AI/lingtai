package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// writeAgentInit writes an agent init.json under .lingtai/<name>/init.json.
func writeAgentInit(t *testing.T, lingtaiDir, name, body string) string {
	t.Helper()
	dir := filepath.Join(lingtaiDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	p := filepath.Join(dir, "init.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

func readManifest(t *testing.T, initPath string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(initPath)
	if err != nil {
		t.Fatalf("read %s: %v", initPath, err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal %s: %v\n%s", initPath, err, data)
	}
	manifest, ok := m["manifest"].(map[string]interface{})
	if !ok {
		t.Fatalf("manifest missing in %s", initPath)
	}
	return manifest
}

func toStringSlice(t *testing.T, raw interface{}) []string {
	t.Helper()
	items, ok := raw.([]interface{})
	if !ok {
		t.Fatalf("allowed is %T, want []interface{}", raw)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, ok := item.(string)
		if !ok {
			t.Fatalf("allowed entry is %T, want string", item)
		}
		out = append(out, s)
	}
	return out
}

// TestM038_LegacyRootContextCopiedToLLM verifies the incident's core repair:
// a legacy root manifest.context_limit with no manifest.llm.context_limit gets
// the value copied into manifest.llm.context_limit.
func TestM038_LegacyRootContextCopiedToLLM(t *testing.T) {
	tmp := t.TempDir()
	lingtaiDir := filepath.Join(tmp, ".lingtai")
	initPath := writeAgentInit(t, lingtaiDir, "alice", `{
  "manifest": {
    "context_limit": 300000,
    "llm": {"provider": "x", "model": "y"}
  }
}`)

	if err := migrateAgentInitContextPresetRepair(lingtaiDir); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	manifest := readManifest(t, initPath)
	llm := manifest["llm"].(map[string]interface{})
	if got := llm["context_limit"]; got != float64(300000) {
		t.Fatalf("llm.context_limit = %#v, want 300000", got)
	}
	// Root context_limit preserved for backward compatibility.
	if got := manifest["context_limit"]; got != float64(300000) {
		t.Fatalf("root context_limit = %#v, want preserved 300000", got)
	}
}

// TestM038_ExistingLLMContextWinsOnConflict verifies that when both root and
// llm context_limit exist but conflict, the canonical llm value is preserved
// and never overwritten by the root value.

func TestM038_LegacyRootContextCreatesMissingLLMBlock(t *testing.T) {
	tmp := t.TempDir()
	lingtaiDir := filepath.Join(tmp, ".lingtai")
	initPath := writeAgentInit(t, lingtaiDir, "alice", `{
  "manifest": {
    "context_limit": 300000
  }
}`)

	if err := migrateAgentInitContextPresetRepair(lingtaiDir); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	manifest := readManifest(t, initPath)
	llm, ok := manifest["llm"].(map[string]interface{})
	if !ok {
		t.Fatalf("llm block not created: %#v", manifest)
	}
	if got := llm["context_limit"]; got != float64(300000) {
		t.Fatalf("llm.context_limit = %#v, want 300000", got)
	}
}

func TestM038_ExistingLLMContextWinsOnConflict(t *testing.T) {
	tmp := t.TempDir()
	lingtaiDir := filepath.Join(tmp, ".lingtai")
	initPath := writeAgentInit(t, lingtaiDir, "bob", `{
  "manifest": {
    "context_limit": 300000,
    "llm": {"provider": "x", "model": "y", "context_limit": 128000}
  }
}`)

	if err := migrateAgentInitContextPresetRepair(lingtaiDir); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	manifest := readManifest(t, initPath)
	llm := manifest["llm"].(map[string]interface{})
	if got := llm["context_limit"]; got != float64(128000) {
		t.Fatalf("llm.context_limit = %#v, want canonical 128000 preserved", got)
	}
}

// TestM038_StalePresetMigratedWhenReplacementExists verifies that stale
// codex.json active/default/allowed pointers are rewritten to the existing
// codex-gpt5.5.json replacement.
func TestM038_StalePresetMigratedWhenReplacementExists(t *testing.T) {
	tmp := t.TempDir()
	home := tmp // pretend HOME for tilde resolution
	t.Setenv("HOME", home)

	savedDir := filepath.Join(home, ".lingtai-tui", "presets", "saved")
	if err := os.MkdirAll(savedDir, 0o755); err != nil {
		t.Fatalf("mkdir saved: %v", err)
	}
	// The replacement exists; the stale codex.json does NOT.
	if err := os.WriteFile(filepath.Join(savedDir, "codex-gpt5.5.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write replacement: %v", err)
	}

	lingtaiDir := filepath.Join(tmp, ".lingtai")
	initPath := writeAgentInit(t, lingtaiDir, "carol", `{
  "manifest": {
    "llm": {"provider": "x", "model": "y", "context_limit": 300000},
    "preset": {
      "active": "~/.lingtai-tui/presets/saved/codex.json",
      "default": "~/.lingtai-tui/presets/saved/codex.json",
      "allowed": ["~/.lingtai-tui/presets/saved/codex.json"]
    }
  }
}`)

	if err := migrateAgentInitContextPresetRepair(lingtaiDir); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	manifest := readManifest(t, initPath)
	preset := manifest["preset"].(map[string]interface{})
	want := "~/.lingtai-tui/presets/saved/codex-gpt5.5.json"
	if got := preset["active"]; got != want {
		t.Fatalf("active = %#v, want %q", got, want)
	}
	if got := preset["default"]; got != want {
		t.Fatalf("default = %#v, want %q", got, want)
	}
	allowed := preset["allowed"].([]interface{})
	if !reflect.DeepEqual(allowed, []interface{}{want}) {
		t.Fatalf("allowed = %#v, want [%q]", allowed, want)
	}
}

// TestM038_PreservesUnrelatedExistingAllowedEntries verifies the migration
// keeps unrelated allowed entries that resolve to existing files, only
// dropping the stale codex.json pointer.
func TestM038_PreservesUnrelatedExistingAllowedEntries(t *testing.T) {
	tmp := t.TempDir()
	home := tmp
	t.Setenv("HOME", home)

	savedDir := filepath.Join(home, ".lingtai-tui", "presets", "saved")
	templatesDir := filepath.Join(home, ".lingtai-tui", "presets", "templates")
	if err := os.MkdirAll(savedDir, 0o755); err != nil {
		t.Fatalf("mkdir saved: %v", err)
	}
	if err := os.MkdirAll(templatesDir, 0o755); err != nil {
		t.Fatalf("mkdir templates: %v", err)
	}
	for _, p := range []string{
		filepath.Join(savedDir, "codex-gpt5.5.json"),
		filepath.Join(templatesDir, "minimax.json"),
	} {
		if err := os.WriteFile(p, []byte("{}"), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	lingtaiDir := filepath.Join(tmp, ".lingtai")
	initPath := writeAgentInit(t, lingtaiDir, "dave", `{
  "manifest": {
    "llm": {"provider": "x", "model": "y", "context_limit": 300000},
    "preset": {
      "active": "~/.lingtai-tui/presets/saved/codex.json",
      "default": "~/.lingtai-tui/presets/templates/minimax.json",
      "allowed": [
        "~/.lingtai-tui/presets/saved/codex.json",
        "~/.lingtai-tui/presets/templates/minimax.json"
      ]
    }
  }
}`)

	if err := migrateAgentInitContextPresetRepair(lingtaiDir); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	manifest := readManifest(t, initPath)
	preset := manifest["preset"].(map[string]interface{})
	// active was stale -> rewritten to replacement
	if got := preset["active"]; got != "~/.lingtai-tui/presets/saved/codex-gpt5.5.json" {
		t.Fatalf("active = %#v, want replacement", got)
	}
	// default already pointed at an existing file -> untouched
	if got := preset["default"]; got != "~/.lingtai-tui/presets/templates/minimax.json" {
		t.Fatalf("default = %#v, want unchanged minimax", got)
	}
	allowed := preset["allowed"].([]interface{})
	want := []interface{}{
		"~/.lingtai-tui/presets/saved/codex-gpt5.5.json",
		"~/.lingtai-tui/presets/templates/minimax.json",
	}
	if !reflect.DeepEqual(allowed, want) {
		t.Fatalf("allowed = %#v, want %#v", allowed, want)
	}
}

// TestM038_SkipsNonAgentDirs verifies that dotfile dirs, the human mailbox,
// and dirs without init.json are not touched.
func TestM038_SkipsNonAgentDirs(t *testing.T) {
	tmp := t.TempDir()
	lingtaiDir := filepath.Join(tmp, ".lingtai")

	// human dir with an init.json-shaped file should still be skipped by name.
	humanInit := writeAgentInit(t, lingtaiDir, "human", `{"manifest":{"context_limit":300000,"llm":{}}}`)
	// dotfile-prefixed dir
	dotInit := writeAgentInit(t, lingtaiDir, ".portal", `{"manifest":{"context_limit":300000,"llm":{}}}`)
	// library dir without init.json — nothing to do, must not error
	libDir := filepath.Join(lingtaiDir, "library")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatalf("mkdir library: %v", err)
	}
	if err := os.WriteFile(filepath.Join(libDir, "note.md"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("write note: %v", err)
	}

	humanBefore, _ := os.ReadFile(humanInit)
	dotBefore, _ := os.ReadFile(dotInit)

	if err := migrateAgentInitContextPresetRepair(lingtaiDir); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	humanAfter, _ := os.ReadFile(humanInit)
	dotAfter, _ := os.ReadFile(dotInit)
	if string(humanBefore) != string(humanAfter) {
		t.Fatalf("human init.json mutated: %s", humanAfter)
	}
	if string(dotBefore) != string(dotAfter) {
		t.Fatalf(".portal init.json mutated: %s", dotAfter)
	}
}

// TestM038_Idempotent verifies a second run makes no further changes.
func TestM038_Idempotent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	savedDir := filepath.Join(tmp, ".lingtai-tui", "presets", "saved")
	if err := os.MkdirAll(savedDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(savedDir, "codex-gpt5.5.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	lingtaiDir := filepath.Join(tmp, ".lingtai")
	initPath := writeAgentInit(t, lingtaiDir, "erin", `{
  "manifest": {
    "context_limit": 300000,
    "llm": {"provider": "x", "model": "y"},
    "preset": {
      "active": "~/.lingtai-tui/presets/saved/codex.json",
      "default": "~/.lingtai-tui/presets/saved/codex.json",
      "allowed": ["~/.lingtai-tui/presets/saved/codex.json"]
    }
  }
}`)

	if err := migrateAgentInitContextPresetRepair(lingtaiDir); err != nil {
		t.Fatalf("migrate 1: %v", err)
	}
	after1, err := os.ReadFile(initPath)
	if err != nil {
		t.Fatalf("read after 1: %v", err)
	}
	if err := migrateAgentInitContextPresetRepair(lingtaiDir); err != nil {
		t.Fatalf("migrate 2: %v", err)
	}
	after2, err := os.ReadFile(initPath)
	if err != nil {
		t.Fatalf("read after 2: %v", err)
	}
	if string(after1) != string(after2) {
		t.Fatalf("second run changed file:\n--- 1 ---\n%s\n--- 2 ---\n%s", after1, after2)
	}
}

func TestM038_MissingActiveDefaultPresetGetsReplacementAllowed(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	replacement := filepath.Join(tmp, ".lingtai-tui", "presets", "saved", "codex-gpt5.5.json")
	if err := os.MkdirAll(filepath.Dir(replacement), 0o755); err != nil {
		t.Fatalf("mkdir replacement dir: %v", err)
	}
	if err := os.WriteFile(replacement, []byte(`{"name":"codex-gpt5.5"}`), 0o644); err != nil {
		t.Fatalf("write replacement: %v", err)
	}

	other := filepath.Join(tmp, "other-existing.json")
	if err := os.WriteFile(other, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("write other preset: %v", err)
	}

	lingtaiDir := filepath.Join(tmp, ".lingtai")
	initPath := writeAgentInit(t, lingtaiDir, "alice", `{
  "manifest": {
    "llm": {"provider": "x", "model": "y"},
    "preset": {
      "active": "~/missing-active.json",
      "default": "~/missing-default.json",
      "allowed": ["`+other+`"]
    }
  }
}`)

	if err := migrateAgentInitContextPresetRepair(lingtaiDir); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	manifest := readManifest(t, initPath)
	preset := manifest["preset"].(map[string]interface{})
	if preset["active"] != legacyCodexReplacement || preset["default"] != legacyCodexReplacement {
		t.Fatalf("active/default not repaired: %#v", preset)
	}
	allowed := toStringSlice(t, preset["allowed"])
	expected := []string{legacyCodexReplacement, other}
	if !reflect.DeepEqual(allowed, expected) {
		t.Fatalf("allowed = %#v, want %#v", allowed, expected)
	}
}
