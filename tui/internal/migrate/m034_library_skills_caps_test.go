package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeM034Doc(t *testing.T, path string, caps map[string]interface{}) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	doc := map[string]interface{}{
		"manifest": map[string]interface{}{
			"capabilities": caps,
		},
	}
	data, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write doc: %v", err)
	}
}

func readM034Caps(t *testing.T, path string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read doc: %v", err)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse doc: %v", err)
	}
	manifest, _ := doc["manifest"].(map[string]interface{})
	caps, _ := manifest["capabilities"].(map[string]interface{})
	return caps
}

func TestMigrateLibrarySkillsCaps_AgentInit(t *testing.T) {
	lingtaiDir := t.TempDir()
	initPath := filepath.Join(lingtaiDir, "orch", "init.json")
	writeM034Doc(t, initPath, map[string]interface{}{
		"codex":   map[string]interface{}{"codex_limit": float64(12)},
		"library": map[string]interface{}{"paths": []interface{}{"../.library_shared", "../../velli"}},
		"file":    map[string]interface{}{},
	})

	if err := migrateLibrarySkillsCaps(lingtaiDir); err != nil {
		t.Fatalf("migrateLibrarySkillsCaps: %v", err)
	}

	caps := readM034Caps(t, initPath)
	if _, ok := caps["codex"]; ok {
		t.Fatalf("codex key should be gone: %v", caps)
	}
	lib, ok := caps["library"].(map[string]interface{})
	if !ok {
		t.Fatalf("library durable config missing: %v", caps)
	}
	if _, ok := lib["codex_limit"]; ok {
		t.Errorf("codex_limit should be removed: %v", lib)
	}
	if got := lib["library_limit"]; got != float64(12) {
		t.Errorf("library_limit = %v, want 12", got)
	}
	if _, ok := lib["paths"]; ok {
		t.Errorf("paths should move off durable library: %v", lib)
	}
	skills, ok := caps["skills"].(map[string]interface{})
	if !ok {
		t.Fatalf("skills config missing: %v", caps)
	}
	paths, _ := skills["paths"].([]interface{})
	if len(paths) != 2 || paths[1] != "../../velli" {
		t.Errorf("skills.paths = %v", paths)
	}
}

func TestMigrateLibrarySkillsCaps_LegacyBareLibraryBecomesSkills(t *testing.T) {
	caps := map[string]interface{}{
		"library": map[string]interface{}{},
	}
	if !rewriteLibrarySkillsCapsMap(caps) {
		t.Fatalf("rewrite reported no change")
	}
	if _, ok := caps["library"]; ok {
		t.Fatalf("bare legacy library should not remain durable library: %v", caps)
	}
	if _, ok := caps["skills"]; !ok {
		t.Fatalf("skills key missing: %v", caps)
	}
}

func TestMigrateLibrarySkillsCaps_LegacyLibraryPathsBecomeSkillsOnly(t *testing.T) {
	caps := map[string]interface{}{
		"library": map[string]interface{}{"paths": []interface{}{"../.library_shared"}},
	}
	if !rewriteLibrarySkillsCapsMap(caps) {
		t.Fatalf("rewrite reported no change")
	}
	if _, ok := caps["library"]; ok {
		t.Fatalf("legacy library paths should not leave durable library: %v", caps)
	}
	skills, ok := caps["skills"].(map[string]interface{})
	if !ok {
		t.Fatalf("skills key missing: %v", caps)
	}
	if _, ok := skills["paths"]; !ok {
		t.Fatalf("skills.paths missing: %v", skills)
	}
}

func TestMigrateLibrarySkillsCaps_ExplicitLibraryAndSkillsKeepsDurableLibrary(t *testing.T) {
	caps := map[string]interface{}{
		"library": map[string]interface{}{},
		"skills":  map[string]interface{}{},
	}
	if !rewriteLibrarySkillsCapsMap(caps) {
		t.Fatalf("rewrite should add default library_limit")
	}
	lib, ok := caps["library"].(map[string]interface{})
	if !ok {
		t.Fatalf("explicit library should remain durable when skills is also present: %v", caps)
	}
	if got := lib["library_limit"]; got != float64(50) {
		t.Fatalf("library_limit = %v, want default 50", got)
	}
	if _, ok := caps["skills"]; !ok {
		t.Fatalf("skills key should remain: %v", caps)
	}
}

func TestMigrateLibrarySkillsCaps_IdempotentNewConfig(t *testing.T) {
	caps := map[string]interface{}{
		"library": map[string]interface{}{"library_limit": float64(50)},
		"skills":  map[string]interface{}{"paths": []interface{}{"../.library_shared"}},
	}
	if rewriteLibrarySkillsCapsMap(caps) {
		t.Fatalf("new config should be unchanged: %v", caps)
	}
}

func TestMigrateLibrarySkillsCaps_GlobalPresetSubdirs(t *testing.T) {
	globalDir := withTempHome(t)
	presetPath := filepath.Join(globalDir, "presets", "saved", "custom.json")
	writeM034Doc(t, presetPath, map[string]interface{}{
		"codex":   map[string]interface{}{},
		"library": map[string]interface{}{"paths": []interface{}{"../.library_shared"}},
	})

	if err := migrateLibrarySkillsCaps(t.TempDir()); err != nil {
		t.Fatalf("migrateLibrarySkillsCaps: %v", err)
	}

	caps := readM034Caps(t, presetPath)
	if _, ok := caps["codex"]; ok {
		t.Fatalf("codex key should be gone: %v", caps)
	}
	lib, ok := caps["library"].(map[string]interface{})
	if !ok {
		t.Fatalf("library key missing: %v", caps)
	}
	if got := lib["library_limit"]; got != float64(50) {
		t.Fatalf("library_limit = %v, want default 50", got)
	}
	if _, ok := caps["skills"]; !ok {
		t.Fatalf("skills key missing: %v", caps)
	}
}
