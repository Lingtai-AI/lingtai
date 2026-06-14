package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPatchPresetSkillsPathsMapAddsMissingSkills(t *testing.T) {
	caps := map[string]interface{}{
		"web_search": map[string]interface{}{"provider": "duckduckgo"},
	}
	if !patchPresetSkillsPathsMap(caps) {
		t.Fatal("expected missing skills capability to be patched")
	}
	skills, ok := caps["skills"].(map[string]interface{})
	if !ok {
		t.Fatalf("skills not added as map: %#v", caps["skills"])
	}
	if !reflect.DeepEqual(skills["paths"], defaultPresetSkillsPaths) {
		t.Fatalf("skills.paths = %#v, want %#v", skills["paths"], defaultPresetSkillsPaths)
	}
}

func TestPatchPresetSkillsPathsMapAddsMissingPathsOnly(t *testing.T) {
	caps := map[string]interface{}{
		"skills": map[string]interface{}{"library_limit": float64(42)},
	}
	if !patchPresetSkillsPathsMap(caps) {
		t.Fatal("expected missing skills.paths to be patched")
	}
	skills := caps["skills"].(map[string]interface{})
	if got := skills["library_limit"]; got != float64(42) {
		t.Fatalf("existing skills config overwritten: %#v", skills)
	}
	if !reflect.DeepEqual(skills["paths"], defaultPresetSkillsPaths) {
		t.Fatalf("skills.paths = %#v, want %#v", skills["paths"], defaultPresetSkillsPaths)
	}
}

func TestPatchPresetSkillsPathsMapPreservesExistingPaths(t *testing.T) {
	custom := []interface{}{"./custom-skills"}
	caps := map[string]interface{}{
		"skills": map[string]interface{}{"paths": custom},
	}
	if patchPresetSkillsPathsMap(caps) {
		t.Fatal("expected existing skills.paths to be left untouched")
	}
	skills := caps["skills"].(map[string]interface{})
	if !reflect.DeepEqual(skills["paths"], custom) {
		t.Fatalf("custom paths overwritten: %#v", skills["paths"])
	}
}

func TestPatchPresetSkillsPathsFileAddsMissingCapabilitiesMap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing-caps.json")
	if err := os.WriteFile(path, []byte(`{"manifest":{"llm":{"provider":"custom"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	patchPresetSkillsPathsFile(path)
	var doc map[string]interface{}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("patched file is not valid json: %v\n%s", err, data)
	}
	manifest := doc["manifest"].(map[string]interface{})
	caps := manifest["capabilities"].(map[string]interface{})
	skills := caps["skills"].(map[string]interface{})
	if !reflect.DeepEqual(skills["paths"], defaultPresetSkillsPaths) {
		t.Fatalf("skills.paths = %#v, want %#v", skills["paths"], defaultPresetSkillsPaths)
	}
}

func TestPatchPresetSkillsPathsFileSkipsMalformedAndPatchesValid(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte(`{"manifest":`), 0o644); err != nil {
		t.Fatal(err)
	}
	patchPresetSkillsPathsFile(bad)
	if got, _ := os.ReadFile(bad); string(got) != `{"manifest":` {
		t.Fatalf("malformed file should be unchanged, got %q", string(got))
	}

	good := filepath.Join(dir, "good.json")
	if err := os.WriteFile(good, []byte(`{"manifest":{"capabilities":{"web_search":{"provider":"duckduckgo"}}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	patchPresetSkillsPathsFile(good)
	var doc map[string]interface{}
	data, err := os.ReadFile(good)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("patched file is not valid json: %v\n%s", err, data)
	}
	manifest := doc["manifest"].(map[string]interface{})
	caps := manifest["capabilities"].(map[string]interface{})
	skills := caps["skills"].(map[string]interface{})
	if !reflect.DeepEqual(skills["paths"], defaultPresetSkillsPaths) {
		t.Fatalf("skills.paths = %#v, want %#v", skills["paths"], defaultPresetSkillsPaths)
	}
}
