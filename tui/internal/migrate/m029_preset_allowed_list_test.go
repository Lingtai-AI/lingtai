package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// helper: write a preset file under dir and return its absolute path.
func writePreset(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	p := filepath.Join(dir, name)
	body := `{"name":"x","description":{"summary":"x"},"manifest":{"llm":{"provider":"x","model":"y"}}}`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

// helper: read manifest.preset block from agent init.json.
func readPresetBlock(t *testing.T, initPath string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(initPath)
	if err != nil {
		t.Fatalf("read %s: %v", initPath, err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal %s: %v", initPath, err)
	}
	manifest, ok := m["manifest"].(map[string]interface{})
	if !ok {
		t.Fatalf("manifest missing in %s", initPath)
	}
	preset, ok := manifest["preset"].(map[string]interface{})
	if !ok {
		t.Fatalf("preset missing in %s", initPath)
	}
	return preset
}

// TestMigratePresetAllowedList_DropsPathAndPopulatesAllowed verifies the
// happy path: an agent with manifest.preset.path pointing at a directory
// of *.json presets gets every preset rewritten into `allowed`.
func TestMigratePresetAllowedList_DropsPathAndPopulatesAllowed(t *testing.T) {
	tmp := t.TempDir()
	lingtaiDir := filepath.Join(tmp, ".lingtai")
	if err := os.MkdirAll(lingtaiDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Two presets in a library directory.
	libDir := filepath.Join(tmp, "presets")
	alpha := writePreset(t, libDir, "alpha.json")
	beta := writePreset(t, libDir, "beta.json")

	// Build an agent init.json with the legacy `path` shape.
	agentDir := filepath.Join(lingtaiDir, "agent")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	init := map[string]interface{}{
		"manifest": map[string]interface{}{
			"preset": map[string]interface{}{
				"active":  alpha,
				"default": alpha,
				"path":    libDir,
			},
		},
	}
	body, _ := json.MarshalIndent(init, "", "  ")
	initPath := filepath.Join(agentDir, "init.json")
	if err := os.WriteFile(initPath, body, 0o644); err != nil {
		t.Fatalf("write init: %v", err)
	}

	if err := migratePresetAllowedList(lingtaiDir); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	preset := readPresetBlock(t, initPath)

	if _, has := preset["path"]; has {
		t.Errorf("preset.path should be dropped, got %v", preset["path"])
	}
	allowed, ok := preset["allowed"].([]interface{})
	if !ok {
		t.Fatalf("preset.allowed missing or wrong type: %T", preset["allowed"])
	}
	if len(allowed) != 2 {
		t.Errorf("expected 2 entries in allowed, got %d: %v", len(allowed), allowed)
	}
	gotAlpha, gotBeta := false, false
	for _, e := range allowed {
		s, _ := e.(string)
		// homeShorten may rewrite under $HOME — both raw absolute and
		// $HOME-normalized forms are acceptable here.
		if s == alpha || filepath.Base(s) == "alpha.json" {
			gotAlpha = true
		}
		if s == beta || filepath.Base(s) == "beta.json" {
			gotBeta = true
		}
	}
	if !gotAlpha || !gotBeta {
		t.Errorf("allowed missing presets: %v", allowed)
	}
}

// TestMigratePresetAllowedList_AppendsDefaultActiveIfOutsideLib verifies
// that `default` and `active` entries that point outside the library
// directory still end up in `allowed`.
func TestMigratePresetAllowedList_AppendsDefaultActiveIfOutsideLib(t *testing.T) {
	tmp := t.TempDir()
	lingtaiDir := filepath.Join(tmp, ".lingtai")
	if err := os.MkdirAll(lingtaiDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	libDir := filepath.Join(tmp, "presets")
	writePreset(t, libDir, "alpha.json")
	// `active` lives outside libDir entirely.
	outside := writePreset(t, filepath.Join(tmp, "elsewhere"), "custom.json")

	agentDir := filepath.Join(lingtaiDir, "agent")
	os.MkdirAll(agentDir, 0o755)
	init := map[string]interface{}{
		"manifest": map[string]interface{}{
			"preset": map[string]interface{}{
				"active":  outside,
				"default": outside,
				"path":    libDir,
			},
		},
	}
	body, _ := json.MarshalIndent(init, "", "  ")
	initPath := filepath.Join(agentDir, "init.json")
	os.WriteFile(initPath, body, 0o644)

	if err := migratePresetAllowedList(lingtaiDir); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	preset := readPresetBlock(t, initPath)
	allowed, _ := preset["allowed"].([]interface{})

	gotAlpha, gotOutside := false, false
	for _, e := range allowed {
		s, _ := e.(string)
		if filepath.Base(s) == "alpha.json" {
			gotAlpha = true
		}
		if s == outside || filepath.Base(s) == "custom.json" {
			gotOutside = true
		}
	}
	if !gotAlpha {
		t.Errorf("allowed missing alpha (from libDir): %v", allowed)
	}
	if !gotOutside {
		t.Errorf("allowed missing outside default/active path: %v", allowed)
	}
}

// TestMigratePresetAllowedList_Idempotent verifies that running the
// migration twice on the new shape is a no-op.
func TestMigratePresetAllowedList_Idempotent(t *testing.T) {
	tmp := t.TempDir()
	lingtaiDir := filepath.Join(tmp, ".lingtai")
	os.MkdirAll(lingtaiDir, 0o755)

	agentDir := filepath.Join(lingtaiDir, "agent")
	os.MkdirAll(agentDir, 0o755)
	init := map[string]interface{}{
		"manifest": map[string]interface{}{
			"preset": map[string]interface{}{
				"active":  "~/.lingtai-tui/presets/alpha.json",
				"default": "~/.lingtai-tui/presets/alpha.json",
				"allowed": []interface{}{"~/.lingtai-tui/presets/alpha.json"},
			},
		},
	}
	body, _ := json.MarshalIndent(init, "", "  ")
	initPath := filepath.Join(agentDir, "init.json")
	os.WriteFile(initPath, body, 0o644)

	original, _ := os.ReadFile(initPath)

	if err := migratePresetAllowedList(lingtaiDir); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := migratePresetAllowedList(lingtaiDir); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	after, _ := os.ReadFile(initPath)
	// Allow whitespace differences from MarshalIndent — compare parsed.
	var origData, afterData map[string]interface{}
	json.Unmarshal(original, &origData)
	json.Unmarshal(after, &afterData)
	origJSON, _ := json.Marshal(origData)
	afterJSON, _ := json.Marshal(afterData)
	if string(origJSON) != string(afterJSON) {
		t.Errorf("idempotency violated:\n  before: %s\n  after:  %s", origJSON, afterJSON)
	}
}

// TestMigratePresetAllowedList_NoPresetBlockNoOp verifies that agents
// without a preset block are left alone (presetless agents have no
// allowed list to populate).
func TestMigratePresetAllowedList_NoPresetBlockNoOp(t *testing.T) {
	tmp := t.TempDir()
	lingtaiDir := filepath.Join(tmp, ".lingtai")
	os.MkdirAll(lingtaiDir, 0o755)

	agentDir := filepath.Join(lingtaiDir, "agent")
	os.MkdirAll(agentDir, 0o755)
	init := map[string]interface{}{
		"manifest": map[string]interface{}{
			"llm": map[string]interface{}{"provider": "x", "model": "y"},
		},
	}
	body, _ := json.MarshalIndent(init, "", "  ")
	initPath := filepath.Join(agentDir, "init.json")
	os.WriteFile(initPath, body, 0o644)

	original, _ := os.ReadFile(initPath)
	if err := migratePresetAllowedList(lingtaiDir); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	after, _ := os.ReadFile(initPath)
	if string(original) != string(after) {
		t.Errorf("non-preset agent should be untouched")
	}
}

// TestMigratePresetAllowedList_AcceptsPathAsList verifies that the legacy
// `path` field as a list of directories is enumerated correctly.
func TestMigratePresetAllowedList_AcceptsPathAsList(t *testing.T) {
	tmp := t.TempDir()
	lingtaiDir := filepath.Join(tmp, ".lingtai")
	os.MkdirAll(lingtaiDir, 0o755)

	libA := filepath.Join(tmp, "lib_a")
	libB := filepath.Join(tmp, "lib_b")
	writePreset(t, libA, "alpha.json")
	writePreset(t, libB, "beta.json")

	agentDir := filepath.Join(lingtaiDir, "agent")
	os.MkdirAll(agentDir, 0o755)
	init := map[string]interface{}{
		"manifest": map[string]interface{}{
			"preset": map[string]interface{}{
				"active":  filepath.Join(libA, "alpha.json"),
				"default": filepath.Join(libA, "alpha.json"),
				"path":    []interface{}{libA, libB},
			},
		},
	}
	body, _ := json.MarshalIndent(init, "", "  ")
	initPath := filepath.Join(agentDir, "init.json")
	os.WriteFile(initPath, body, 0o644)

	if err := migratePresetAllowedList(lingtaiDir); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	preset := readPresetBlock(t, initPath)
	allowed, _ := preset["allowed"].([]interface{})

	got := map[string]bool{}
	for _, e := range allowed {
		if s, ok := e.(string); ok {
			got[filepath.Base(s)] = true
		}
	}
	if !got["alpha.json"] || !got["beta.json"] {
		t.Errorf("expected alpha.json and beta.json in allowed; got %v", allowed)
	}
}
