package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeAgentInit(t *testing.T, lingtaiDir, agent, content string) string {
	t.Helper()
	agentDir := filepath.Join(lingtaiDir, agent)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(agentDir, "init.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func readAgentSkills(t *testing.T, path string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("patched init.json is not valid json: %v\n%s", err, data)
	}
	manifest, ok := doc["manifest"].(map[string]interface{})
	if !ok {
		t.Fatalf("manifest missing or not a map: %s", data)
	}
	caps, ok := manifest["capabilities"].(map[string]interface{})
	if !ok {
		t.Fatalf("capabilities missing or not a map: %s", data)
	}
	skills, ok := caps["skills"].(map[string]interface{})
	if !ok {
		t.Fatalf("skills missing or not a map: %s", data)
	}
	return skills
}

func TestMigrateAgentInitSkillsPathsPatchesMultipleAgents(t *testing.T) {
	lingtaiDir := t.TempDir()
	// skills entry present but paths lost
	orch := writeAgentInit(t, lingtaiDir, "orch",
		`{"manifest":{"capabilities":{"skills":{"library_limit":42},"web_search":{"provider":"zhipu"}}}}`)
	// skills entry lost entirely
	scout := writeAgentInit(t, lingtaiDir, "scout",
		`{"manifest":{"capabilities":{"bash":{"yolo":true}}}}`)

	if err := migrateAgentInitSkillsPaths(lingtaiDir); err != nil {
		t.Fatal(err)
	}

	orchSkills := readAgentSkills(t, orch)
	if !reflect.DeepEqual(orchSkills["paths"], defaultPresetSkillsPaths) {
		t.Fatalf("orch skills.paths = %#v, want %#v", orchSkills["paths"], defaultPresetSkillsPaths)
	}
	if got := orchSkills["library_limit"]; got != float64(42) {
		t.Fatalf("orch existing skills config overwritten: %#v", orchSkills)
	}

	scoutSkills := readAgentSkills(t, scout)
	if !reflect.DeepEqual(scoutSkills["paths"], defaultPresetSkillsPaths) {
		t.Fatalf("scout skills.paths = %#v, want %#v", scoutSkills["paths"], defaultPresetSkillsPaths)
	}

	// sibling capability entries survive
	data, _ := os.ReadFile(orch)
	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	caps := doc["manifest"].(map[string]interface{})["capabilities"].(map[string]interface{})
	ws, ok := caps["web_search"].(map[string]interface{})
	if !ok || ws["provider"] != "zhipu" {
		t.Fatalf("web_search entry damaged: %#v", caps["web_search"])
	}
}

func TestMigrateAgentInitSkillsPathsCreatesMissingCapabilitiesMap(t *testing.T) {
	lingtaiDir := t.TempDir()
	path := writeAgentInit(t, lingtaiDir, "orch", `{"manifest":{"llm":{"provider":"custom"}}}`)

	if err := migrateAgentInitSkillsPaths(lingtaiDir); err != nil {
		t.Fatal(err)
	}

	skills := readAgentSkills(t, path)
	if !reflect.DeepEqual(skills["paths"], defaultPresetSkillsPaths) {
		t.Fatalf("skills.paths = %#v, want %#v", skills["paths"], defaultPresetSkillsPaths)
	}
}

func TestMigrateAgentInitSkillsPathsPreservesCustomPaths(t *testing.T) {
	lingtaiDir := t.TempDir()
	content := `{"manifest":{"capabilities":{"skills":{"paths":["./custom-skills"]}}}}`
	path := writeAgentInit(t, lingtaiDir, "orch", content)

	if err := migrateAgentInitSkillsPaths(lingtaiDir); err != nil {
		t.Fatal(err)
	}

	// File must be byte-identical: nothing to patch means no rewrite.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != content {
		t.Fatalf("init.json with custom paths was rewritten: %s", data)
	}
}

func TestMigrateAgentInitSkillsPathsSkipsMalformedAndNonMapCases(t *testing.T) {
	lingtaiDir := t.TempDir()
	malformed := writeAgentInit(t, lingtaiDir, "broken", `{"manifest":`)
	nonMapCaps := writeAgentInit(t, lingtaiDir, "weird-caps",
		`{"manifest":{"capabilities":["skills"]}}`)
	nonMapManifest := writeAgentInit(t, lingtaiDir, "weird-manifest",
		`{"manifest":"nope"}`)
	// a valid sibling proves the migration continues past the broken ones
	good := writeAgentInit(t, lingtaiDir, "zz-good", `{"manifest":{"capabilities":{}}}`)

	if err := migrateAgentInitSkillsPaths(lingtaiDir); err != nil {
		t.Fatal(err)
	}

	for name, path := range map[string]string{
		"malformed": malformed, "non-map capabilities": nonMapCaps, "non-map manifest": nonMapManifest,
	} {
		before := map[string]string{
			"malformed":            `{"manifest":`,
			"non-map capabilities": `{"manifest":{"capabilities":["skills"]}}`,
			"non-map manifest":     `{"manifest":"nope"}`,
		}[name]
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != before {
			t.Fatalf("%s init.json should be unchanged, got: %s", name, data)
		}
	}

	skills := readAgentSkills(t, good)
	if !reflect.DeepEqual(skills["paths"], defaultPresetSkillsPaths) {
		t.Fatalf("valid sibling not patched: %#v", skills["paths"])
	}
}
